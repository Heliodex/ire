package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"ire/contract"
)

// TestHackatimeChecks covers the public stats fetch, the five-minute cache and a stale fallback.
func TestHackatimeChecks(t *testing.T) {
	var (
		calls atomic.Int32
		fail  atomic.Bool
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)

		if fail.Load() {
			http.Error(w, "upstream", http.StatusInternalServerError)
			return
		}

		writeTestJSON(w, map[string]any{
			"data": map[string]any{"projects": []map[string]any{
				{"name": "Board", "total_seconds": 7200.0},
				{"name": "Firmware", "total_seconds": 1800.0},
			}},
		})
	}))
	t.Cleanup(server.Close)

	application := newTestApp(t)
	application.config.HackatimeAPIBase = server.URL

	if _, err := application.store.Exec(t.Context(), `insert into hackatime_link (submission_id, project) values (?, ?), (?, ?)`,
		"sub_fixture_1", "Board", "sub_fixture_1", "Firmware"); err != nil {
		t.Fatal(err)
	}

	ship := submissionRow{ID: "sub_fixture_1", MakerHackatim: "U1", HoursClaimed: 23040}

	checks := checkMap(application.hackatimeChecks(t.Context(), ship))

	if checks["hackatime.linked"].Status != statusPass || checks["hackatime.time"].Status != statusPass || checks["hackatime.stale"].Status != statusPass {
		t.Fatalf("checks = %+v", checks)
	}

	if checks["hackatime.time"].Summary != "2.50h tracked all-time" {
		t.Errorf("summary = %q", checks["hackatime.time"].Summary)
	}

	if calls.Load() != 2 {
		t.Errorf("fetches = %d, want 2", calls.Load())
	}

	// A second read inside five minutes uses the cache.
	checks = checkMap(application.hackatimeChecks(t.Context(), ship))
	if calls.Load() != 2 || checks["hackatime.stale"].Status != statusPass {
		t.Fatalf("cached checks = %+v, fetches = %d", checks, calls.Load())
	}

	// After the window, a failure keeps the cached value and reports it as stale.
	base := application.now()
	application.clock = func() time.Time { return base.Add(6 * time.Minute) }

	fail.Store(true)

	checks = checkMap(application.hackatimeChecks(t.Context(), ship))
	if checks["hackatime.stale"].Status != statusError || checks["hackatime.time"].Status != statusPass {
		t.Fatalf("stale checks = %+v", checks)
	}

	if checks["hackatime.time"].Summary != "2.50h tracked all-time" {
		t.Errorf("stale total = %q", checks["hackatime.time"].Summary)
	}
}

// TestHackatimeMissingIdentity covers the skip path rather than treating an unlinked maker as zero.
func TestHackatimeMissingIdentity(t *testing.T) {
	application := newTestApp(t)

	checks := checkMap(application.hackatimeChecks(t.Context(), submissionRow{ID: "sub_fixture_1"}))
	if checks["hackatime.linked"].Status != statusSkip {
		t.Fatalf("checks = %+v", checks)
	}
}

// checkMap indexes checks by key.
func checkMap(checks []contract.CheckResult) map[string]contract.CheckResult {
	out := make(map[string]contract.CheckResult, len(checks))

	for _, result := range checks {
		out[result.Key] = result
	}

	return out
}
