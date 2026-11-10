package main

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	"ire/contract"
)

// TestApprovalRequiresJustification covers the two fields that make an approval defensible and checks that they reach the outbound record.
func TestApprovalRequiresJustification(t *testing.T) {
	application := newTestApp(t)
	setJustificationPolicy(t, application, true, true)

	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))
	hours := 5 * secondsPerHour
	note := "Thanks for the detailed journal."
	request := contract.DecisionRequest{
		Acknowledgements:     warningKeys(bundle.Checks),
		Decision:             decisionApproved,
		HoursApprovedSeconds: &hours,
		IdempotencyKey:       "dec-justification-1",
		InternalNote:         "Checked the board and firmware.",
		NoteToMaker:          note,
		Version:              bundle.Submission.Version,
	}

	enc, _ := json.Marshal(request)

	missingFeatures := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", enc, cookie, nil)

	if missingFeatures.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing features = %d: %s", missingFeatures.Code, missingFeatures.Body.String())
	}

	if problem := decode[contract.ErrorResponse](t, missingFeatures); problem.Code != "technical_features_required" || problem.Field == nil || *problem.Field != "technicalFeatures" {
		t.Fatalf("missing features problem = %+v", problem)
	}

	features := "Firmware drives the OLED from the MPU6050 over I2C; the custom board routes both switches and the DHT11."
	request.TechnicalFeatures = &features

	enc, _ = json.Marshal(request)

	missingDeflation := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", enc, cookie, nil)

	if missingDeflation.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing deflation = %d: %s", missingDeflation.Code, missingDeflation.Body.String())
	}

	if problem := decode[contract.ErrorResponse](t, missingDeflation); problem.Code != "deflation_reason_required" || problem.Field == nil || *problem.Field != "deflationReason" {
		t.Fatalf("missing deflation problem = %+v", problem)
	}

	copiedNote := note
	request.DeflationReason = &copiedNote

	enc, _ = json.Marshal(request)
	copied := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", enc, cookie, nil)

	if copied.Code != http.StatusUnprocessableEntity {
		t.Fatalf("copied note = %d: %s", copied.Code, copied.Body.String())
	}

	if problem := decode[contract.ErrorResponse](t, copied); problem.Code != "deflation_reason_is_note" {
		t.Fatalf("copied note problem = %+v", problem)
	}

	reason := "An intermediate maker would take about 6h for this board and firmware; 5h is reasonable after cutting the tutorial build."
	request.DeflationReason = &reason

	enc, _ = json.Marshal(request)

	approved := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", enc, cookie, nil)

	if approved.Code != http.StatusOK {
		t.Fatalf("approved = %d: %s", approved.Code, approved.Body.String())
	}

	deliveries := decode[contract.FakeDeliveriesResponse](t, do(t, application, http.MethodGet, "/api/v1/dev/outbox/received", nil, cookie, nil))
	if len(deliveries.Deliveries) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(deliveries.Deliveries))
	}

	var event struct {
		Review struct {
			Justification struct {
				TechnicalFeatures string `json:"technical_features"`
				DeflationReason   string `json:"deflation_reason"`
			} `json:"justification"`
		} `json:"review"`
	}

	if err := json.Unmarshal([]byte(deliveries.Deliveries[0].Body), &event); err != nil {
		t.Fatal(err)
	}

	if event.Review.Justification.TechnicalFeatures != features {
		t.Errorf("technical features = %q, want %q", event.Review.Justification.TechnicalFeatures, features)
	}

	if event.Review.Justification.DeflationReason != reason {
		t.Errorf("deflation reason = %q, want %q", event.Review.Justification.DeflationReason, reason)
	}
}

// TestApprovalWithoutCutDoesNotNeedDeflationReason checks that the deflation reason is only required when the approved time is below the captured time.
func TestApprovalWithoutCutDoesNotNeedDeflationReason(t *testing.T) {
	application := newTestApp(t)
	setJustificationPolicy(t, application, true, true)

	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))
	hours := max(
		bundle.Submission.HoursClaimedSeconds,
		bundle.Submission.HoursJournalSeconds+bundle.Submission.HoursHackatimeSeconds,
	)
	features := "Firmware, board and enclosure as described in the README."
	request := contract.DecisionRequest{
		Acknowledgements:     warningKeys(bundle.Checks),
		Decision:             decisionApproved,
		HoursApprovedSeconds: &hours,
		IdempotencyKey:       "dec-no-cut-1",
		InternalNote:         "Checked.",
		NoteToMaker:          "Nice work.",
		TechnicalFeatures:    &features,
		Version:              bundle.Submission.Version,
	}

	enc, _ := json.Marshal(request)
	approved := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", enc, cookie, nil)

	if approved.Code != http.StatusOK {
		t.Fatalf("approved = %d: %s", approved.Code, approved.Body.String())
	}
}
