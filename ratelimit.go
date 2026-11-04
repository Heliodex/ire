package main

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
)

const (
	browserRateLimit = 120
	machineRateLimit = 240
	rateLimitWindow  = time.Minute
)

// rateLimiter keeps a sliding window of request times per key. The app runs one instance, and the mutex keeps tests and future parallel handlers safe.
type rateLimiter struct {
	mu    sync.Mutex
	limit int
	now   func() time.Time
	hits  map[string][]time.Time
}

// newRateLimiter builds a limiter with the shared one-minute window.
func newRateLimiter(limit int, now func() time.Time) *rateLimiter {
	return &rateLimiter{limit: limit, now: now, hits: map[string][]time.Time{}}
}

// allow records a hit and reports whether the key is still inside its limit.
func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-rateLimitWindow)
	recent := make([]time.Time, 0, len(l.hits[key]))

	for _, hit := range l.hits[key] {
		if hit.After(cutoff) {
			recent = append(recent, hit)
		}
	}

	if len(recent) >= l.limit {
		l.hits[key] = recent
		return false
	}

	l.hits[key] = append(recent, now)

	return true
}

// withRateLimit rejects a burst of mutations before it reaches storage. Reads and the supervised job endpoints stay unlimited.
func (a *reviewApp) withRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)

			return
		}

		key, limiter := a.rateKey(r)
		if limiter != nil && !limiter.allow(key) {
			w.Header().Set("Retry-After", "60")
			app.WriteError(w, http.StatusTooManyRequests, "too many requests; try again shortly")

			return
		}

		next.ServeHTTP(w, r)
	})
}

// rateKey picks the machine or browser bucket and a key that does not retain a raw session token.
func (a *reviewApp) rateKey(r *http.Request) (string, *rateLimiter) {
	if strings.HasPrefix(r.URL.Path, "/api/ingest/") || strings.HasPrefix(r.URL.Path, "/api/v1/ingest/") || strings.HasPrefix(r.URL.Path, "/api/v1/withdraw/") {
		return remoteHost(r), a.machineLimiter
	}

	if token, ok := reviewSessions.Token(r); ok && token != "" {
		return sha256Hex([]byte(token)), a.browserLimiter
	}

	return remoteHost(r), a.browserLimiter
}

// remoteHost is the address the runtime reports; wasm deployments terminate the browser connection at the supervisor.
func remoteHost(r *http.Request) string {
	host := r.RemoteAddr
	if index := strings.LastIndex(host, ":"); index >= 0 {
		host = host[:index]
	}

	if host == "" {
		return "unknown"
	}

	return host
}
