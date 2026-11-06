package main

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	"ire/contract"
)

const importReviewedPath = testBase + "/import-reviewed"

func machineHeaders() map[string]string {
	return map[string]string{"Authorization": "Bearer test-ingest"}
}

// importShipRequest builds one batch entry around a normal ingest body.
func importShipRequest(t *testing.T, body []byte, externalID string, reviews []contract.ImportReview) contract.ImportShipsRequest {
	t.Helper()

	var ship contract.IngestRequest
	if err := json.Unmarshal(body, &ship); err != nil {
		t.Fatal(err)
	}

	ship.ExternalID = externalID

	return contract.ImportShipsRequest{Ships: []contract.ImportShip{{Ship: ship, Reviews: reviews}}}
}

// TestImportReviewedCreatesDecidedShip covers a ship Ire never saw: the decision, artifacts and history all land.
func TestImportReviewedCreatesDecidedShip(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	note := "Lovely board."
	request := importShipRequest(t, ingestBody("unused"), "import:reviewed", []contract.ImportReview{{
		DecidedAt:            "2026-10-20T10:00:00Z",
		Decision:             "approved",
		HoursApprovedSeconds: 6 * 3600,
		NoteToMaker:          &note,
		ReviewerEmail:        "ari@example.com",
		ReviewerName:         stringPtr("Ari Reviewer"),
		Stage:                1,
	}})

	body, _ := json.Marshal(request)

	recorder := do(t, application, http.MethodPost, importReviewedPath, body, nil, machineHeaders())
	if recorder.Code != http.StatusOK {
		t.Fatalf("import = %d: %s", recorder.Code, recorder.Body.String())
	}

	result := decode[contract.ImportResult](t, recorder)
	if result.Imported != 1 || result.Skipped != 0 || len(result.Failed) != 0 {
		t.Fatalf("result = %+v", result)
	}

	var id string

	if err := application.store.QueryRow(t.Context(), `select id from submission where external_id = ?`, "import:reviewed").Scan(&id); err != nil {
		t.Fatal(err)
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/"+id, nil, cookie, nil))
	if bundle.Submission.Status != statusDecided {
		t.Fatalf("status = %q", bundle.Submission.Status)
	}

	if bundle.Submission.HoursApprovedSeconds == nil || *bundle.Submission.HoursApprovedSeconds != 6*3600 {
		t.Fatalf("approved = %v", bundle.Submission.HoursApprovedSeconds)
	}

	if bundle.Submission.DecidedAt == nil || *bundle.Submission.DecidedAt != "2026-10-20T10:00:00Z" {
		t.Fatalf("decided_at = %v", bundle.Submission.DecidedAt)
	}

	if len(bundle.Journals) != 2 || len(bundle.Bom) != 2 {
		t.Fatalf("artifacts = %d journals, %d bom", len(bundle.Journals), len(bundle.Bom))
	}

	if len(bundle.Reviews) != 1 || bundle.Reviews[0].Decision != "approved" || bundle.Reviews[0].ReviewerEmail != "ari@example.com" {
		t.Fatalf("reviews = %+v", bundle.Reviews)
	}

	var outbox, checks int

	if err := application.store.QueryRow(t.Context(), `select count(*) from webhook_outbox`).Scan(&outbox); err != nil {
		t.Fatal(err)
	}

	if err := application.store.QueryRow(t.Context(), `select count(*) from check_result where submission_id = ?`, id).Scan(&checks); err != nil {
		t.Fatal(err)
	}

	if outbox != 0 || checks != 0 {
		t.Fatalf("import wrote %d outbox rows and %d checks", outbox, checks)
	}

	reviewed := decode[contract.ReviewedResponse](t, do(t, application, http.MethodGet, testBase+"/reviewed?decision=all", nil, cookie, nil))

	found := false

	for _, item := range reviewed.Items {
		if item.ID == id {
			found = item.Decision == "approved"
		}
	}

	if !found {
		t.Fatalf("imported ship missing from reviewed: %+v", reviewed.Items)
	}

	replay := do(t, application, http.MethodPost, importReviewedPath, body, nil, machineHeaders())
	if replay.Code != http.StatusOK {
		t.Fatalf("replay = %d: %s", replay.Code, replay.Body.String())
	}

	replayed := decode[contract.ImportResult](t, replay)
	if replayed.Skipped != 1 || replayed.Imported != 0 {
		t.Fatalf("replay result = %+v", replayed)
	}
}

// TestImportReviewedDecidesOpenShip covers the shadow copy case: the existing open ship becomes decided in place.
func TestImportReviewedDecidesOpenShip(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	queue := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&limit=50", nil, cookie, nil))

	var open contract.QueueItem

	for _, item := range queue.Items {
		if item.Status == statusReady {
			open = item

			break
		}
	}

	if open.ID == "" {
		t.Fatal("no open ship to decide")
	}

	var versionsBefore int

	if err := application.store.QueryRow(t.Context(), `select count(*) from submission_version where submission_id = ?`, open.ID).Scan(&versionsBefore); err != nil {
		t.Fatal(err)
	}

	ship := contract.IngestRequest{
		ExternalID:  open.ExternalID,
		Title:       open.Title,
		Description: "historical import",
		Maker:       contract.IngestMaker{Email: "maker@example.com", Name: "Maker"},
	}

	request := contract.ImportShipsRequest{Ships: []contract.ImportShip{{
		Ship: ship,
		Reviews: []contract.ImportReview{
			{DecidedAt: "2026-10-20T09:00:00Z", Decision: "changes", HoursApprovedSeconds: 0, NoteToMaker: stringPtr("Please fix the BOM."), ReviewerEmail: "ari@example.com", Stage: 1},
			{DecidedAt: "2026-10-22T09:00:00Z", Decision: "approved", HoursApprovedSeconds: 5 * 3600, ReviewerEmail: "ari@example.com", ReviewerName: stringPtr("Ari"), Stage: 1},
		},
	}}}

	body, _ := json.Marshal(request)

	recorder := do(t, application, http.MethodPost, importReviewedPath, body, nil, machineHeaders())
	if recorder.Code != http.StatusOK {
		t.Fatalf("import = %d: %s", recorder.Code, recorder.Body.String())
	}

	result := decode[contract.ImportResult](t, recorder)
	if result.Imported != 1 || len(result.Failed) != 0 {
		t.Fatalf("result = %+v", result)
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/"+open.ID, nil, cookie, nil))
	if bundle.Submission.Status != statusDecided {
		t.Fatalf("status = %q", bundle.Submission.Status)
	}

	if len(bundle.Reviews) != 2 {
		t.Fatalf("reviews = %+v", bundle.Reviews)
	}

	if bundle.Submission.HoursApprovedSeconds == nil || *bundle.Submission.HoursApprovedSeconds != 5*3600 {
		t.Fatalf("approved = %v", bundle.Submission.HoursApprovedSeconds)
	}

	var versionsAfter int

	if err := application.store.QueryRow(t.Context(), `select count(*) from submission_version where submission_id = ?`, open.ID).Scan(&versionsAfter); err != nil {
		t.Fatal(err)
	}

	if versionsAfter != versionsBefore {
		t.Fatalf("versions changed from %d to %d", versionsBefore, versionsAfter)
	}

	after := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&limit=50", nil, cookie, nil))

	for _, item := range after.Items {
		if item.ID == open.ID {
			t.Fatalf("decided ship still in the queue")
		}
	}
}

func TestImportReviewedRequiresMachineToken(t *testing.T) {
	application := newTestApp(t)

	recorder := do(t, application, http.MethodPost, importReviewedPath, []byte(`{"ships":[]}`), nil, nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d: %s", recorder.Code, recorder.Body.String())
	}
}
