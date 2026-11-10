package main

import (
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	"ire/contract"
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
	reason := "idle"

	body, _ := json.Marshal(contract.JournalReviewRequest{
		ApprovedSeconds: approved,
		DeflationReason: &reason,
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

	if result.Journal.DeflationReason == nil || *result.Journal.DeflationReason != reason {
		t.Errorf("deflation reason = %v, want %q", result.Journal.DeflationReason, reason)
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

// TestJournalCutRequiresReason covers the reason requirement when an entry is deflated.
func TestJournalCutRequiresReason(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, reviewer, nil))
	entry := bundle.Journals[0]
	path := testBase + "/submissions/sub_fixture_1/journal/" + entry.ID

	body, _ := json.Marshal(contract.JournalReviewRequest{ApprovedSeconds: entry.Seconds / 2, Version: 1})

	refused := do(t, application, http.MethodPatch, path, body, reviewer, nil)
	if refused.Code != http.StatusUnprocessableEntity {
		t.Fatalf("cut without reason = %d: %s", refused.Code, refused.Body.String())
	}

	if problem := decode[contract.ErrorResponse](t, refused); problem.Code != "deflation_reason_required" || problem.Field == nil || *problem.Field != "deflationReason" {
		t.Fatalf("problem = %+v", problem)
	}

	reason := "tutorial"
	body, _ = json.Marshal(contract.JournalReviewRequest{ApprovedSeconds: entry.Seconds / 2, DeflationReason: &reason, Version: 1})

	accepted := do(t, application, http.MethodPatch, path, body, reviewer, nil)
	if accepted.Code != http.StatusOK {
		t.Fatalf("cut with reason = %d: %s", accepted.Code, accepted.Body.String())
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

// TestJournalMediaURLs exposes only safe image attachments for review display.
func TestJournalMediaURLs(t *testing.T) {
	media := journalMediaURLs(`[{"type":"image","url":"https://example.com/a.jpg","alt":"Board"},
		{"url":"https://example.com/b.jpg"},
		{"type":"video","url":"https://example.com/c.mp4"},
		{"type":"image","url":"javascript:alert(1)"},
		{"type":"image","url":"ftp://example.com/d.jpg"},
		{"type":"image"},
		{"type":"image","url":"  "}]`)

	if len(media) != 2 || media[0].URL != "https://example.com/a.jpg" {
		t.Fatalf("media = %+v", media)
	}

	if media[0].Alt == nil || *media[0].Alt != "Board" {
		t.Fatalf("alt = %+v", media[0])
	}

	if media[1].Alt != nil {
		t.Fatalf("missing alt should stay missing: %+v", media[1])
	}

	if got := journalMediaURLs("not json"); len(got) != 0 {
		t.Fatalf("malformed = %+v", got)
	}

	images := make([]string, 0, maxJournalMedia+6)
	images = append(images, "[{\"type\":\"image\",\"url\":\"https://example.com/x.jpg\"}")

	for range maxJournalMedia + 5 {
		images = append(images, "{\"type\":\"image\",\"url\":\"https://example.com/x.jpg\"}")
	}

	if got := journalMediaURLs(strings.Join(images, ",") + "]"); len(got) != maxJournalMedia {
		t.Fatalf("capped = %d", len(got))
	}
}

// TestJournalMediaReachesTheBundle loads the seeded attachments through the bundle path.
func TestJournalMediaReachesTheBundle(t *testing.T) {
	application := newTestApp(t)

	journals, err := application.loadJournals(t.Context(), "sub_fixture_1")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range journals {
		if len(entry.Media) > 0 {
			if entry.Media[0].URL != "https://example.com/journal.jpg" {
				t.Fatalf("media = %+v", entry.Media)
			}

			return
		}
	}

	t.Fatal("fixture journals carry no media")
}
