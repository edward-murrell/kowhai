// Package oidctest is a small OpenID Connect provider for tests and local
// development. It supports the authorisation code flow with PKCE, signs
// ID tokens with ES256 (like Kanidm) or RS256 (like Authelia), and lets
// the user pick who to sign in as. Never run it in production.
package oidctest

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// User is someone who can sign in.
type User struct {
	Subject string   `json:"sub"`
	Email   string   `json:"email"`
	Name    string   `json:"name"`
	Groups  []string `json:"groups"`
}

type Provider struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// Alg is ES256 or RS256.
	Alg   string
	Users []User
	// EndSession turns on the end_session_endpoint.
	EndSession bool

	key   crypto.Signer
	keyID string

	mu    sync.Mutex
	codes map[string]grant
	toks  map[string]User
}

type grant struct {
	user      User
	nonce     string
	challenge string
	redirect  string
	expires   time.Time
}

// New makes a provider. issuer must be the URL it will be served at.
func New(issuer, clientID, secret, alg string, users []User) (*Provider, error) {
	p := &Provider{Issuer: strings.TrimRight(issuer, "/"), ClientID: clientID, ClientSecret: secret, Alg: alg, Users: users,
		codes: map[string]grant{}, toks: map[string]User{}, keyID: "k1"}
	var err error
	switch alg {
	case "ES256":
		p.key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case "RS256", "":
		p.Alg = "RS256"
		p.key, err = rsa.GenerateKey(rand.Reader, 2048)
	default:
		return nil, fmt.Errorf("unsupported alg %s", alg)
	}
	return p, err
}

func rnd() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (p *Provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, mustPath(p.Issuer))
	switch path {
	case "/.well-known/openid-configuration":
		p.discovery(w)
	case "/jwks":
		p.jwks(w)
	case "/authorize":
		p.authorize(w, r)
	case "/token":
		p.token(w, r)
	case "/userinfo":
		p.userinfo(w, r)
	case "/logout":
		http.Redirect(w, r, r.URL.Query().Get("post_logout_redirect_uri"), http.StatusFound)
	default:
		http.NotFound(w, r)
	}
}

func mustPath(issuer string) string {
	u, _ := url.Parse(issuer)
	return u.Path
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (p *Provider) discovery(w http.ResponseWriter) {
	d := map[string]any{
		"issuer":                                p.Issuer,
		"authorization_endpoint":                p.Issuer + "/authorize",
		"token_endpoint":                        p.Issuer + "/token",
		"userinfo_endpoint":                     p.Issuer + "/userinfo",
		"jwks_uri":                              p.Issuer + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{p.Alg},
		"code_challenge_methods_supported":      []string{"S256"},
		"scopes_supported":                      []string{"openid", "profile", "email", "groups"},
	}
	if p.EndSession {
		d["end_session_endpoint"] = p.Issuer + "/logout"
	}
	writeJSON(w, d)
}

func (p *Provider) jwks(w http.ResponseWriter) {
	writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: p.key.Public(), KeyID: p.keyID, Algorithm: p.Alg, Use: "sig"}}})
}

var pickPage = template.Must(template.New("pick").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Dev sign-in</title>
<style>body{font-family:system-ui;max-width:28rem;margin:4rem auto}button{display:block;width:100%;margin:.5rem 0;padding:.75rem;font-size:1rem}</style>
</head><body><h1>Dev sign-in</h1><p>Pick a user. This provider is for development only.</p>
<form method="post">{{range .}}<button name="sub" value="{{.Subject}}">{{.Name}} &lt;{{.Email}}&gt;</button>{{end}}</form>
</body></html>`))

func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	q := r.URL.Query()
	if q.Get("client_id") != p.ClientID {
		http.Error(w, "unknown client", http.StatusBadRequest)
		return
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		// Kanidm insists on PKCE with S256. So do we.
		http.Error(w, "PKCE S256 required", http.StatusBadRequest)
		return
	}
	sub := r.PostForm.Get("sub")
	if sub == "" {
		sub = q.Get("login_hint")
	}
	if sub == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pickPage.Execute(w, p.Users)
		return
	}
	var user *User
	for i := range p.Users {
		if p.Users[i].Subject == sub || p.Users[i].Email == sub {
			user = &p.Users[i]
		}
	}
	if user == nil {
		http.Error(w, "unknown user", http.StatusBadRequest)
		return
	}
	code := rnd()
	p.mu.Lock()
	p.codes[code] = grant{user: *user, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), expires: time.Now().Add(time.Minute)}
	p.mu.Unlock()
	u, _ := url.Parse(q.Get("redirect_uri"))
	v := u.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	u.RawQuery = v.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != p.ClientID || secret != p.ClientSecret {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]string{"error": "invalid_client"})
		return
	}
	code := r.PostForm.Get("code")
	p.mu.Lock()
	g, found := p.codes[code]
	delete(p.codes, code)
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !found || g.expires.Before(time.Now()) || g.redirect != r.PostForm.Get("redirect_uri") ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant"})
		return
	}
	now := time.Now()
	idt, err := p.sign(map[string]any{
		"iss": p.Issuer, "sub": g.user.Subject, "aud": p.ClientID, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"nonce": g.nonce, "email": g.user.Email, "email_verified": true, "name": g.user.Name, "groups": g.user.Groups,
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	at := rnd()
	p.mu.Lock()
	p.toks[at] = g.user
	p.mu.Unlock()
	writeJSON(w, map[string]any{"access_token": at, "token_type": "Bearer", "expires_in": 300, "id_token": idt})
}

func (p *Provider) userinfo(w http.ResponseWriter, r *http.Request) {
	at := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	p.mu.Lock()
	u, ok := p.toks[at]
	p.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{"sub": u.Subject, "email": u.Email, "email_verified": true, "name": u.Name, "groups": u.Groups})
}

func (p *Provider) sign(claims map[string]any) (string, error) {
	alg := jose.SignatureAlgorithm(p.Alg)
	s, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: jose.JSONWebKey{Key: p.key, KeyID: p.keyID}}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(claims)
	obj, err := s.Sign(b)
	if err != nil {
		return "", err
	}
	return obj.CompactSerialize()
}
