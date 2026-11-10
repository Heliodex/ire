package main

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	"ire/contract"
)

// TestApprovalBlockedByOpenFinding covers the finding lifecycle and the approval gate.
func TestApprovalBlockedByOpenFinding(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))

	createBody, _ := json.Marshal(contract.FindingCreateRequest{
		MakerAction: stringPtr("Write a README that explains the build."),
		Question:    stringPtr("Is the README real?"),
		Severity:    "fixable",
		Station:     "narrative",
		Summary:     "The README is a placeholder.",
	})

	created := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/findings", createBody, cookie, nil)
	if created.Code != http.StatusOK {
		t.Fatalf("create finding = %d: %s", created.Code, created.Body.String())
	}

	finding := decode[contract.FindingResponse](t, created).Finding
	if finding.Status != "open" || finding.ExternalID != bundle.Submission.ExternalID {
		t.Fatalf("finding = %+v", finding)
	}

	list := decode[contract.FindingsResponse](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1/findings", nil, cookie, nil))
	if len(list.Findings) != 1 || list.Findings[0].ID != finding.ID {
		t.Fatalf("findings = %+v", list.Findings)
	}

	hours := max(
		bundle.Submission.HoursClaimedSeconds,
		bundle.Submission.HoursJournalSeconds+bundle.Submission.HoursHackatimeSeconds,
	)
	approveBody, _ := json.Marshal(contract.DecisionRequest{
		Acknowledgements:     warningKeys(bundle.Checks),
		Decision:             decisionApproved,
		HoursApprovedSeconds: &hours,
		IdempotencyKey:       "dec-open-finding-1",
		InternalNote:         "Checked.",
		NoteToMaker:          "Nice.",
		Version:              bundle.Submission.Version,
	})

	blocked := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", approveBody, cookie, nil)
	if blocked.Code != http.StatusUnprocessableEntity {
		t.Fatalf("approval with open finding = %d: %s", blocked.Code, blocked.Body.String())
	}

	if problem := decode[contract.ErrorResponse](t, blocked); problem.Code != "open_findings" {
		t.Fatalf("problem = %+v", problem)
	}

	fixBody, _ := json.Marshal(contract.FindingUpdateRequest{Resolution: stringPtr("README rewritten with the build steps."), Status: "fixed"})

	fixed := do(t, application, http.MethodPatch, testBase+"/submissions/sub_fixture_1/findings/"+finding.ID, fixBody, cookie, nil)
	if fixed.Code != http.StatusOK {
		t.Fatalf("fix finding = %d: %s", fixed.Code, fixed.Body.String())
	}

	if updated := decode[contract.FindingResponse](t, fixed).Finding; updated.Status != "fixed" || updated.Resolution == nil {
		t.Fatalf("fixed finding = %+v", updated)
	}

	approved := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", approveBody, cookie, nil)
	if approved.Code != http.StatusOK {
		t.Fatalf("approval after fix = %d: %s", approved.Code, approved.Body.String())
	}
}

// TestReturnRecordsItsFindings covers the finding_return rows that make a return auditable.
func TestReturnRecordsItsFindings(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))

	createBody, _ := json.Marshal(contract.FindingCreateRequest{
		MakerAction: stringPtr("Add the Gerber ZIP."),
		Question:    stringPtr("Are the fabrication files present?"),
		Severity:    "fixable",
		Station:     "triage",
		Summary:     "The Gerber ZIP is missing.",
	})

	created := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/findings", createBody, cookie, nil)
	if created.Code != http.StatusOK {
		t.Fatalf("create finding = %d: %s", created.Code, created.Body.String())
	}

	finding := decode[contract.FindingResponse](t, created).Finding

	returnBody, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionChanges,
		FindingIds:     []string{finding.ID},
		IdempotencyKey: "dec-return-finding-1",
		InternalNote:   "Returning with the complete list.",
		NoteToMaker:    "Please add the Gerber ZIP and resubmit.",
		Version:        bundle.Submission.Version,
	})

	returned := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", returnBody, cookie, nil)
	if returned.Code != http.StatusOK {
		t.Fatalf("return = %d: %s", returned.Code, returned.Body.String())
	}

	outcome := decode[contract.DecisionOutcome](t, returned)

	var links int
	if err := application.store.QueryRow(t.Context(), `select count(*) from finding_return where review_id = ? and finding_id = ?`, outcome.ID, finding.ID).Scan(&links); err != nil {
		t.Fatal(err)
	}

	if links != 1 {
		t.Fatalf("finding_return links = %d, want 1", links)
	}

	badBody, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionChanges,
		FindingIds:     []string{"fnd_missing"},
		IdempotencyKey: "dec-return-missing-finding",
		NoteToMaker:    "Please fix this.",
		Version:        1,
	})

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("second claim = %d: %s", claim.Code, claim.Body.String())
	}

	bad := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", badBody, cookie, nil)
	if bad.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad finding return = %d: %s", bad.Code, bad.Body.String())
	}

	if problem := decode[contract.ErrorResponse](t, bad); problem.Code != "invalid_finding" {
		t.Fatalf("bad finding problem = %+v", problem)
	}
}
