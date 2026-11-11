package api

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"kowhai/internal/service"
)

// RateLimiter limits requests per token, per user or per client IP.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rps     rate.Limit
	burst   int
}

type bucket struct {
	lim  *rate.Limiter
	seen time.Time
}

func NewRateLimiter(rps float64, burst int) *RateLimiter {
	rl := &RateLimiter{buckets: map[string]*bucket{}, rps: rate.Limit(rps), burst: burst}
	go rl.sweep()
	return rl
}

func (rl *RateLimiter) sweep() {
	for range time.Tick(5 * time.Minute) {
		rl.mu.Lock()
		for k, b := range rl.buckets {
			if time.Since(b.seen) > 10*time.Minute {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *RateLimiter) allow(key string) bool {
	rl.mu.Lock()
	b := rl.buckets[key]
	if b == nil {
		b = &bucket{lim: rate.NewLimiter(rl.rps, rl.burst)}
		rl.buckets[key] = b
	}
	b.seen = time.Now()
	rl.mu.Unlock()
	return b.lim.Allow()
}

// Middleware applies the limit. Run it after the auth middleware so the
// actor is known. SSE streams are long-lived and only count once.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "ip:" + clientIP(r)
		if a := service.ActorFrom(r.Context()); a != nil {
			key = "user:" + a.User.ID
			if a.Token != nil {
				key = "token:" + a.Token.ID
			}
		}
		if !rl.allow(key) {
			w.Header().Set("Retry-After", strconv.Itoa(1))
			WriteProblem(w, http.StatusTooManyRequests, "Too many requests", "slow down and try again")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
