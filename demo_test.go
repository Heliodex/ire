package main

import (
	"context"
	"net/http"
	"testing"
)

// TestDemoResult covers the status mapping without a public TLS host.
func TestDemoResult(t *testing.T) {
	pass := demoResult(http.StatusOK, "now")
	if pass.Status != statusPass || pass.Source == nil || *pass.Source != "probe" {
		t.Fatalf("200 = %+v", pass)
	}

	blocked := demoResult(http.StatusForbidden, "now")
	if blocked.Status != statusWarn || blocked.Severity != severityWarn {
		t.Fatalf("403 = %+v", blocked)
	}

	missing := demoResult(http.StatusNotFound, "now")
	if missing.Status != statusWarn {
		t.Fatalf("404 = %+v", missing)
	}
}

// TestSafeDemoURL rejects local and plain-HTTP targets.
func TestSafeDemoURL(t *testing.T) {
	for _, value := range []string{"http://example.com", "https://localhost/demo", "https://127.0.0.1/demo", "https://192.168.1.2/demo"} {
		if _, err := safeDemoURL(context.Background(), value); err == nil {
			t.Errorf("%s was accepted", value)
		}
	}

	if _, err := safeDemoURL(context.Background(), "https://example.com/demo"); err != nil {
		t.Errorf("public URL rejected: %v", err)
	}
}

// TestDemoChecksSkipsMissingURL covers the no-demo path.
func TestDemoChecksSkipsMissingURL(t *testing.T) {
	application := newTestApp(t)

	checks := application.demoChecks(t.Context(), submissionRow{ID: "sub_fixture_1"})
	if len(checks) != 1 || checks[0].Status != statusSkip {
		t.Fatalf("checks = %+v", checks)
	}
}
