package main

import (
	"encoding/json/v2"
	"ire/contract"
	"net/http"
	"testing"
)

// TestReviewedListsDecidedShips covers the reviewed tab and its decision filter.
func TestReviewedListsDecidedShips(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	body, _ := json.Marshal(contract.DecisionRequest{
		Acknowledgements: []string{},
		Decision: decisionApproved,
		IdempotencyKey: "dec-reviewed-1",
		NoteToMaker: "Clean board, clear BOM. Approved.",
		Version: 1,
	})

	if decided := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", body, cookie, nil); decided.Code != http.StatusOK {
		t.Fatalf("decision = %d: %s", decided.Code, decided.Body.String())
	}

	recorder := do(t, application, http.MethodGet, testBase+"/reviewed?decision=all", nil, cookie, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reviewed = %d: %s", recorder.Code, recorder.Body.String())
	}

	response := decode[contract.ReviewedResponse](t, recorder)
	if len(response.Items) != 1 {
		t.Fatalf("reviewed items = %+v", response.Items)
	}

	if item := response.Items[0]; item.ID != "sub_fixture_1" || item.Decision != decisionApproved || item.HoursApprovedSeconds != 23040 {
		t.Errorf("reviewed item = %+v", item)
	}

	empty := do(t, application, http.MethodGet, testBase+"/reviewed?decision=rejected", nil, cookie, nil)
	if empty.Code != http.StatusOK {
		t.Fatalf("rejected filter = %d: %s", empty.Code, empty.Body.String())
	}

	if filtered := decode[contract.ReviewedResponse](t, empty); len(filtered.Items) != 0 {
		t.Errorf("rejected filter items = %+v", filtered.Items)
	}

	if invalid := do(t, application, http.MethodGet, testBase+"/reviewed?decision=unknown", nil, cookie, nil); invalid.Code != http.StatusBadRequest {
		t.Errorf("invalid filter = %d, want 400", invalid.Code)
	}

	if anonymous := do(t, application, http.MethodGet, testBase+"/reviewed", nil, nil, nil); anonymous.Code != http.StatusUnauthorized {
		t.Errorf("anonymous reviewed = %d, want 401", anonymous.Code)
	}
}
