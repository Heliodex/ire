package main

import (
	"encoding/json/v2"
	"ire/contract"
	"net/http"
	"strings"
	"testing"
)

// TestAuditorIsReadOnly covers the auditor role outside shadow mode.
func TestAuditorIsReadOnly(t *testing.T) {
	application := newTestApp(t)

	if _, err := application.store.Exec(t.Context(), `insert into user (id, email, name, created_at) values ('usr_auditor', 'auditor@example.com', 'Aud', ?)`, application.nowText()); err != nil {
		t.Fatal(err)
	}

	if _, err := application.store.Exec(t.Context(), `insert into membership (id, program_id, user_id, role, tracks, permissions, active, created_at)
		values ('mem_auditor', ?, 'usr_auditor', ?, '["hardware"]', '[]', 1, ?)`, programID, roleAuditor, application.nowText()); err != nil {
		t.Fatal(err)
	}

	cookie := login(t, application, "auditor@example.com")

	if queue := do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all", nil, cookie, nil); queue.Code != http.StatusOK {
		t.Fatalf("auditor queue = %d: %s", queue.Code, queue.Body.String())
	}

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusForbidden {
		t.Fatalf("auditor claim = %d: %s", claim.Code, claim.Body.String())
	}

	body, _ := json.Marshal(contract.DecisionRequest{
		Decision: decisionApproved,
		IdempotencyKey: "auditor-decision",
		NoteToMaker: "Auditors cannot decide.",
		Version: 1,
	})

	if decision := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", body, cookie, nil); decision.Code != http.StatusForbidden {
		t.Fatalf("auditor decision = %d: %s", decision.Code, decision.Body.String())
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

	if accepted := do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{"X-Ari-Signature": hmacHex("test-ingest", body)}); accepted.Code != http.StatusCreated {
		t.Fatalf("large ingest = %d: %s", accepted.Code, accepted.Body.String())
	}

	request.Journals[0].Text = strings.Repeat("x", maxIngestBody+1)
	body, _ = json.Marshal(request)

	if rejected := do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{"X-Ari-Signature": hmacHex("test-ingest", body)}); rejected.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized ingest = %d: %s", rejected.Code, rejected.Body.String())
	}
}

// TestNestedMetaRoundTrip covers the richer Review-only fields Half Life sends alongside the Ari payload.
func TestNestedMetaRoundTrip(t *testing.T) {
	application := newTestApp(t)

	var request contract.IngestRequest

	if err := json.Unmarshal(ingestBody("meta:design"), &request); err != nil {
		t.Fatal(err)
	}

	request.Meta.GrantCapCents = new(7500)
	request.Meta.PriorReviews = []contract.IngestPriorReview{
		{
			Result: "changes",
			Feedback: stringPtr("Add the board render."),
			Reviewer: stringPtr("Ari Reviewer"),
			At: stringPtr("2026-10-30T10:00:00Z"),
		},
	}
	body, _ := json.Marshal(request)
	created := decode[contract.IngestAccepted](t, do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{"X-Ari-Signature": hmacHex("test-ingest", body)}))
	cookie := login(t, application, "reviewer@example.com")
	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/"+created.ID, nil, cookie, nil))
	if len(bundle.Bom) != 2 {
		t.Errorf("bom lines = %d", len(bundle.Bom))
	}

	if bundle.Submission.GrantCapCents == nil || *bundle.Submission.GrantCapCents != 7500 {
		t.Errorf("grant cap = %v", bundle.Submission.GrantCapCents)
	}

	if len(bundle.PriorReviews) != 1 || bundle.PriorReviews[0].Result != "changes" {
		t.Errorf("prior reviews = %+v", bundle.PriorReviews)
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
		Decision: decisionApproved,
		IdempotencyKey: "disabled-outbound",
		NoteToMaker: "Do not deliver this.",
		Version: 1,
	})

	if outcome := decode[contract.DecisionOutcome](t, do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", body, cookie, nil)); outcome.OutboxID != nil || outcome.DeliveryStatus != nil && *outcome.DeliveryStatus != "" {
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

// TestShadowModeIsReadOnly covers the server guards, the me flag, ingest and outbox suppression.
func TestShadowModeIsReadOnly(t *testing.T) {
	application := newTestApp(t)
	application.config.Shadow = true
	cookie := login(t, application, "admin@example.com")

	if me := decode[contract.MeResponse](t, do(t, application, http.MethodGet, "/api/v1/me", nil, cookie, nil)); !me.ReadOnly {
		t.Fatal("me.readOnly = false")
	}

	paths := []struct {
		method, path string
		body []byte
	}{
		{http.MethodPost, testBase + "/submissions/sub_fixture_1/claim", nil},
		{http.MethodPost, testBase + "/submissions/sub_fixture_1/heartbeat", nil},
		{http.MethodDelete, testBase + "/submissions/sub_fixture_1/claim", nil},
		{
			http.MethodPut,
			testBase + "/submissions/sub_fixture_1/draft",
			[]byte(`{"note":"read only","acknowledgements":[]}`),
		},
		{
			http.MethodPost,
			testBase + "/submissions/sub_fixture_1/send-back",
			[]byte(`{"reason":"read only","version":1}`),
		},
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
		Decision: decisionApproved,
		IdempotencyKey: "shadow-decision",
		NoteToMaker: "This must not be recorded.",
		Version: 1,
	})

	if rejected := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", decision, cookie, nil); rejected.Code != http.StatusConflict {
		t.Fatalf("decision = %d: %s", rejected.Code, rejected.Body.String())
	}

	body := ingestBody("shadow:design")

	if accepted := do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{"X-Ari-Signature": hmacHex("test-ingest", body)}); accepted.Code != http.StatusCreated {
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
