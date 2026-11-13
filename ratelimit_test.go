package main

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

// TestRetryAfterSeconds rounds a partial second up and never asks for zero.
func TestRetryAfterSeconds(t *testing.T) {
	for wait, want := range map[time.Duration]string{0: "1", 1500 * time.Millisecond: "2", time.Minute: "60"} {
		if got := retryAfterSeconds(wait); got != want {
			t.Errorf("retryAfterSeconds(%v) = %s, want %s", wait, got, want)
		}
	}
}

// TestBrowserMutationLimit covers the handler-level rejection and the machine bucket.
func TestBrowserMutationLimit(t *testing.T) {
	application := newTestApp(t)
	application.browserLimit.Limit = 2

	cookie := login(t, application, "reviewer@example.com")
	body := []byte(`{"email":"reviewer@example.com"}`)

	for range 2 {
		if recorder := do(t, application, http.MethodPost, "/api/v1/dev/login", body, cookie, nil); recorder.Code == http.StatusTooManyRequests {
			t.Fatal("request inside the limit was rejected")
		}
	}

	limited := do(t, application, http.MethodPost, "/api/v1/dev/login", body, cookie, nil)
	if retry, _ := strconv.Atoi(limited.Header().Get("Retry-After")); limited.Code != http.StatusTooManyRequests || retry < 1 || retry > 60 {
		t.Fatalf("limited = %d %q", limited.Code, limited.Header().Get("Retry-After"))
	}

	if read := do(t, application, http.MethodGet, "/api/v1/me", nil, cookie, nil); read.Code != http.StatusOK {
		t.Fatalf("read = %d: %s", read.Code, read.Body.String())
	}

	application.machineLimit.Limit = 1

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
