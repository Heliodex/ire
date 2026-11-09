package main

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	"ire/contract"
)

func decideFixture1(t *testing.T, application *ireApp, cookie *http.Cookie, key string) contract.DecisionOutcome {
	t.Helper()

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))
	body, _ := json.Marshal(contract.DecisionRequest{
		Decision:         decisionApproved,
		Version:          bundle.Submission.Version,
		NoteToMaker:      "Approved in tests.",
		Acknowledgements: warningKeys(bundle.Checks),
		IdempotencyKey:   key,
	})

	recorder := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", body, cookie, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("decision = %d: %s", recorder.Code, recorder.Body.String())
	}

	return decode[contract.DecisionOutcome](t, recorder)
}

func addReviewer(t *testing.T, application *ireApp, id, email, name string) *http.Cookie {
	t.Helper()

	if _, err := application.store.Exec(t.Context(), `insert into user (id, email, name, created_at) values (?, ?, ?, ?)`,
		id, email, name, application.nowText()); err != nil {
		t.Fatal(err)
	}

	if _, err := application.store.Exec(t.Context(), `insert into membership (id, program_id, user_id, role, tracks, permissions, active, created_at)
		values (?, ?, ?, ?, '[]', '[]', 1, ?)`, newID("mem"), programID, id, roleReviewer, application.nowText()); err != nil {
		t.Fatal(err)
	}

	return login(t, application, email)
}

func TestAdminCanRevertDecision(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	admin := login(t, application, "admin@example.com")

	decideFixture1(t, application, reviewer, "revert-admin-decide")

	body, _ := json.Marshal(contract.RevertDecisionRequest{Reason: "Recorded by mistake.", Version: 1})
	recorder := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision/revert", body, admin, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("revert = %d: %s", recorder.Code, recorder.Body.String())
	}

	result := decode[contract.RevertDecisionResult](t, recorder)
	if result.Status != statusReady || result.Stage != stageOne {
		t.Fatalf("result = %+v", result)
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, admin, nil))
	if bundle.Submission.Status != statusReady || bundle.Submission.DecidedAt != nil {
		t.Fatalf("submission = %+v", bundle.Submission)
	}

	var invalidated bool
	for _, review := range bundle.Reviews {
		if review.InvalidatedAt != nil {
			invalidated = true
		}
	}

	if !invalidated {
		t.Fatal("expected invalidated review")
	}
}

func TestOriginalReviewerCanRevertOwnDecision(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	other := addReviewer(t, application, "usr_reviewer2", "reviewer2@example.com", "Reviewer Two")

	decideFixture1(t, application, reviewer, "revert-own-decide")

	body, _ := json.Marshal(contract.RevertDecisionRequest{Reason: "Wrong call.", Version: 1})
	denied := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision/revert", body, other, nil)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("other revert = %d: %s", denied.Code, denied.Body.String())
	}

	recorder := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision/revert", body, reviewer, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("own revert = %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestAmendDecision(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	admin := login(t, application, "admin@example.com")

	decideFixture1(t, application, reviewer, "amend-decide")

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, admin, nil))
	hours := bundle.Submission.HoursClaimedSeconds
	body, _ := json.Marshal(contract.AmendDecisionRequest{
		Decision:             decisionChanges,
		HoursApprovedSeconds: &hours,
		Version:              bundle.Submission.Version,
		NoteToMaker:          "Please clarify the README.",
		Reason:               "Should have asked for changes.",
		IdempotencyKey:       "amend-1",
		Acknowledgements:     []string{},
	})

	recorder := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision/amend", body, admin, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("amend = %d: %s", recorder.Code, recorder.Body.String())
	}

	outcome := decode[contract.DecisionOutcome](t, recorder)
	if outcome.Status != statusDecided || outcome.Held != 0 {
		t.Fatalf("outcome = %+v", outcome)
	}

	updated := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, admin, nil))
	if updated.Submission.Status != statusDecided {
		t.Fatalf("submission = %+v", updated.Submission)
	}

	var finals, invalidated int
	for _, review := range updated.Reviews {
		if review.InvalidatedAt != nil {
			invalidated++
			continue
		}

		finals++
		if review.Decision != decisionChanges {
			t.Errorf("final decision = %q", review.Decision)
		}
	}

	if invalidated == 0 || finals != 1 {
		t.Fatalf("reviews invalidated=%d finals=%d", invalidated, finals)
	}
}

func TestOriginalReviewerCanAmendOwnDecision(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	other := addReviewer(t, application, "usr_reviewer3", "reviewer3@example.com", "Reviewer Three")

	decideFixture1(t, application, reviewer, "amend-own-decide")

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, reviewer, nil))
	hours := bundle.Submission.HoursClaimedSeconds
	body, _ := json.Marshal(contract.AmendDecisionRequest{
		Decision:             decisionChanges,
		HoursApprovedSeconds: &hours,
		Version:              bundle.Submission.Version,
		NoteToMaker:          "Needs a small fix.",
		Reason:               "I missed an issue.",
		IdempotencyKey:       "amend-own-1",
		Acknowledgements:     []string{},
	})

	denied := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision/amend", body, other, nil)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("other amend = %d: %s", denied.Code, denied.Body.String())
	}

	recorder := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision/amend", body, reviewer, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("own amend = %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestRevertRejectedStaleVersion(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	admin := login(t, application, "admin@example.com")

	decideFixture1(t, application, reviewer, "revert-stale-decide")

	body, _ := json.Marshal(contract.RevertDecisionRequest{Reason: "Nope.", Version: 99})
	recorder := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision/revert", body, admin, nil)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("stale revert = %d: %s", recorder.Code, recorder.Body.String())
	}
}
