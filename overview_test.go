package main

import (
	"net/http"
	"testing"

	"ire/contract"
)

// TestOverviewWaitPercentile covers the nearest-rank picks behind the wait readout.
func TestOverviewWaitPercentile(t *testing.T) {
	if got := overviewWaitPercentile(nil, 50); got != 0 {
		t.Errorf("empty p50 = %d, want 0", got)
	}

	if got := overviewWaitPercentile([]int64{42}, 95); got != 42 {
		t.Errorf("single p95 = %d, want 42", got)
	}

	sorted := []int64{10, 20, 30, 40}
	if got := overviewWaitPercentile(sorted, 50); got != 20 {
		t.Errorf("p50 = %d, want 20", got)
	}

	if got := overviewWaitPercentile(sorted, 95); got != 40 {
		t.Errorf("p95 = %d, want 40", got)
	}

	if got := overviewWaitPercentile(sorted, 100); got != 40 {
		t.Errorf("p100 = %d, want 40", got)
	}
}

// TestOverviewMedian covers the middle-value picks behind the turnaround panel.
func TestOverviewMedian(t *testing.T) {
	if got := overviewMedian(nil); got != 0 {
		t.Errorf("empty median = %d, want 0", got)
	}

	if got := overviewMedian([]int64{7}); got != 7 {
		t.Errorf("single median = %d, want 7", got)
	}

	if got := overviewMedian([]int64{10, 20, 30}); got != 20 {
		t.Errorf("odd median = %d, want 20", got)
	}

	if got := overviewMedian([]int64{10, 20, 30, 40}); got != 25 {
		t.Errorf("even median = %d, want 25", got)
	}
}

// TestOverview covers the programme health readout: counts, wait percentiles and series shapes.
func TestOverview(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	guest := do(t, application, http.MethodGet, testBase+"/overview", nil, nil, nil)
	if guest.Code != http.StatusUnauthorized {
		t.Fatalf("guest overview = %d, want 401", guest.Code)
	}

	response := decode[contract.OverviewResponse](t, do(t, application, http.MethodGet, testBase+"/overview", nil, cookie, nil))

	if response.NeedsReview <= 0 {
		t.Fatalf("needsReview = %d, want fixtures open", response.NeedsReview)
	}

	if response.Wait.Count != response.NeedsReview {
		t.Errorf("wait count = %d, want %d", response.Wait.Count, response.NeedsReview)
	}

	if response.Wait.P50MS > response.Wait.P95MS || response.Wait.P95MS > response.Wait.MaxMS {
		t.Errorf("percentiles out of order: p50=%d p95=%d max=%d", response.Wait.P50MS, response.Wait.P95MS, response.Wait.MaxMS)
	}

	if response.Wait.OldestID == nil || response.Wait.OldestTitle == nil {
		t.Error("open queue should name its oldest ship")
	}

	if len(response.ByStatus) == 0 {
		t.Error("byStatus should list the open statuses")
	}

	if len(response.Daily) != 14 {
		t.Errorf("daily = %d days, want 14", len(response.Daily))
	}

	ingested := 0
	for _, day := range response.Daily {
		ingested += day.Ingested
	}

	if ingested < response.NeedsReview {
		t.Errorf("ingested flow = %d, want at least %d open ships", ingested, response.NeedsReview)
	}

	if response.Turnaround.Decisions != 0 {
		t.Errorf("turnaround decisions = %d, want 0 without reviews", response.Turnaround.Decisions)
	}

	if len(response.Outcomes) != 0 {
		t.Errorf("outcomes = %d rows, want 0 without reviews", len(response.Outcomes))
	}
}
