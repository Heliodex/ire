package main

import (
	"net/http"
	"testing"

	"review/contract"
)

// TestQueueRoutesResubmissionToPreviousReviewer covers the previous-reviewer hold: the reviewer who last decided a ship sees its resubmission first, while other reviewers see it behind the rest of the queue.
func TestQueueRoutesResubmissionToPreviousReviewer(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	organiser := login(t, application, "organiser@example.com")

	var reviewerID string
	if err := application.store.QueryRow(t.Context(), `select id from user where email = ?`, "reviewer@example.com").Scan(&reviewerID); err != nil {
		t.Fatal(err)
	}

	now := application.nowText()

	statements := []string{
		`update submission set priority = 0`,
		`update submission set status = 'decided', decided_at = '` + now + `' where id = 'sub_fixture_1'`,
		`update submission set previous_submission_id = 'sub_fixture_1' where id = 'sub_fixture_2'`,
	}
	for _, statement := range statements {
		if _, err := application.store.Exec(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := application.store.Exec(t.Context(), `insert into review (id, submission_id, version, stage, reviewer_id, reviewer_email, reviewer_role, decision, policy_version, is_final, created_at)
		values ('rev_routing', 'sub_fixture_1', 1, 1, ?, 'reviewer@example.com', 'reviewer', 'approved', 1, 1, ?)`, reviewerID, now); err != nil {
		t.Fatal(err)
	}

	reviewerQueue := queueItems(t, application, reviewer)
	if len(reviewerQueue) != 2 || reviewerQueue[0].ID != "sub_fixture_2" || reviewerQueue[1].ID != "sub_fixture_3" {
		t.Errorf("previous reviewer queue = %v, want the resubmission first", reviewerQueue)
	}

	if reviewerQueue[0].PreviousReviewer == nil || *reviewerQueue[0].PreviousReviewer != "reviewer@example.com" {
		t.Errorf("previous reviewer = %v", reviewerQueue[0].PreviousReviewer)
	}

	organiserQueue := queueItems(t, application, organiser)
	if len(organiserQueue) != 2 || organiserQueue[0].ID != "sub_fixture_3" || organiserQueue[1].ID != "sub_fixture_2" {
		t.Errorf("other reviewer queue = %v, want the fresh resubmission last", organiserQueue)
	}
}

// queueItems lists the stage 1 queue in the order the reviewer sees it.
func queueItems(t *testing.T, application *reviewApp, cookie *http.Cookie) []contract.QueueItem {
	t.Helper()

	recorder := do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all", nil, cookie, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("queue = %d: %s", recorder.Code, recorder.Body.String())
	}

	return decode[contract.QueueResponse](t, recorder).Items
}
