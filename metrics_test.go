package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestMetrics covers the machine guard and the main counters.
func TestMetrics(t *testing.T) {
	application := newTestApp(t)

	if unauthorized := do(t, application, http.MethodGet, "/metrics", nil, nil, nil); unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized = %d: %s", unauthorized.Code, unauthorized.Body.String())
	}

	recorder := do(t, application, http.MethodGet, "/metrics", nil, nil, map[string]string{"Authorization": "Bearer test-ingest"})
	if recorder.Code != http.StatusOK || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("metrics = %d %q", recorder.Code, recorder.Header().Get("Content-Type"))
	}

	body := recorder.Body.String()

	for _, expected := range []string{
		"review_queue_open_total 3",
		"review_queue_depth{stage=\"1\",status=\"ready\"}",
		"review_hold_rate 0.0000",
		"review_delivery_total",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("missing %q in:\n%s", expected, body)
		}
	}
}
