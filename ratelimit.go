package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

const (
	browserRateLimit = 120
	machineRateLimit = 240
	rateLimitWindow  = time.Minute
)

// The limits live in the supervisor's key-value store: wasm instances are per request and share no memory, so an in-process window would reset on every instance.
var (
	browserLimit = storage.RateLimit{Prefix: "rl:browser:", Limit: browserRateLimit, Window: rateLimitWindow}
	machineLimit = storage.RateLimit{Prefix: "rl:machine:", Limit: machineRateLimit, Window: rateLimitWindow}
)

// retryAfterSeconds rounds the wait up to whole seconds, at least one, for the Retry-After header.
func retryAfterSeconds(wait time.Duration) string {
	return strconv.Itoa(max(1, int((wait+time.Second-1)/time.Second)))
}

// withRateLimit rejects a burst of mutations before it reaches storage. Reads and the supervised job endpoints stay unlimited.
func (a *ireApp) withRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)

			return
		}

		key, limit := a.rateKey(r)

		decision, err := limit.Allow(r.Context(), a.store, key)
		if err != nil {
			// A storage outage must not lock every user out, so the request goes through unlimited.
			app.Errorf("rate limit: %v", err)
		} else if !decision.Allowed {
			w.Header().Set("Retry-After", retryAfterSeconds(decision.RetryAfter))
			app.WriteError(w, http.StatusTooManyRequests, "too many requests; try again shortly")

			return
		}

		next.ServeHTTP(w, r)
	})
}

// rateKey picks the machine or browser bucket and a key that does not retain a raw session token.
func (a *ireApp) rateKey(r *http.Request) (string, storage.RateLimit) {
	if strings.HasPrefix(r.URL.Path, "/api/ingest/") || strings.HasPrefix(r.URL.Path, "/api/v1/ingest/") || strings.HasPrefix(r.URL.Path, "/api/v1/withdraw/") {
		return remoteHost(r), a.machineLimit
	}

	if token, ok := reviewSessions.Token(r); ok && token != "" {
		return sha256Hex([]byte(token)), a.browserLimit
	}

	return remoteHost(r), a.browserLimit
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
