// Package service holds the business rules. Every change goes through
// here, so permissions, activity rows, events and search indexing apply
// the same way for the REST API, MCP and workers.
package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"kowhai/internal/events"
	"kowhai/internal/store"
)

// Clock lets tests control time.
type Clock interface{ Now() time.Time }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// RealClock returns the system clock in UTC.
func RealClock() Clock { return realClock{} }

// Blob is the attachment object store.
type Blob interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// Config holds the settings the service needs.
type Config struct {
	MaxUploadBytes int64
	// AdminEmails are always site admins.
	AdminEmails []string
	// AdminGroup is an OIDC group whose members are site admins.
	AdminGroup string
}

type Service struct {
	DB    *store.DB
	Hub   *events.Hub
	Blob  Blob
	Clock Clock
	Log   *slog.Logger
	Cfg   Config

	idMu      sync.Mutex
	idEntropy io.Reader
}

func New(db *store.DB, hub *events.Hub, blob Blob, cfg Config, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		DB: db, Hub: hub, Blob: blob, Clock: RealClock(), Log: log, Cfg: cfg,
		idEntropy: ulid.Monotonic(rand.Reader, 0),
	}
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

// NewID returns a new ULID. IDs from one process sort in creation order,
// which the SSE replay relies on.
func (s *Service) NewID() string {
	s.idMu.Lock()
	defer s.idMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(s.Clock.Now()), s.idEntropy).String()
}

// ---- Actor ----

// Actor is the user making a request, and the token they used if any.
type Actor struct {
	User  store.User
	Token *store.ApiToken
	// TokenBoardWorkspace is the workspace of Token.BoardID, when set.
	TokenBoardWorkspace string
}

func (a *Actor) ID() string { return a.User.ID }

// CanWrite reports whether the token, if any, has the write scope.
func (a *Actor) CanWrite() bool {
	if a.Token == nil {
		return true
	}
	return hasScope(a.Token.Scopes, "write")
}

// Label is the actor label shown next to the user's name.
func (a *Actor) Label() string {
	if a.Token == nil {
		return ""
	}
	return a.Token.ActorLabel
}

func (a *Actor) tokenID() sql.NullString {
	if a.Token == nil {
		return sql.NullString{}
	}
	return ns(a.Token.ID)
}

func hasScope(scopes, want string) bool {
	for _, s := range strings.Split(scopes, ",") {
		if strings.TrimSpace(s) == want {
			return true
		}
	}
	return false
}

type actorKey struct{}

// WithActor stores the actor on the context.
func WithActor(ctx context.Context, a *Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom returns the actor on the context, or nil.
func ActorFrom(ctx context.Context) *Actor {
	a, _ := ctx.Value(actorKey{}).(*Actor)
	return a
}

func mustActor(ctx context.Context) (*Actor, error) {
	a := ActorFrom(ctx)
	if a == nil {
		return nil, ErrUnauthorized
	}
	return a, nil
}

// ---- Errors ----

// Error carries an HTTP status. The API turns it into problem+json.
type Error struct {
	Status int
	Title  string
	Detail string
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return e.Title + ": " + e.Detail
	}
	return e.Title
}

func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Status == e.Status
}

var (
	ErrBadRequest   = &Error{Status: 400, Title: "Bad request"}
	ErrUnauthorized = &Error{Status: 401, Title: "Not signed in"}
	ErrForbidden    = &Error{Status: 403, Title: "Forbidden"}
	ErrNotFound     = &Error{Status: 404, Title: "Not found"}
	ErrConflict     = &Error{Status: 409, Title: "Conflict"}
	ErrStale        = &Error{Status: 412, Title: "Version mismatch", Detail: "the resource changed, reload and try again"}
	ErrTooLarge     = &Error{Status: 413, Title: "Too large"}
)

func badRequest(format string, args ...any) error {
	return &Error{Status: 400, Title: "Bad request", Detail: fmt.Sprintf(format, args...)}
}

func forbidden(detail string) error {
	return &Error{Status: 403, Title: "Forbidden", Detail: detail}
}

func conflict(detail string) error {
	return &Error{Status: 409, Title: "Conflict", Detail: detail}
}

// notFound maps sql.ErrNoRows to ErrNotFound.
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// CheckVersion compares an If-Match value with the current version.
// An empty ifMatch skips the check.
func CheckVersion(ifMatch string, version int64) error {
	ifMatch = strings.Trim(strings.TrimPrefix(strings.TrimSpace(ifMatch), "W/"), `"`)
	if ifMatch == "" || ifMatch == "*" {
		return nil
	}
	v, err := strconv.ParseInt(ifMatch, 10, 64)
	if err != nil {
		return badRequest("If-Match must be a version number")
	}
	if v != version {
		return ErrStale
	}
	return nil
}

// ---- Null helpers ----

func ns(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func nsp(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return ns(*s)
}

func nt(t time.Time) sql.NullTime { return sql.NullTime{Time: t.UTC(), Valid: !t.IsZero()} }

func ntp(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return nt(*t)
}

func strp(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	v := s.String
	return &v
}

func timep(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}

func ptr[T any](v T) *T { return &v }

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func clampLimit(l *int, def, maxv int) int {
	if l == nil || *l <= 0 {
		return def
	}
	return min(*l, maxv)
}
