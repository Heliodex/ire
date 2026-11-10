package main

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	"ire/contract"
)

// setHackatimeSeconds gives a fixture ship Hackatime time to deflate.
func setHackatimeSeconds(t *testing.T, application *ireApp, submissionID string, seconds int) {
	t.Helper()

	if _, err := application.store.Exec(context.Background(), `update submission set hours_hackatime_seconds = ? where id = ?`, seconds, submissionID); err != nil {
		t.Fatal(err)
	}
}

// journalEntriesTotal sums the approved-or-claimed journal ledger the approved total follows.
func journalEntriesTotal(journals []contract.JournalEntry) int {
	total := 0

	for _, entry := range journals {
		if entry.ApprovedSeconds != nil {
			total += *entry.ApprovedSeconds
		} else {
			total += entry.Seconds
		}
	}

	return total
}

// TestHackatimeCutSavesLedger covers the Hackatime approved time, its reason and the ship total it feeds.
func TestHackatimeCutSavesLedger(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	setHackatimeSeconds(t, application, "sub_fixture_1", 3600)

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, reviewer, nil))
	if len(bundle.Adjustments) != 0 {
		t.Fatalf("adjustments = %+v, want none", bundle.Adjustments)
	}

	reason := "idle"
	body, _ := json.Marshal(contract.HackatimeReviewRequest{
		ApprovedSeconds: 1800,
		DeflationReason: &reason,
		ReviewerNote:    "Half the Hackatime window was a video left playing.",
		Version:         1,
	})

	adjusted := do(t, application, http.MethodPatch, testBase+"/submissions/sub_fixture_1/hackatime", body, reviewer, nil)
	if adjusted.Code != http.StatusOK {
		t.Fatalf("hackatime adjustment = %d: %s", adjusted.Code, adjusted.Body.String())
	}

	result := decode[contract.HackatimeAdjustment](t, adjusted)
	if result.Adjustment == nil {
		t.Fatal("adjustment is missing")
	}

	if result.Adjustment.Seconds != 1800 || result.Adjustment.Source != "hackatime" || result.Adjustment.Reason != reason {
		t.Errorf("adjustment = %+v", result.Adjustment)
	}

	expected := journalEntriesTotal(bundle.Journals) - 1800
	if result.HoursApprovedSeconds != expected {
		t.Errorf("total = %d, want %d", result.HoursApprovedSeconds, expected)
	}

	reloaded := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, reviewer, nil))
	if len(reloaded.Adjustments) != 1 || reloaded.Adjustments[0].Source != "hackatime" {
		t.Fatalf("reloaded adjustments = %+v", reloaded.Adjustments)
	}

	if reloaded.Submission.HoursApprovedSeconds == nil || *reloaded.Submission.HoursApprovedSeconds != expected {
		t.Errorf("submission hours = %v, want %d", reloaded.Submission.HoursApprovedSeconds, expected)
	}

	restoredBody, _ := json.Marshal(contract.HackatimeReviewRequest{ApprovedSeconds: 3600, Version: 1})

	restored := do(t, application, http.MethodPatch, testBase+"/submissions/sub_fixture_1/hackatime", restoredBody, reviewer, nil)
	if restored.Code != http.StatusOK {
		t.Fatalf("restore = %d: %s", restored.Code, restored.Body.String())
	}

	restoredResult := decode[contract.HackatimeAdjustment](t, restored)
	if restoredResult.Adjustment != nil {
		t.Errorf("restored adjustment = %+v, want none", restoredResult.Adjustment)
	}

	if restoredResult.HoursApprovedSeconds != journalEntriesTotal(bundle.Journals) {
		t.Errorf("restored total = %d, want %d", restoredResult.HoursApprovedSeconds, journalEntriesTotal(bundle.Journals))
	}
}

// TestHackatimeCutRequiresReason covers the reason requirement on a Hackatime cut.
func TestHackatimeCutRequiresReason(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	setHackatimeSeconds(t, application, "sub_fixture_1", 3600)

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	body, _ := json.Marshal(contract.HackatimeReviewRequest{ApprovedSeconds: 1800, Version: 1})

	refused := do(t, application, http.MethodPatch, testBase+"/submissions/sub_fixture_1/hackatime", body, reviewer, nil)
	if refused.Code != http.StatusUnprocessableEntity {
		t.Fatalf("cut without reason = %d: %s", refused.Code, refused.Body.String())
	}

	if problem := decode[contract.ErrorResponse](t, refused); problem.Code != "deflation_reason_required" || problem.Field == nil || *problem.Field != "deflationReason" {
		t.Fatalf("problem = %+v", problem)
	}

	overBody, _ := json.Marshal(contract.HackatimeReviewRequest{ApprovedSeconds: 7200, Version: 1})

	over := do(t, application, http.MethodPatch, testBase+"/submissions/sub_fixture_1/hackatime", overBody, reviewer, nil)
	if over.Code != http.StatusUnprocessableEntity {
		t.Fatalf("cut above claim = %d: %s", over.Code, over.Body.String())
	}
}

// TestHackatimeAdjustmentRequiresClaimAndVersion covers the guards on the Hackatime edit.
func TestHackatimeAdjustmentRequiresClaimAndVersion(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	setHackatimeSeconds(t, application, "sub_fixture_1", 3600)

	body, _ := json.Marshal(contract.HackatimeReviewRequest{ApprovedSeconds: 3600, Version: 1})

	if unclaimed := do(t, application, http.MethodPatch, testBase+"/submissions/sub_fixture_1/hackatime", body, reviewer, nil); unclaimed.Code != http.StatusConflict {
		t.Fatalf("unclaimed = %d: %s", unclaimed.Code, unclaimed.Body.String())
	}

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	staleBody, _ := json.Marshal(contract.HackatimeReviewRequest{ApprovedSeconds: 3600, Version: 99})

	stale := do(t, application, http.MethodPatch, testBase+"/submissions/sub_fixture_1/hackatime", staleBody, reviewer, nil)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), "stale_version") {
		t.Fatalf("stale = %d: %s", stale.Code, stale.Body.String())
	}
}

// clippedEntry returns a journal entry carrying timelapse coverage.
func clippedEntry(t *testing.T, bundle contract.ReviewBundle) contract.JournalEntry {
	t.Helper()

	for _, entry := range bundle.Journals {
		if len(entry.Timelapses) > 0 && entry.Timelapses[0].CoveredSeconds > 0 {
			return entry
		}
	}

	t.Fatal("fixture journals carry no timelapse clips")

	return contract.JournalEntry{}
}

// TestTimelapseCutSavesLedger covers one clip's approved time and the ship total it feeds.
func TestTimelapseCutSavesLedger(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, reviewer, nil))
	entry := clippedEntry(t, bundle)
	covered := entry.Timelapses[0].CoveredSeconds
	approved := covered / 2
	reason := "unverifiable"

	body, _ := json.Marshal(contract.TimelapseReviewRequest{
		ApprovedSeconds: approved,
		DeflationReason: &reason,
		ReviewerNote:    "The clip shows another screen.",
		Version:         1,
	})

	adjusted := do(t, application, http.MethodPatch, testBase+"/submissions/sub_fixture_1/journal/"+entry.ID+"/timelapse/0", body, reviewer, nil)
	if adjusted.Code != http.StatusOK {
		t.Fatalf("timelapse adjustment = %d: %s", adjusted.Code, adjusted.Body.String())
	}

	result := decode[contract.TimelapseAdjustment](t, adjusted)
	if result.Adjustment == nil || result.Adjustment.Seconds != covered-approved {
		t.Fatalf("adjustment = %+v", result.Adjustment)
	}

	if result.Journal.Timelapses[0].ApprovedSeconds == nil || *result.Journal.Timelapses[0].ApprovedSeconds != approved {
		t.Fatalf("clip approved = %+v", result.Journal.Timelapses[0].ApprovedSeconds)
	}

	expected := journalEntriesTotal(bundle.Journals) - (covered - approved)
	if result.HoursApprovedSeconds != expected {
		t.Errorf("total = %d, want %d", result.HoursApprovedSeconds, expected)
	}

	restoreBody, _ := json.Marshal(contract.TimelapseReviewRequest{ApprovedSeconds: covered, Version: 1})

	restored := do(t, application, http.MethodPatch, testBase+"/submissions/sub_fixture_1/journal/"+entry.ID+"/timelapse/0", restoreBody, reviewer, nil)
	if restored.Code != http.StatusOK {
		t.Fatalf("restore = %d: %s", restored.Code, restored.Body.String())
	}

	if restoredResult := decode[contract.TimelapseAdjustment](t, restored); restoredResult.Adjustment != nil {
		t.Errorf("restored adjustment = %+v, want none", restoredResult.Adjustment)
	}
}

// TestTimelapseCutRequiresReasonAndKnownClip covers the guards on the clip edit.
func TestTimelapseCutRequiresReasonAndKnownClip(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, reviewer, nil))
	entry := clippedEntry(t, bundle)
	covered := entry.Timelapses[0].CoveredSeconds
	path := testBase + "/submissions/sub_fixture_1/journal/" + entry.ID + "/timelapse/0"

	body, _ := json.Marshal(contract.TimelapseReviewRequest{ApprovedSeconds: covered / 2, Version: 1})

	if refused := do(t, application, http.MethodPatch, path, body, reviewer, nil); refused.Code != http.StatusUnprocessableEntity {
		t.Fatalf("cut without reason = %d: %s", refused.Code, refused.Body.String())
	}

	missing := do(t, application, http.MethodPatch, testBase+"/submissions/sub_fixture_1/journal/"+entry.ID+"/timelapse/99", body, reviewer, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown clip = %d: %s", missing.Code, missing.Body.String())
	}
}

// TestJournalTimelapsesExposed covers the clip list behind the timelapse grid.
func TestJournalTimelapsesExposed(t *testing.T) {
	adjustments := []contract.TimeAdjustment{
		{ID: "t1", Kind: "cut", Seconds: 600, Reason: "idle", Source: "clip:entry_1:clip_a", CreatedAt: "2026-01-01T00:00:00Z"},
	}
	clips := journalTimelapses("entry_1", `[
		{"provider":"lapse","externalId":"clip_a","url":"https://lapse.hackclub.com/clip_a","thumbnailUrl":"https://cdn.example.com/a.jpg","coveredSeconds":1800},
		{"provider":"lapse","externalId":"clip_b","url":"javascript:alert(1)","coveredSeconds":900},
		{"provider":"lapse","coveredSeconds":-5}
	]`, adjustments)

	if len(clips) != 3 {
		t.Fatalf("clips = %d, want 3", len(clips))
	}

	if clips[0].URL == nil || *clips[0].URL != "https://lapse.hackclub.com/clip_a" {
		t.Errorf("url = %+v", clips[0].URL)
	}

	if clips[0].ApprovedSeconds == nil || *clips[0].ApprovedSeconds != 1200 {
		t.Errorf("approved = %+v", clips[0].ApprovedSeconds)
	}

	if clips[1].URL != nil {
		t.Errorf("unsafe url should stay missing: %+v", clips[1].URL)
	}

	if clips[1].ApprovedSeconds != nil {
		t.Errorf("full clip should carry no approved override: %+v", clips[1].ApprovedSeconds)
	}

	if clips[2].CoveredSeconds != 0 {
		t.Errorf("negative coverage = %d, want 0", clips[2].CoveredSeconds)
	}

	if got := journalTimelapses("entry_1", "not json", nil); len(got) != 0 {
		t.Errorf("malformed = %d, want 0", len(got))
	}
}
