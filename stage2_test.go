package main

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	"review/contract"
)

// holdSecondPass takes fixture 2 through a stage 1 approval with every warning acknowledged.
func holdSecondPass(t *testing.T, application *reviewApp, reviewer *http.Cookie, key string) contract.ReviewBundle {
	t.Helper()

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("first-pass claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_2", nil, reviewer, nil))

	approve, _ := json.Marshal(contract.DecisionRequest{
		Decision:         decisionApproved,
		Version:          1,
		NoteToMaker:      "First pass approves the hours.",
		Acknowledgements: warningKeys(bundle.Checks),
		IdempotencyKey:   key,
	})

	held := decode[contract.DecisionOutcome](t, do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", approve, reviewer, nil))
	if held.Held != 1 {
		t.Fatalf("first pass was not held: %+v", held)
	}

	bundle = decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_2", nil, reviewer, nil))

	for _, review := range bundle.Reviews {
		if review.Held == 1 && len(review.Acknowledgements) == 0 {
			t.Error("the held review did not keep its acknowledgements")
		}
	}

	return bundle
}

// TestStageTwoOverrideRequiresReason covers a changed second-pass decision or hours.
func TestStageTwoOverrideRequiresReason(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	organiser := login(t, application, "organiser@example.com")

	holdSecondPass(t, application, reviewer, "override-hold")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, organiser, nil); claim.Code != http.StatusOK {
		t.Fatalf("second-pass claim = %d: %s", claim.Code, claim.Body.String())
	}

	hours := 3000

	missing, _ := json.Marshal(contract.DecisionRequest{
		Decision:             decisionApproved,
		HoursApprovedSeconds: &hours,
		Version:              1,
		NoteToMaker:          "Reduce the approved hours.",
		IdempotencyKey:       "override-missing",
	})

	rejected := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", missing, organiser, nil)
	if rejected.Code != http.StatusUnprocessableEntity {
		t.Fatalf("override without reason = %d: %s", rejected.Code, rejected.Body.String())
	}

	var body contract.ErrorResponse
	if err := json.Unmarshal(rejected.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if body.Code != "override_reason_required" {
		t.Fatalf("error code = %q", body.Code)
	}

	reason := "The first pass was too generous on the firmware hours."
	confirm, _ := json.Marshal(contract.DecisionRequest{
		Decision:             decisionApproved,
		HoursApprovedSeconds: &hours,
		Version:              1,
		NoteToMaker:          "Reduce the approved hours.",
		OverrideReason:       &reason,
		IdempotencyKey:       "override-good",
	})

	confirmed := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", confirm, organiser, nil)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("override = %d: %s", confirmed.Code, confirmed.Body.String())
	}

	outcome := decode[contract.DecisionOutcome](t, confirmed)
	if outcome.Held != 0 || outcome.DeliveryStatus == nil || *outcome.DeliveryStatus != "delivered" {
		t.Fatalf("outcome = %+v", outcome)
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_2", nil, organiser, nil))

	var final contract.Review

	for _, review := range bundle.Reviews {
		if review.Stage == 2 {
			final = review
		}
	}

	if final.OverrideReason == nil || *final.OverrideReason != reason {
		t.Fatalf("stored override = %+v", final)
	}

	received := decode[contract.FakeDeliveriesResponse](t, do(t, application, http.MethodGet, "/api/v1/dev/outbox/received", nil, organiser, nil))
	if len(received.Deliveries) != 1 {
		t.Fatalf("received %d deliveries", len(received.Deliveries))
	}

	var event struct {
		Review struct {
			ApprovedSeconds int    `json:"approved_seconds"`
			OverrideReason  string `json:"override_reason"`
		} `json:"review"`
	}
	if err := json.Unmarshal([]byte(received.Deliveries[0].Body), &event); err != nil {
		t.Fatal(err)
	}

	if event.Review.ApprovedSeconds != hours || event.Review.OverrideReason != reason {
		t.Fatalf("event review = %+v", event.Review)
	}
}

// TestStageTwoRejectsStaleVersion covers the version guard on the second pass.
func TestStageTwoRejectsStaleVersion(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	organiser := login(t, application, "organiser@example.com")

	holdSecondPass(t, application, reviewer, "stale-hold")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, organiser, nil); claim.Code != http.StatusOK {
		t.Fatalf("second-pass claim = %d: %s", claim.Code, claim.Body.String())
	}

	confirm, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionApproved,
		Version:        2,
		NoteToMaker:    "Confirm a stale version.",
		IdempotencyKey: "stale-confirm",
	})

	rejected := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", confirm, organiser, nil)
	if rejected.Code != http.StatusConflict {
		t.Fatalf("stale second pass = %d: %s", rejected.Code, rejected.Body.String())
	}

	var body contract.ErrorResponse
	if err := json.Unmarshal(rejected.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if body.Code != "stale_version" {
		t.Fatalf("error code = %q", body.Code)
	}
}

// TestStageTwoSendBack covers invalidating a hold and returning the ship to stage 1 without an outbound event.
func TestStageTwoSendBack(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	organiser := login(t, application, "organiser@example.com")

	holdSecondPass(t, application, reviewer, "send-back-hold")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, organiser, nil); claim.Code != http.StatusOK {
		t.Fatalf("second-pass claim = %d: %s", claim.Code, claim.Body.String())
	}

	reviewerAttempt, _ := json.Marshal(contract.SendBackRequest{Reason: "Ask a reviewer to look again.", Version: 1})

	if denied := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/send-back", reviewerAttempt, reviewer, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("reviewer send back = %d: %s", denied.Code, denied.Body.String())
	}

	empty, _ := json.Marshal(contract.SendBackRequest{Reason: "   ", Version: 1})

	if missing := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/send-back", empty, organiser, nil); missing.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty reason = %d: %s", missing.Code, missing.Body.String())
	}

	request, _ := json.Marshal(contract.SendBackRequest{Reason: "The warning acknowledgement needs a second opinion.", Version: 1})

	sent := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/send-back", request, organiser, nil)
	if sent.Code != http.StatusOK {
		t.Fatalf("send back = %d: %s", sent.Code, sent.Body.String())
	}

	result := decode[contract.SendBackResult](t, sent)
	if result.Status != statusReady || result.Stage != stageOne {
		t.Fatalf("send back result = %+v", result)
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_2", nil, reviewer, nil))
	if bundle.Submission.Status != statusReady || bundle.Submission.Stage != stageOne {
		t.Fatalf("submission = %+v", bundle.Submission)
	}

	if bundle.Submission.HoursApprovedSeconds != nil {
		t.Errorf("approved hours survived send back: %d", *bundle.Submission.HoursApprovedSeconds)
	}

	var invalidated bool

	for _, review := range bundle.Reviews {
		if review.Stage == stageOne && review.Held == 1 {
			invalidated = review.InvalidatedAt != nil
		}
	}

	if !invalidated {
		t.Error("the held review was not invalidated")
	}

	seen := false

	for _, event := range bundle.Events {
		if event.Kind == "sent_back" {
			seen = true
		}
	}

	if !seen {
		t.Error("no sent_back event")
	}

	outbox := decode[contract.OutboxResponse](t, do(t, application, http.MethodGet, testBase+"/outbox", nil, organiser, nil))
	if len(outbox.Deliveries) != 0 {
		t.Fatalf("send back wrote %d outbound deliveries", len(outbox.Deliveries))
	}

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("stage 1 reclaim after send back = %d: %s", claim.Code, claim.Body.String())
	}

	// Re-hold after the send-back and confirm; the invalidated review must not appear in the stage history.
	bundle = decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_2", nil, reviewer, nil))

	approve, _ := json.Marshal(contract.DecisionRequest{
		Decision:         decisionApproved,
		Version:          1,
		NoteToMaker:      "Second look approves the hours.",
		Acknowledgements: warningKeys(bundle.Checks),
		IdempotencyKey:   "send-back-rehold",
	})

	if held := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", approve, reviewer, nil); held.Code != http.StatusOK {
		t.Fatalf("re-hold = %d: %s", held.Code, held.Body.String())
	}

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, organiser, nil); claim.Code != http.StatusOK {
		t.Fatalf("second claim after re-hold = %d: %s", claim.Code, claim.Body.String())
	}

	confirm, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionApproved,
		Version:        1,
		NoteToMaker:    "Confirmed after the send-back.",
		IdempotencyKey: "send-back-confirm",
	})

	confirmed := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", confirm, organiser, nil)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirm after send back = %d: %s", confirmed.Code, confirmed.Body.String())
	}

	received := decode[contract.FakeDeliveriesResponse](t, do(t, application, http.MethodGet, "/api/v1/dev/outbox/received", nil, organiser, nil))
	if len(received.Deliveries) != 1 {
		t.Fatalf("received %d deliveries", len(received.Deliveries))
	}

	var event struct {
		StageHistory []struct {
			Stage int `json:"stage"`
		} `json:"stage_history"`
	}
	if err := json.Unmarshal([]byte(received.Deliveries[0].Body), &event); err != nil {
		t.Fatal(err)
	}

	if len(event.StageHistory) != 2 || event.StageHistory[0].Stage != 1 || event.StageHistory[1].Stage != 2 {
		t.Fatalf("stage history after send back = %+v", event.StageHistory)
	}
}
