// Package server builds the HTTP handler: routes, middleware and the
// embedded web app.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/edward-murrell/kowhai/internal/api"
	"github.com/edward-murrell/kowhai/internal/auth"
	"github.com/edward-murrell/kowhai/internal/events"
	"github.com/edward-murrell/kowhai/internal/mcp"
	"github.com/edward-murrell/kowhai/internal/service"
)

// Pinger is something /readyz checks.
type Pinger interface {
	Ping(ctx context.Context) error
}

type Options struct {
	Service *service.Service
	Hub     *events.Hub
	Auth    *auth.Auth
	Log     *slog.Logger
	BaseURL string
	Version string
	// Proxies are trusted to set X-Forwarded-For.
	Proxies []netip.Prefix
	// RateLimit is requests per second per user or token. 0 turns it off.
	RateLimit int
	// Ready lists dependencies for /readyz.
	Ready map[string]Pinger
	// Extra routes, such as the dev OIDC provider.
	Extra map[string]http.Handler
}

// Handler returns the full HTTP handler.
func Handler(o Options) http.Handler {
	mux := http.NewServeMux()
	o.Auth.Routes(mux)
	api.Mount(mux, o.Service, &api.Streams{S: o.Service, Hub: o.Hub})
	mux.Handle("/mcp", mcp.Handler(o.Service, o.Version, o.BaseURL))
	for p, h := range o.Extra {
		mux.Handle(p, h)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		var errs []string
		for name, p := range o.Ready {
			if err := p.Ping(c); err != nil {
				errs = append(errs, name+": "+err.Error())
			}
		}
		if len(errs) > 0 {
			http.Error(w, strings.Join(errs, "\n"), http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/", spaHandler())

	var h http.Handler = mux
	if o.RateLimit > 0 {
		h = limitAPI(api.NewRateLimiter(float64(o.RateLimit), o.RateLimit*3), h)
	}
	h = o.Auth.Middleware(h)
	h = securityHeaders(h)
	h = metricsMiddleware(h)
	h = accessLog(o.Log, h)
	h = realIP(o.Proxies, h)
	h = recoverer(o.Log, h)
	return h
}
