package main

import (
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	"review/contract"
)

// TestJournalAdjustmentSavesDeflation covers the per-entry approved time, its note and the ship total it feeds.
func TestJournalAdjustmentSavesDeflation(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, reviewer, nil))
	if len(bundle.Journals) == 0 {
		t.Fatal("fixture has no journal entries")
	}

	entry := bundle.Journals[0]
	approved := entry.Seconds / 2

	body, _ := json.Marshal(contract.JournalReviewRequest{
		ApprovedSeconds: approved,
		ReviewerNote:    "Schematic work, the layout is not finished.",
		Version:         1,
	})

	adjusted := do(t, application, http.MethodPatch, testBase+"/submissions/sub_fixture_1/journal/"+entry.ID, body, reviewer, nil)
	if adjusted.Code != http.StatusOK {
		t.Fatalf("journal adjustment = %d: %s", adjusted.Code, adjusted.Body.String())
	}

	result := decode[contract.JournalAdjustment](t, adjusted)
	if result.Journal.ApprovedSeconds == nil || *result.Journal.ApprovedSeconds != approved {
		t.Errorf("approved seconds = %v, want %d", result.Journal.ApprovedSeconds, approved)
	}
	if result.Journal.ReviewerNote == nil || *result.Journal.ReviewerNote == "" {
		t.Errorf("reviewer note = %v", result.Journal.ReviewerNote)
	}

	expected := approved
	for _, other := range bundle.Journals[1:] {
		expected += other.Seconds
	}
	if result.HoursApprovedSeconds != expected {
		t.Errorf("total = %d, want %d", result.HoursApprovedSeconds, expected)
	}

	reloaded := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, reviewer, nil))
	if reloaded.Submission.HoursApprovedSeconds == nil || *reloaded.Submission.HoursApprovedSeconds != expected {
		t.Errorf("submission hours = %v, want %d", reloaded.Submission.HoursApprovedSeconds, expected)
	}
	if fresh := reloaded.Journals[0]; fresh.ApprovedSeconds == nil || *fresh.ApprovedSeconds != approved {
		t.Errorf("reloaded entry = %+v", fresh)
	}
}

// TestJournalAdjustmentRequiresClaimAndVersion covers the guards on the per-entry edit.
func TestJournalAdjustmentRequiresClaimAndVersion(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, reviewer, nil))
	entry := bundle.Journals[0]

	body, _ := json.Marshal(contract.JournalReviewRequest{ApprovedSeconds: entry.Seconds, Version: 1})
	path := testBase + "/submissions/sub_fixture_1/journal/" + entry.ID

	if unclaimed := do(t, application, http.MethodPatch, path, body, reviewer, nil); unclaimed.Code != http.StatusConflict {
		t.Fatalf("unclaimed = %d: %s", unclaimed.Code, unclaimed.Body.String())
	}

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	staleBody, _ := json.Marshal(contract.JournalReviewRequest{ApprovedSeconds: entry.Seconds, Version: 99})
	stale := do(t, application, http.MethodPatch, path, staleBody, reviewer, nil)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), "stale_version") {
		t.Fatalf("stale = %d: %s", stale.Code, stale.Body.String())
	}
}
