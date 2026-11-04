package main

import (
	"net/http"
	"testing"
	"time"
)

// TestRateLimiterWindow covers the sliding window and the reset after it.
func TestRateLimiterWindow(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(2, func() time.Time { return now })

	if !limiter.allow("key") {
		t.Fatal("first request was rejected")
	}

	if !limiter.allow("key") {
		t.Fatal("second request was rejected")
	}

	if limiter.allow("key") {
		t.Fatal("third request was allowed")
	}

	if !limiter.allow("other") {
		t.Fatal("another key was rejected")
	}

	now = now.Add(61 * time.Second)

	if !limiter.allow("key") {
		t.Fatal("request after the window was rejected")
	}
}

// TestBrowserMutationLimit covers the handler-level rejection and the machine bucket.
func TestBrowserMutationLimit(t *testing.T) {
	application := newTestApp(t)
	application.browserLimiter = newRateLimiter(2, application.now)

	cookie := login(t, application, "reviewer@example.com")
	body := []byte(`{"email":"reviewer@example.com"}`)

	for range 2 {
		if recorder := do(t, application, http.MethodPost, "/api/v1/dev/login", body, cookie, nil); recorder.Code == http.StatusTooManyRequests {
			t.Fatal("request inside the limit was rejected")
		}
	}

	limited := do(t, application, http.MethodPost, "/api/v1/dev/login", body, cookie, nil)
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") != "60" {
		t.Fatalf("limited = %d %q", limited.Code, limited.Header().Get("Retry-After"))
	}

	if read := do(t, application, http.MethodGet, "/api/v1/me", nil, cookie, nil); read.Code != http.StatusOK {
		t.Fatalf("read = %d: %s", read.Code, read.Body.String())
	}

	application.machineLimiter = newRateLimiter(1, application.now)

	first := do(t, application, http.MethodPost, "/api/ingest/half-life", ingestBody("rate-limit-a"), nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", ingestBody("rate-limit-a")),
	})
	if first.Code == http.StatusTooManyRequests {
		t.Fatal("first machine request was limited")
	}

	second := do(t, application, http.MethodPost, "/api/ingest/half-life", ingestBody("rate-limit-b"), nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", ingestBody("rate-limit-b")),
	})
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second machine request = %d: %s", second.Code, second.Body.String())
	}
}
