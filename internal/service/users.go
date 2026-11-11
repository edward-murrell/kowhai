package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"math/big"
	"slices"
	"strings"
	"time"

	. "github.com/edward-murrell/kowhai/internal/apitypes"
	"github.com/edward-murrell/kowhai/internal/store"
)

// LoginInfo is what the OIDC callback learned about the user.
type LoginInfo struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
	Groups        []string
}

// ErrDisabled is returned when a disabled user tries to sign in.
var ErrDisabled = &Error{Status: 403, Title: "Account disabled"}

func (s *Service) isAdmin(info LoginInfo) bool {
	for _, e := range s.Cfg.AdminEmails {
		if e != "" && strings.EqualFold(e, info.Email) {
			return true
		}
	}
	if g := s.Cfg.AdminGroup; g != "" {
		for _, have := range info.Groups {
			// Kanidm sends groups as SPNs: name@domain.
			if have == g || strings.SplitN(have, "@", 2)[0] == g {
				return true
			}
		}
	}
	return false
}

// LoginUser finds or creates the user for an OIDC login, refreshes their
// profile and accepts pending workspace invites for their email.
func (s *Service) LoginUser(ctx context.Context, info LoginInfo) (store.User, error) {
	var user store.User
	name := info.Name
	if name == "" {
		name = strings.SplitN(info.Email, "@", 2)[0]
	}
	groups := mustJSON(slices.DeleteFunc(slices.Clone(info.Groups), func(g string) bool { return g == "" }))
	if groups == "null" {
		groups = "[]"
	}
	err := s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		now := s.now()
		u, err := tx.GetUserByOIDC(ctx, store.GetUserByOIDCParams{OidcIssuer: info.Issuer, OidcSubject: info.Subject})
		switch {
		case errors.Is(err, sql.ErrNoRows):
			u, err = tx.CreateUser(ctx, store.CreateUserParams{
				ID: s.NewID(), OidcIssuer: info.Issuer, OidcSubject: info.Subject, Email: info.Email,
				DisplayName: name, AvatarUrl: info.Picture, Groups: groups, IsAdmin: s.isAdmin(info),
				CreatedAt: now, UpdatedAt: now,
			})
		case err == nil:
			if u.DisabledAt.Valid {
				return ErrDisabled
			}
			u, err = tx.UpdateUserFromLogin(ctx, store.UpdateUserFromLoginParams{
				ID: u.ID, Email: info.Email, DisplayName: name, AvatarUrl: info.Picture, Groups: groups,
				IsAdmin: s.isAdmin(info), UpdatedAt: now,
			})
		}
		if err != nil {
			return err
		}
		user = u
		if info.Email == "" || !info.EmailVerified {
			return nil
		}
		invites, err := tx.ListInvitesForEmail(ctx, store.ListInvitesForEmailParams{Email: info.Email, Now: now})
		if err != nil {
			return err
		}
		a := &Actor{User: u}
		for _, inv := range invites {
			if _, err := tx.GetWorkspaceRole(ctx, store.GetWorkspaceRoleParams{WorkspaceID: inv.WorkspaceID, UserID: u.ID}); err == nil {
				// Already a member. Keep the existing role.
			} else if err := tx.UpsertWorkspaceMember(ctx, store.UpsertWorkspaceMemberParams{
				WorkspaceID: inv.WorkspaceID, UserID: u.ID, Role: inv.Role, CreatedAt: now,
			}); err != nil {
				return err
			}
			if err := tx.DeleteWorkspaceInvite(ctx, inv.ID); err != nil {
				return err
			}
			if _, err := s.record(ctx, tx, a, activityInput{
				WorkspaceID: inv.WorkspaceID, Type: TWorkspaceMemberAdded,
				Payload: map[string]any{"user_id": u.ID, "user_name": u.DisplayName, "role": inv.Role, "via": "invite"},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return user, err
}

func (s *Service) Me(ctx context.Context) (Me, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Me{}, err
	}
	n, err := s.DB.R.CountUnreadNotifications(ctx, a.User.ID)
	if err != nil {
		return Me{}, err
	}
	me := Me{User: userOut(a.User), UnreadNotifications: int(n)}
	if a.Token != nil {
		me.Token = ptr(tokenOut(*a.Token))
	}
	return me, nil
}

// SearchUsers finds active users by name or email for member pickers.
func (s *Service) SearchUsers(ctx context.Context, q string) ([]User, error) {
	if _, err := mustActor(ctx); err != nil {
		return nil, err
	}
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return []User{}, nil
	}
	rows, err := s.DB.R.ListUsers(ctx, store.ListUsersParams{Query: q, Limit: 40})
	if err != nil {
		return nil, err
	}
	out := []User{}
	for _, u := range rows {
		if !u.DisabledAt.Valid && len(out) < 20 {
			out = append(out, userOut(u))
		}
	}
	return out, nil
}

func (s *Service) AdminListUsers(ctx context.Context, q *string, cursor *string, limit *int) (UserPage, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return UserPage{}, err
	}
	if err := s.Authorize(ctx, a, ActSiteAdmin, Resource{}); err != nil {
		return UserPage{}, err
	}
	l := clampLimit(limit, 50, 200)
	rows, err := s.DB.R.ListUsers(ctx, store.ListUsersParams{After: deref(cursor, ""), Query: deref(q, ""), Limit: int64(l + 1)})
	if err != nil {
		return UserPage{}, err
	}
	page := UserPage{Items: []User{}}
	if len(rows) > l {
		rows = rows[:l]
		page.NextCursor = ptr(rows[l-1].ID)
	}
	for _, u := range rows {
		page.Items = append(page.Items, userOut(u))
	}
	return page, nil
}

// AdminSetDisabled disables or enables an account. Disabling also ends
// the user's sessions. Their tokens stop working because auth checks
// the user's disabled flag.
func (s *Service) AdminSetDisabled(ctx context.Context, userID string, disabled bool) (User, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return User{}, err
	}
	if err := s.Authorize(ctx, a, ActSiteAdmin, Resource{}); err != nil {
		return User{}, err
	}
	if userID == a.User.ID && disabled {
		return User{}, badRequest("you cannot disable your own account")
	}
	var out store.User
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		at := sql.NullTime{}
		if disabled {
			at = nt(s.now())
		}
		u, err := tx.SetUserDisabled(ctx, store.SetUserDisabledParams{ID: userID, DisabledAt: at, UpdatedAt: s.now()})
		if err != nil {
			return notFound(err)
		}
		out = u
		if disabled {
			return tx.DeleteUserSessions(ctx, userID)
		}
		return nil
	})
	return userOut(out), err
}

// ---- Personal access tokens ----

const tokenPrefix = "kowhai_pat_"

// HashToken returns the stored form of a token.
func HashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// IsPAT reports whether s looks like a personal access token.
func IsPAT(s string) bool { return strings.HasPrefix(s, tokenPrefix) }

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// RandomString returns n random base-62 characters.
func RandomString(n int) string {
	b := make([]byte, n)
	m := big.NewInt(int64(len(base62)))
	for i := range b {
		v, err := rand.Int(rand.Reader, m)
		if err != nil {
			panic(err)
		}
		b[i] = base62[v.Int64()]
	}
	return string(b)
}

func (s *Service) ListTokens(ctx context.Context) ([]Token, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(ctx, a, ActTokenManage, Resource{}); err != nil {
		return nil, err
	}
	rows, err := s.DB.R.ListUserTokens(ctx, a.User.ID)
	if err != nil {
		return nil, err
	}
	out := []Token{}
	for _, t := range rows {
		out = append(out, tokenOut(t))
	}
	return out, nil
}

func (s *Service) CreateToken(ctx context.Context, in CreateToken) (CreatedToken, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return CreatedToken{}, err
	}
	if err := s.Authorize(ctx, a, ActTokenManage, Resource{}); err != nil {
		return CreatedToken{}, err
	}
	var scopes []string
	for _, sc := range in.Scopes {
		if sc != "read" && sc != "write" {
			return CreatedToken{}, badRequest("unknown scope %q", sc)
		}
		if !slices.Contains(scopes, string(sc)) {
			scopes = append(scopes, string(sc))
		}
	}
	if slices.Contains(scopes, "write") && !slices.Contains(scopes, "read") {
		scopes = append([]string{"read"}, scopes...)
	}
	if strings.TrimSpace(in.Name) == "" {
		return CreatedToken{}, badRequest("name is required")
	}
	// The user must be able to see what the token is limited to.
	if ws := deref(in.WorkspaceId, ""); ws != "" {
		if _, err := s.loadWorkspace(ctx, a, ws, ActWorkspaceView); err != nil {
			return CreatedToken{}, err
		}
	}
	if bid := deref(in.BoardId, ""); bid != "" {
		b, err := s.loadBoard(ctx, a, bid, ActBoardView)
		if err != nil {
			return CreatedToken{}, err
		}
		if ws := deref(in.WorkspaceId, ""); ws != "" && ws != b.WorkspaceID {
			return CreatedToken{}, badRequest("board is not in that workspace")
		}
	}
	if in.ExpiresAt != nil && in.ExpiresAt.Before(s.now()) {
		return CreatedToken{}, badRequest("expires_at is in the past")
	}
	secret := RandomString(40)
	plain := tokenPrefix + secret
	row, err := store.New(s.DB.Write).CreateToken(ctx, store.CreateTokenParams{
		ID: s.NewID(), UserID: a.User.ID, Name: strings.TrimSpace(in.Name), TokenHash: HashToken(plain),
		Prefix: tokenPrefix + secret[:6], Scopes: strings.Join(scopes, ","),
		WorkspaceID: nsp(in.WorkspaceId), BoardID: nsp(in.BoardId),
		ActorLabel: strings.TrimSpace(deref(in.ActorLabel, "")), ExpiresAt: ntp(in.ExpiresAt), CreatedAt: s.now(),
	})
	if err != nil {
		return CreatedToken{}, err
	}
	t := tokenOut(row)
	return CreatedToken{
		Id: t.Id, Name: t.Name, Prefix: t.Prefix, Scopes: t.Scopes, WorkspaceId: t.WorkspaceId, BoardId: t.BoardId,
		ActorLabel: t.ActorLabel, ExpiresAt: t.ExpiresAt, Revoked: false, CreatedAt: t.CreatedAt, Token: plain,
	}, nil
}

func (s *Service) RevokeToken(ctx context.Context, id string) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	if err := s.Authorize(ctx, a, ActTokenManage, Resource{}); err != nil {
		return err
	}
	t, err := s.DB.R.GetToken(ctx, id)
	if err != nil || t.UserID != a.User.ID {
		return ErrNotFound
	}
	return store.New(s.DB.Write).RevokeToken(ctx, store.RevokeTokenParams{ID: id, UserID: a.User.ID, RevokedAt: nt(s.now())})
}

// ActorForToken checks a bearer token and returns its actor.
func (s *Service) ActorForToken(ctx context.Context, plain string) (*Actor, error) {
	t, err := s.DB.R.GetTokenByHash(ctx, HashToken(plain))
	if err != nil {
		return nil, ErrUnauthorized
	}
	now := s.now()
	if t.RevokedAt.Valid || (t.ExpiresAt.Valid && t.ExpiresAt.Time.Before(now)) {
		return nil, ErrUnauthorized
	}
	u, err := s.DB.R.GetUser(ctx, t.UserID)
	if err != nil || u.DisabledAt.Valid {
		return nil, ErrUnauthorized
	}
	a := &Actor{User: u, Token: &t}
	if t.BoardID.Valid {
		b, err := s.DB.R.GetBoard(ctx, t.BoardID.String)
		if err != nil {
			return nil, ErrUnauthorized
		}
		a.TokenBoardWorkspace = b.WorkspaceID
	}
	// Record use at most once a minute to keep writes down.
	if !t.LastUsedAt.Valid || now.Sub(t.LastUsedAt.Time) > time.Minute {
		_ = store.New(s.DB.Write).TouchToken(ctx, store.TouchTokenParams{ID: t.ID, LastUsedAt: nt(now)})
	}
	return a, nil
}
