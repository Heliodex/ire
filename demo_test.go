package main

import (
	"context"
	"net/http"
	"testing"
)

// TestDemoChecksSkipsMissingURL covers the no-demo path.
func TestDemoChecksSkipsMissingURL(t *testing.T) {
	application := newTestApp(t)

	if checks := application.demoChecks(t.Context(), submissionRow{
		ID: "sub_fixture_1",
	}); len(checks) != 1 || checks[0].Status != statusSkip {
		t.Fatalf("checks = %+v", checks)
	}
}

// TestDemoResult covers the status mapping without a public TLS host.
func TestDemoResult(t *testing.T) {
	if pass := demoResult(http.StatusOK, "now"); pass.Status != statusPass || pass.Source == nil || *pass.Source != "probe" {
		t.Fatalf("200 = %+v", pass)
	}

	if blocked := demoResult(http.StatusForbidden, "now"); blocked.Status != statusWarn || blocked.Severity != severityWarn {
		t.Fatalf("403 = %+v", blocked)
	}

	if missing := demoResult(http.StatusNotFound, "now"); missing.Status != statusWarn {
		t.Fatalf("404 = %+v", missing)
	}
}

// TestSafeDemoURL rejects local and plain-HTTP targets.
func TestSafeDemoURL(t *testing.T) {
	for _, value := range []string{
		"http://example.com",
		"https://localhost/demo",
		"https://127.0.0.1/demo",
		"https://192.168.1.2/demo",
	} {
		if _, err := safeDemoURL(context.Background(), value); err == nil {
			t.Errorf("%s was accepted", value)
		}
	}

	if _, err := safeDemoURL(context.Background(), "https://example.com/demo"); err != nil {
		t.Errorf("public URL rejected: %v", err)
	}
}
