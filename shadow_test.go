package main

import (
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	"review/contract"
)

// TestShadowModeIsReadOnly covers the server guards, the me flag, ingest and outbox suppression.
func TestShadowModeIsReadOnly(t *testing.T) {
	application := newTestApp(t)
	application.config.Shadow = true

	cookie := login(t, application, "admin@example.com")

	me := decode[contract.MeResponse](t, do(t, application, http.MethodGet, "/api/v1/me", nil, cookie, nil))
	if !me.ReadOnly {
		t.Fatal("me.readOnly = false")
	}

	paths := []struct {
		method, path string
		body         []byte
	}{
		{http.MethodPost, testBase + "/submissions/sub_fixture_1/claim", nil},
		{http.MethodPost, testBase + "/submissions/sub_fixture_1/heartbeat", nil},
		{http.MethodDelete, testBase + "/submissions/sub_fixture_1/claim", nil},
		{http.MethodPut, testBase + "/submissions/sub_fixture_1/draft", []byte(`{"note":"read only","acknowledgements":[]}`)},
		{http.MethodPost, testBase + "/submissions/sub_fixture_1/send-back", []byte(`{"reason":"read only","version":1}`)},
	}

	for _, item := range paths {
		recorder := do(t, application, item.method, item.path, item.body, cookie, nil)
		if recorder.Code != http.StatusConflict {
			t.Errorf("%s %s = %d: %s", item.method, item.path, recorder.Code, recorder.Body.String())
			continue
		}

		var problem contract.ErrorResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
			t.Fatalf("decode %s: %v", item.path, err)
		}

		if problem.Code != shadowErrorCode {
			t.Errorf("%s code = %q", item.path, problem.Code)
		}
	}

	decision, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionApproved,
		Version:        1,
		NoteToMaker:    "This must not be recorded.",
		IdempotencyKey: "shadow-decision",
	})

	rejected := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", decision, cookie, nil)
	if rejected.Code != http.StatusConflict {
		t.Fatalf("decision = %d: %s", rejected.Code, rejected.Body.String())
	}

	body := ingestBody("shadow:design")
	accepted := do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	})

	if accepted.Code != http.StatusCreated {
		t.Fatalf("shadow ingest = %d: %s", accepted.Code, accepted.Body.String())
	}

	var deliveries int
	if err := application.store.QueryRow(t.Context(), `select count(*) from webhook_outbox`).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}

	if deliveries != 0 {
		t.Errorf("outbox rows = %d in shadow mode", deliveries)
	}
}

// TestOutboundDisabledSuppressesDelivery covers the dead outbound_enabled column.
func TestOutboundDisabledSuppressesDelivery(t *testing.T) {
	application := newTestApp(t)

	if _, err := application.store.Exec(t.Context(), `update program set outbound_enabled = 0 where id = ?`, programID); err != nil {
		t.Fatal(err)
	}

	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	body, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionApproved,
		Version:        1,
		NoteToMaker:    "Do not deliver this.",
		IdempotencyKey: "disabled-outbound",
	})

	outcome := decode[contract.DecisionOutcome](t, do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", body, cookie, nil))
	if outcome.OutboxID != nil || (outcome.DeliveryStatus != nil && *outcome.DeliveryStatus != "") {
		t.Fatalf("outcome = %+v", outcome)
	}

	var deliveries int
	if err := application.store.QueryRow(t.Context(), `select count(*) from webhook_outbox`).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}

	if deliveries != 0 {
		t.Errorf("outbox rows = %d with outbound disabled", deliveries)
	}
}

// TestIngestPayloadLimits covers a payload larger than the old 1 MiB cap and one over the new 16 MiB cap.
func TestIngestPayloadLimits(t *testing.T) {
	application := newTestApp(t)

	var request contract.IngestRequest
	if err := json.Unmarshal(ingestBody("large:design"), &request); err != nil {
		t.Fatal(err)
	}

	request.Journals[0].Text = strings.Repeat("A detailed account of the board and the firmware. ", 48000)

	body, _ := json.Marshal(request)
	if len(body) <= 1<<20 {
		t.Fatalf("test payload is only %d bytes", len(body))
	}

	accepted := do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	})
	if accepted.Code != http.StatusCreated {
		t.Fatalf("large ingest = %d: %s", accepted.Code, accepted.Body.String())
	}

	request.Journals[0].Text = strings.Repeat("x", maxIngestBody+1)

	body, _ = json.Marshal(request)
	rejected := do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	})

	if rejected.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized ingest = %d: %s", rejected.Code, rejected.Body.String())
	}
}
