// Package testenv runs a full Kōwhai server in-process for tests: a real
// SQLite file in a temp folder, an in-memory blob store, and the dev OIDC
// provider. Every API call is checked against api/openapi.yaml.
package testenv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"

	apispec "kowhai/api"
	"kowhai/internal/auth"
	"kowhai/internal/auth/oidctest"
	"kowhai/internal/blob"
	"kowhai/internal/events"
	"kowhai/internal/server"
	"kowhai/internal/service"
	"kowhai/internal/store"
)

// Users the dev provider knows. Alice is a site admin.
var Users = []oidctest.User{
	{Subject: "alice", Email: "alice@example.com", Name: "Alice", Groups: []string{"admins"}},
	{Subject: "bob", Email: "bob@example.com", Name: "Bob"},
	{Subject: "carol", Email: "carol@example.com", Name: "Carol"},
	{Subject: "dave", Email: "dave@example.com", Name: "Dave"},
}

// Clock is a settable clock.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t.UTC()
	c.mu.Unlock()
}

func (c *Clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type Env struct {
	T     testing.TB
	URL   string
	Svc   *service.Service
	DB    *store.DB
	Blob  *blob.Mem
	Hub   *events.Hub
	Clock *Clock
	Srv   *httptest.Server
	// Transport reaches the server. Use it for every client.
	Transport *http.Transport

	router routers.Router
	mu     sync.Mutex
	errs   []string
}

type Option func(*opts)

type opts struct {
	alg       string
	rateLimit int
}

// WithAlg picks the provider's signing algorithm (ES256 or RS256).
func WithAlg(alg string) Option { return func(o *opts) { o.alg = alg } }

// WithRateLimit turns on rate limiting.
func WithRateLimit(rps int) Option { return func(o *opts) { o.rateLimit = rps } }

func New(t testing.TB, options ...Option) *Env {
	o := opts{alg: "ES256"}
	for _, f := range options {
		f(&o)
	}
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "kowhai.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	hub := events.NewHub()
	mem := blob.NewMem()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.New(db, hub, mem, service.Config{MaxUploadBytes: 1 << 20, AdminGroup: "admins"}, log)
	clock := &Clock{t: time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)}
	svc.Clock = clock

	e := &Env{T: t, Svc: svc, DB: db, Blob: mem, Hub: hub, Clock: clock}
	// The handler is created after the server so it knows its own URL.
	var h http.Handler
	ln := newMemListener()
	e.Srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	e.Srv.Listener = ln
	e.Srv.Start()
	t.Cleanup(e.Srv.Close)
	e.URL = e.Srv.URL
	e.Transport = &http.Transport{DialContext: ln.Dial}

	issuer := e.URL + "/oidc"
	prov, err := oidctest.New(issuer, "kowhai", "secret", o.alg, Users)
	if err != nil {
		t.Fatal(err)
	}
	prov.EndSession = o.alg == "RS256"
	au, err := auth.New(auth.Config{BaseURL: e.URL, Issuer: issuer, ClientID: "kowhai", ClientSecret: "secret", HTTPClient: e.HTTP()}, svc, log)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := au.Discover(context.Background()); err != nil {
			t.Fatalf("discovery: %v", err)
		}
	}()
	inner := server.Handler(server.Options{
		Service: svc, Hub: hub, Auth: au, Log: log, BaseURL: e.URL, Version: "test", RateLimit: o.rateLimit,
		Ready: map[string]server.Pinger{"db": db, "s3": mem}, Extra: map[string]http.Handler{"/oidc/": prov},
	})
	e.router = e.loadRouter()
	h = e.validate(inner)
	t.Cleanup(func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, m := range e.errs {
			t.Errorf("contract: %s", m)
		}
	})
	return e
}

// HTTP returns a plain client for the test server.
func (e *Env) HTTP() *http.Client { return &http.Client{Transport: e.Transport} }

func (e *Env) loadRouter() routers.Router {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(apispec.YAML)
	if err != nil {
		e.T.Fatal(err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		e.T.Fatalf("openapi.yaml is not valid: %v", err)
	}
	doc.Servers = openapi3.Servers{{URL: e.URL + "/api/v1"}}
	r, err := legacy.NewRouter(doc)
	if err != nil {
		e.T.Fatal(err)
	}
	return r
}

type capture struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
	stream bool
}

func (c *capture) WriteHeader(s int) {
	c.status = s
	c.stream = strings.HasPrefix(c.Header().Get("Content-Type"), "text/event-stream")
	c.ResponseWriter.WriteHeader(s)
}

func (c *capture) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = 200
	}
	if !c.stream && c.body.Len() < 4<<20 {
		c.body.Write(b)
	}
	return c.ResponseWriter.Write(b)
}

func (c *capture) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *capture) Unwrap() http.ResponseWriter { return c.ResponseWriter }

// validate checks API requests and responses against the spec. It skips
// streams, downloads and uploads.
func (e *Env) validate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") || strings.HasSuffix(r.URL.Path, "/events") ||
			strings.HasSuffix(r.URL.Path, "/download") || strings.HasSuffix(r.URL.Path, "/openapi.yaml") ||
			(r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/attachments")) {
			next.ServeHTTP(w, r)
			return
		}
		u := *r.URL
		u.Scheme, u.Host = "http", strings.TrimPrefix(e.URL, "http://")
		rr := r.Clone(r.Context())
		rr.URL = &u
		route, params, err := e.router.FindRoute(rr)
		if err != nil {
			c := &capture{ResponseWriter: w}
			next.ServeHTTP(c, r)
			if c.status != http.StatusNotFound {
				e.fail("%s %s: no route in spec: %v", r.Method, r.URL.Path, err)
			}
			return
		}
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			rr.Body = io.NopCloser(bytes.NewReader(body))
		}
		in := &openapi3filter.RequestValidationInput{Request: rr, PathParams: params, Route: route,
			Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}}
		reqOK := openapi3filter.ValidateRequest(r.Context(), in) == nil
		c := &capture{ResponseWriter: w}
		next.ServeHTTP(c, r)
		// Only check responses for valid requests that succeeded or failed cleanly.
		if !reqOK || c.stream {
			return
		}
		out := &openapi3filter.ResponseValidationInput{
			RequestValidationInput: in, Status: c.status, Header: w.Header(),
			Body: io.NopCloser(bytes.NewReader(c.body.Bytes())),
			Options: &openapi3filter.Options{IncludeResponseStatus: true},
		}
		if err := openapi3filter.ValidateResponse(r.Context(), out); err != nil {
			e.fail("%s %s -> %d: %v\nbody: %.500s", r.Method, r.URL.Path, c.status, err, c.body.String())
		}
	})
}

func (e *Env) fail(format string, args ...any) {
	e.mu.Lock()
	e.errs = append(e.errs, fmt.Sprintf(format, args...))
	e.mu.Unlock()
}

// Client is an HTTP client signed in as one user, by cookie or token.
type Client struct {
	E     *Env
	HTTP  *http.Client
	CSRF  string
	Token string
	Email string
}

// Login signs in through the OIDC flow and returns a cookie client.
func (e *Env) Login(email string) *Client {
	e.T.Helper()
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar, Transport: e.Transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if !strings.HasPrefix(req.URL.Path, "/oidc/") && !strings.HasPrefix(req.URL.Path, "/auth/") {
			// Signed in and sent back to the app.
			return http.ErrUseLastResponse
		}
		if strings.Contains(req.URL.Path, "/oidc/authorize") && req.URL.Query().Get("login_hint") == "" {
			q := req.URL.Query()
			q.Set("login_hint", email)
			req.URL.RawQuery = q.Encode()
		}
		return nil
	}}
	resp, err := hc.Get(e.URL + "/auth/login")
	if err != nil {
		e.T.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		e.T.Fatalf("login %s: status %d", email, resp.StatusCode)
	}
	c := &Client{E: e, HTTP: hc, Email: email}
	u, _ := url.Parse(e.URL)
	for _, ck := range jar.Cookies(u) {
		if ck.Name == auth.CSRFCookie {
			c.CSRF = ck.Value
		}
	}
	if c.CSRF == "" {
		e.T.Fatal("no CSRF cookie after login")
	}
	return c
}

// WithToken returns a client that uses a bearer token.
func (e *Env) WithToken(tok string) *Client {
	return &Client{E: e, HTTP: e.HTTP(), Token: tok}
}

// Do sends a request and decodes a JSON response into out. It returns
// the status code.
func (c *Client) Do(method, path string, body, out any, headers ...string) int {
	c.E.T.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.E.URL+"/api/v1"+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	} else if c.CSRF != "" {
		req.Header.Set("X-Kowhai-CSRF", c.CSRF)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		c.E.T.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil && resp.StatusCode < 300 && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			c.E.T.Fatalf("%s %s: decode: %v: %s", method, path, err, data)
		}
	}
	return resp.StatusCode
}

// Must is Do that fails the test unless the status is want.
func (c *Client) Must(want int, method, path string, body, out any, headers ...string) {
	c.E.T.Helper()
	var raw json.RawMessage
	got := c.Do(method, path, body, &raw, headers...)
	if got != want {
		c.E.T.Fatalf("%s %s: status %d, want %d", method, path, got, want)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			c.E.T.Fatal(err)
		}
	}
}
