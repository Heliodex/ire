package main

import (
	"bytes"
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

	editBody, _ := json.Marshal(contract.FindingUpdateRequest{
		MakerAction: stringPtr("Add the missing build steps to the README."),
		Severity:    stringPtr("block"),
		Status:      "open",
		Summary:     stringPtr("The README is a placeholder with no build steps."),
	})

	edited := do(t, application, http.MethodPatch, testBase+"/submissions/sub_fixture_1/findings/"+finding.ID, editBody, cookie, nil)
	if edited.Code != http.StatusOK {
		t.Fatalf("edit finding = %d: %s", edited.Code, edited.Body.String())
	}

	if updated := decode[contract.FindingResponse](t, edited).Finding; updated.Summary != "The README is a placeholder with no build steps." ||
		updated.Severity != "block" || updated.MakerAction == nil ||
		*updated.MakerAction != "Add the missing build steps to the README." {
		t.Fatalf("edited finding = %+v", updated)
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

// TestFindingSurvivesResubmission covers the ledger key: findings follow the external id, not the version row.
func TestFindingSurvivesResubmission(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	first := ingestBody("starbie:design")

	created := do(t, application, http.MethodPost, "/api/ingest/half-life", first, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", first),
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("first ingest = %d: %s", created.Code, created.Body.String())
	}

	v1 := decode[contract.IngestAccepted](t, created)

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/"+v1.ID+"/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim v1 = %d: %s", claim.Code, claim.Body.String())
	}

	bundle1 := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/"+v1.ID, nil, cookie, nil))

	createBody, _ := json.Marshal(contract.FindingCreateRequest{
		MakerAction: stringPtr("Add the Gerber ZIP."),
		Severity:    "fixable",
		Station:     "triage",
		Summary:     "The Gerber ZIP is missing.",
	})

	made := do(t, application, http.MethodPost, testBase+"/submissions/"+v1.ID+"/findings", createBody, cookie, nil)
	if made.Code != http.StatusOK {
		t.Fatalf("finding v1 = %d: %s", made.Code, made.Body.String())
	}

	finding := decode[contract.FindingResponse](t, made).Finding

	withdraw := []byte(`{"external_id":"starbie:design"}`)
	if closed := do(t, application, http.MethodPost, "/api/ingest/half-life/withdraw", withdraw, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", withdraw),
	}); closed.Code != http.StatusOK {
		t.Fatalf("withdraw = %d: %s", closed.Code, closed.Body.String())
	}

	second := bytes.Replace(first, []byte("Line follower"), []byte("Line follower v2"), 1)

	resubmitted := do(t, application, http.MethodPost, "/api/ingest/half-life", second, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", second),
	})
	if resubmitted.Code != http.StatusCreated {
		t.Fatalf("resubmit = %d: %s", resubmitted.Code, resubmitted.Body.String())
	}

	v2 := decode[contract.IngestAccepted](t, resubmitted)
	if v2.Version != 2 {
		t.Fatalf("version = %d, want 2", v2.Version)
	}

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/"+v2.ID+"/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim v2 = %d: %s", claim.Code, claim.Body.String())
	}

	bundle2 := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/"+v2.ID, nil, cookie, nil))
	if len(bundle2.Findings) != 1 || bundle2.Findings[0].ID != finding.ID {
		t.Fatalf("v2 findings = %+v", bundle2.Findings)
	}

	if bundle2.Findings[0].OpenedVersion != 1 || bundle2.Findings[0].OpenedSubmissionID != bundle1.Submission.ID {
		t.Fatalf("finding origin = %+v", bundle2.Findings[0])
	}

	fixBody, _ := json.Marshal(contract.FindingUpdateRequest{Resolution: stringPtr("Added the Gerber ZIP."), Status: "fixed"})

	fixed := do(t, application, http.MethodPatch, testBase+"/submissions/"+v2.ID+"/findings/"+finding.ID, fixBody, cookie, nil)
	if fixed.Code != http.StatusOK {
		t.Fatalf("fix on v2 = %d: %s", fixed.Code, fixed.Body.String())
	}

	hours := max(
		bundle2.Submission.HoursClaimedSeconds,
		bundle2.Submission.HoursJournalSeconds+bundle2.Submission.HoursHackatimeSeconds,
	)
	approveBody, _ := json.Marshal(contract.DecisionRequest{
		Acknowledgements:     warningKeys(bundle2.Checks),
		Decision:             decisionApproved,
		HoursApprovedSeconds: &hours,
		IdempotencyKey:       "dec-resubmission-finding",
		NoteToMaker:          "Thanks for the fix.",
		Version:              bundle2.Submission.Version,
	})

	approved := do(t, application, http.MethodPost, testBase+"/submissions/"+v2.ID+"/decision", approveBody, cookie, nil)
	if approved.Code != http.StatusOK {
		t.Fatalf("approve v2 = %d: %s", approved.Code, approved.Body.String())
	}
}
