// Package auth handles OIDC login, browser sessions, CSRF protection and
// bearer token authentication. The SPA never sees OIDC tokens.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"kowhai/internal/service"
	"kowhai/internal/store"
)

const (
	SessionCookie = "kowhai_session"
	CSRFCookie    = "kowhai_csrf"
	CSRFHeader    = "X-Kowhai-CSRF"

	sessionTTL     = 30 * 24 * time.Hour
	touchEvery     = 5 * time.Minute
	loginStateTTL  = 10 * time.Minute
	providerRetry  = 30 * time.Second
	providerTimout = 10 * time.Second
)

// Config holds the OIDC client settings.
type Config struct {
	BaseURL      string // public URL of Kōwhai, e.g. https://kowhai.example.com
	Issuer       string
	ClientID     string
	ClientSecret string
	// HTTPClient is used for calls to the provider. Tests and dev set ups
	// with private CAs pass their own.
	HTTPClient *http.Client
}

type Auth struct {
	cfg    Config
	svc    *service.Service
	db     *store.DB
	log    *slog.Logger
	secure bool
	base   *url.URL

	mu         sync.Mutex
	provider   *oidc.Provider
	verifier   *oidc.IDTokenVerifier
	endSession string
	lastTry    time.Time
}

func New(cfg Config, svc *service.Service, log *slog.Logger) (*Auth, error) {
	u, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("KOWHAI_BASE_URL must be an absolute URL")
	}
	a := &Auth{cfg: cfg, svc: svc, db: svc.DB, log: log, secure: u.Scheme == "https", base: u}
	return a, nil
}

func (a *Auth) ctx(ctx context.Context) context.Context {
	if a.cfg.HTTPClient != nil {
		return oidc.ClientContext(ctx, a.cfg.HTTPClient)
	}
	return ctx
}

// Discover loads the provider metadata. It is retried on demand, so the
// app starts even when the provider is briefly down.
func (a *Auth) Discover(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.discoverLocked(ctx)
}

func (a *Auth) discoverLocked(ctx context.Context) error {
	if a.provider != nil {
		return nil
	}
	if a.cfg.Issuer == "" {
		return errors.New("KOWHAI_OIDC_ISSUER is not set")
	}
	if time.Since(a.lastTry) < providerRetry && !a.lastTry.IsZero() {
		return errors.New("identity provider unavailable")
	}
	a.lastTry = time.Now()
	ctx, cancel := context.WithTimeout(a.ctx(ctx), providerTimout)
	defer cancel()
	p, err := oidc.NewProvider(ctx, a.cfg.Issuer)
	if err != nil {
		return fmt.Errorf("oidc discovery: %w", err)
	}
	var extra struct {
		EndSession string   `json:"end_session_endpoint"`
		Algs       []string `json:"id_token_signing_alg_values_supported"`
	}
	_ = p.Claims(&extra)
	a.provider = p
	a.endSession = extra.EndSession
	vc := &oidc.Config{ClientID: a.cfg.ClientID}
	if len(extra.Algs) > 0 {
		// Kanidm signs with ES256 by default, Authelia with RS256.
		vc.SupportedSigningAlgs = extra.Algs
	}
	a.verifier = p.Verifier(vc)
	return nil
}

func (a *Auth) oauth() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     a.cfg.ClientID,
		ClientSecret: a.cfg.ClientSecret,
		Endpoint:     a.provider.Endpoint(),
		RedirectURL:  a.base.String() + "/auth/callback",
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "groups"},
	}
}

// Routes registers /auth/login, /auth/callback and /auth/logout.
func (a *Auth) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", a.login)
	mux.HandleFunc("GET /auth/callback", a.callback)
	mux.HandleFunc("POST /auth/logout", a.logout)
}

func safeReturn(s string) string {
	if s == "" || !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/\\") {
		return "/"
	}
	return s
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := a.Discover(ctx); err != nil {
		a.log.Error("oidc", "err", err)
		http.Error(w, "The sign-in provider is not reachable. Try again shortly.", http.StatusServiceUnavailable)
		return
	}
	state := service.RandomString(32)
	nonce := service.RandomString(32)
	verifier := oauth2.GenerateVerifier()
	now := time.Now().UTC()
	if err := store.New(a.db.Write).CreateOIDCState(ctx, store.CreateOIDCStateParams{
		State: state, Nonce: nonce, CodeVerifier: verifier, ReturnTo: safeReturn(r.URL.Query().Get("return_to")),
		ExpiresAt: now.Add(loginStateTTL), CreatedAt: now,
	}); err != nil {
		a.log.Error("save login state", "err", err)
		http.Error(w, "Sign-in failed", http.StatusInternalServerError)
		return
	}
	u := a.oauth().AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, u, http.StatusFound)
}

type claims struct {
	Subject           string   `json:"sub"`
	Email             string   `json:"email"`
	EmailVerified     *bool    `json:"email_verified"`
	Name              string   `json:"name"`
	PreferredUsername string   `json:"preferred_username"`
	Picture           string   `json:"picture"`
	Groups            []string `json:"groups"`
	Nonce             string   `json:"nonce"`
}

func (c *claims) merge(o claims) {
	if c.Email == "" {
		c.Email, c.EmailVerified = o.Email, o.EmailVerified
	}
	if c.EmailVerified == nil {
		c.EmailVerified = o.EmailVerified
	}
	if c.Name == "" {
		c.Name = o.Name
	}
	if c.PreferredUsername == "" {
		c.PreferredUsername = o.PreferredUsername
	}
	if c.Picture == "" {
		c.Picture = o.Picture
	}
	if len(c.Groups) == 0 {
		c.Groups = o.Groups
	}
}

func (a *Auth) callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	fail := func(status int, msg string, err error) {
		if err != nil {
			a.log.Warn("login failed", "reason", msg, "err", err)
		}
		http.Error(w, msg, status)
	}
	if e := r.URL.Query().Get("error"); e != "" {
		fail(http.StatusUnauthorized, "Sign-in was cancelled or refused: "+e, nil)
		return
	}
	if err := a.Discover(ctx); err != nil {
		fail(http.StatusServiceUnavailable, "The sign-in provider is not reachable", err)
		return
	}
	st, err := store.New(a.db.Write).TakeOIDCState(ctx, r.URL.Query().Get("state"))
	if err != nil || st.ExpiresAt.Before(time.Now()) {
		fail(http.StatusBadRequest, "Sign-in link expired. Start again.", err)
		return
	}
	tok, err := a.oauth().Exchange(a.ctx(ctx), r.URL.Query().Get("code"), oauth2.VerifierOption(st.CodeVerifier))
	if err != nil {
		fail(http.StatusUnauthorized, "Sign-in failed", err)
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		fail(http.StatusUnauthorized, "Sign-in failed", errors.New("no id_token"))
		return
	}
	idt, err := a.verifier.Verify(a.ctx(ctx), raw)
	if err != nil {
		fail(http.StatusUnauthorized, "Sign-in failed", err)
		return
	}
	var c claims
	if err := idt.Claims(&c); err != nil {
		fail(http.StatusUnauthorized, "Sign-in failed", err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(c.Nonce), []byte(st.Nonce)) != 1 {
		fail(http.StatusUnauthorized, "Sign-in failed", errors.New("nonce mismatch"))
		return
	}
	// Authelia and others put profile claims in userinfo only.
	if ui, err := a.provider.UserInfo(a.ctx(ctx), oauth2.StaticTokenSource(tok)); err == nil {
		var uc claims
		if err := ui.Claims(&uc); err == nil && (uc.Subject == "" || uc.Subject == idt.Subject) {
			c.merge(uc)
		}
	}
	name := c.Name
	if name == "" {
		name = c.PreferredUsername
	}
	user, err := a.svc.LoginUser(ctx, service.LoginInfo{
		Issuer: idt.Issuer, Subject: idt.Subject, Email: strings.ToLower(c.Email),
		EmailVerified: c.EmailVerified != nil && *c.EmailVerified,
		Name:          name, Picture: c.Picture, Groups: c.Groups,
	})
	if err != nil {
		if errors.Is(err, service.ErrDisabled) {
			fail(http.StatusForbidden, "Your account is disabled.", nil)
			return
		}
		fail(http.StatusInternalServerError, "Sign-in failed", err)
		return
	}
	if err := a.startSession(w, r, user.ID); err != nil {
		fail(http.StatusInternalServerError, "Sign-in failed", err)
		return
	}
	http.Redirect(w, r, st.ReturnTo, http.StatusFound)
}

func hashSession(v string) string {
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}

func (a *Auth) startSession(w http.ResponseWriter, r *http.Request, userID string) error {
	val := service.RandomString(43)
	now := time.Now().UTC()
	ua := r.UserAgent()
	if len(ua) > 300 {
		ua = ua[:300]
	}
	if err := store.New(a.db.Write).CreateSession(r.Context(), store.CreateSessionParams{
		ID: hashSession(val), UserID: userID, ExpiresAt: now.Add(sessionTTL), LastSeenAt: now, UserAgent: ua, CreatedAt: now,
	}); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: val, Path: "/", HttpOnly: true, Secure: a.secure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
	a.setCSRF(w)
	return nil
}

func (a *Auth) setCSRF(w http.ResponseWriter) string {
	v := service.RandomString(32)
	http.SetCookie(w, &http.Cookie{
		Name: CSRFCookie, Value: v, Path: "/", HttpOnly: false, Secure: a.secure, SameSite: http.SameSiteLaxMode,
		MaxAge: int(sessionTTL.Seconds()),
	})
	return v
}

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		_ = store.New(a.db.Write).DeleteSession(r.Context(), hashSession(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode})
	redirect := "/"
	a.mu.Lock()
	end := a.endSession
	a.mu.Unlock()
	// Kanidm has no end-session endpoint, so logout stays local there.
	if end != "" {
		q := url.Values{"client_id": {a.cfg.ClientID}, "post_logout_redirect_uri": {a.base.String() + "/"}}
		sep := "?"
		if strings.Contains(end, "?") {
			sep = "&"
		}
		redirect = end + sep + q.Encode()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"redirect": redirect})
}

// Middleware puts the actor on the request context when the request has
// a valid bearer token or session cookie. It rejects cookie requests that
// change state without the CSRF header. It does not reject anonymous
// requests: the service layer does that.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if h := r.Header.Get("Authorization"); h != "" {
			tok, ok := strings.CutPrefix(h, "Bearer ")
			if !ok || !service.IsPAT(strings.TrimSpace(tok)) {
				unauthorized(w, "unsupported Authorization header")
				return
			}
			actor, err := a.svc.ActorForToken(ctx, strings.TrimSpace(tok))
			if err != nil {
				unauthorized(w, "invalid or revoked token")
				return
			}
			next.ServeHTTP(w, r.WithContext(service.WithActor(ctx, actor)))
			return
		}
		csrf := ""
		if c, err := r.Cookie(CSRFCookie); err == nil {
			csrf = c.Value
		} else {
			csrf = a.setCSRF(w)
		}
		c, err := r.Cookie(SessionCookie)
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		actor, err := a.sessionActor(ctx, c.Value)
		if err != nil {
			http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secure})
			next.ServeHTTP(w, r)
			return
		}
		if !safeMethod(r.Method) {
			got := r.Header.Get(CSRFHeader)
			if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(csrf)) != 1 {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"status":403,"title":"Forbidden","detail":"missing or wrong CSRF header"}`))
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(service.WithActor(ctx, actor)))
	})
}

func unauthorized(w http.ResponseWriter, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="kowhai"`)
	w.WriteHeader(http.StatusUnauthorized)
	b, _ := json.Marshal(map[string]any{"status": 401, "title": "Not signed in", "detail": detail})
	_, _ = w.Write(b)
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

func (a *Auth) sessionActor(ctx context.Context, val string) (*service.Actor, error) {
	id := hashSession(val)
	s, err := a.db.R.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if s.ExpiresAt.Before(now) {
		return nil, errors.New("expired")
	}
	u, err := a.db.R.GetUser(ctx, s.UserID)
	if err != nil || u.DisabledAt.Valid {
		return nil, errors.New("user gone")
	}
	if now.Sub(s.LastSeenAt) > touchEvery {
		_ = store.New(a.db.Write).TouchSession(ctx, store.TouchSessionParams{ID: id, LastSeenAt: now, ExpiresAt: now.Add(sessionTTL)})
	}
	return &service.Actor{User: u}, nil
}

// Ready reports whether the provider metadata has loaded.
func (a *Auth) Ready() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.provider != nil
}
