package server

import (
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/edward-murrell/kowhai/internal/api"
)

// limitAPI rate-limits the API and MCP endpoints, leaving SSE streams and
// static files alone.
func limitAPI(rl *api.RateLimiter, next http.Handler) http.Handler {
	limited := rl.Middleware(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if (strings.HasPrefix(p, "/api/") || p == "/mcp" || strings.HasPrefix(p, "/auth/")) && !strings.HasSuffix(p, "/events") {
			limited.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		if !strings.HasPrefix(r.URL.Path, "/api/docs") && !strings.HasPrefix(r.URL.Path, "/api/v1/attachments/") {
			h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: https:; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		}
		next.ServeHTTP(w, r)
	})
}

// realIP takes the client address from X-Forwarded-For, but only when the
// direct peer is a trusted proxy.
func realIP(trusted []netip.Prefix, next http.Handler) http.Handler {
	isTrusted := func(a netip.Addr) bool {
		for _, p := range trusted {
			if p.Contains(a.Unmap()) {
				return true
			}
		}
		return false
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		peer, perr := netip.ParseAddr(host)
		if err == nil && perr == nil && len(trusted) > 0 && isTrusted(peer) {
			// Walk the chain right to left and stop at the first untrusted hop.
			hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
			for i := len(hops) - 1; i >= 0; i-- {
				a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
				if err != nil {
					break
				}
				r.RemoteAddr = net.JoinHostPort(a.String(), "0")
				if !isTrusted(a) {
					break
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	n      int64
}

func (s *statusWriter) WriteHeader(c int) { s.status = c; s.ResponseWriter.WriteHeader(c) }
func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = 200
	}
	n, err := s.ResponseWriter.Write(b)
	s.n += int64(n)
	return n, err
}
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func accessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || strings.HasPrefix(r.URL.Path, "/assets/") {
			return
		}
		log.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.status, "bytes", sw.n,
			"ms", time.Since(start).Milliseconds(), "ip", r.RemoteAddr)
	})
}

func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Error("panic", "value", v, "path", r.URL.Path)
				api.WriteProblem(w, 500, "Internal error", "")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
