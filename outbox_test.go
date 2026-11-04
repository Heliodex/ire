package main

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"review/contract"
)

// TestOutboundRetriesAfterReceiverError covers the shared outbox retry path and its stored attempt history.
func TestOutboundRetriesAfterReceiverError(t *testing.T) {
	var calls atomic.Int32

	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := calls.Add(1)

		body, _ := io.ReadAll(r.Body)
		timestamp := r.Header.Get("X-Ari-Timestamp")
		delivery := r.Header.Get("X-Ari-Delivery-Id")
		signature := r.Header.Get("X-Ari-Signature")

		if hmacHex("test-outbound", []byte(timestamp+"."+delivery+"."+string(body))) != signature {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}

		if attempt == 1 {
			http.Error(w, "try again", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)

	application := newTestApp(t)

	if _, err := application.store.Exec(t.Context(), `update program set outbound_url = ? where id = ?`, receiver.URL, programID); err != nil {
		t.Fatal(err)
	}

	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	body, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionApproved,
		Version:        1,
		NoteToMaker:    "Retry this delivery.",
		IdempotencyKey: "retry-delivery",
	})

	outcome := decode[contract.DecisionOutcome](t, do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", body, cookie, nil))
	if outcome.DeliveryStatus == nil || *outcome.DeliveryStatus != "pending" {
		t.Fatalf("first attempt = %+v", outcome)
	}

	pending := decode[contract.OutboxResponse](t, do(t, application, http.MethodGet, testBase+"/outbox", nil, cookie, nil))
	if len(pending.Deliveries) != 1 || pending.Deliveries[0].Status != "pending" || pending.Deliveries[0].Attempts != 1 {
		t.Fatalf("outbox after the first attempt = %+v", pending.Deliveries)
	}

	if pending.Deliveries[0].LastHttpStatus == nil || *pending.Deliveries[0].LastHttpStatus != http.StatusInternalServerError {
		t.Fatalf("last status = %v", pending.Deliveries[0].LastHttpStatus)
	}

	base := application.now()
	application.clock = func() time.Time { return base.Add(time.Minute) }

	if tick := do(t, application, http.MethodPost, "/jobs/tick", nil, nil, nil); tick.Code != http.StatusOK {
		t.Fatalf("tick = %d: %s", tick.Code, tick.Body.String())
	}

	delivered := decode[contract.OutboxResponse](t, do(t, application, http.MethodGet, testBase+"/outbox", nil, cookie, nil))
	if len(delivered.Deliveries) != 1 || delivered.Deliveries[0].Status != "delivered" || delivered.Deliveries[0].Attempts != 2 {
		t.Fatalf("outbox after the retry = %+v", delivered.Deliveries)
	}

	if calls.Load() != 2 {
		t.Errorf("receiver calls = %d, want 2", calls.Load())
	}
}

// TestOutboundFailsPermanentlyOn4xx covers a permanent rejection with no further attempts.
func TestOutboundFailsPermanentlyOn4xx(t *testing.T) {
	var calls atomic.Int32

	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "not acceptable", http.StatusBadRequest)
	}))
	t.Cleanup(receiver.Close)

	application := newTestApp(t)

	if _, err := application.store.Exec(t.Context(), `update program set outbound_url = ? where id = ?`, receiver.URL, programID); err != nil {
		t.Fatal(err)
	}

	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	body, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionApproved,
		Version:        1,
		NoteToMaker:    "Reject this delivery permanently.",
		IdempotencyKey: "permanent-delivery",
	})

	decode[contract.DecisionOutcome](t, do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", body, cookie, nil))

	base := application.now()
	application.clock = func() time.Time { return base.Add(time.Minute) }

	if tick := do(t, application, http.MethodPost, "/jobs/tick", nil, nil, nil); tick.Code != http.StatusOK {
		t.Fatalf("tick = %d: %s", tick.Code, tick.Body.String())
	}

	outbox := decode[contract.OutboxResponse](t, do(t, application, http.MethodGet, testBase+"/outbox", nil, cookie, nil))
	if len(outbox.Deliveries) != 1 || outbox.Deliveries[0].Status != "failed" || outbox.Deliveries[0].Attempts != 1 {
		t.Fatalf("outbox = %+v", outbox.Deliveries)
	}

	application.clock = func() time.Time { return base.Add(10 * time.Minute) }

	if tick := do(t, application, http.MethodPost, "/jobs/tick", nil, nil, nil); tick.Code != http.StatusOK {
		t.Fatalf("second tick = %d: %s", tick.Code, tick.Body.String())
	}

	if calls.Load() != 1 {
		t.Errorf("receiver calls = %d, want 1 after a permanent failure", calls.Load())
	}
}

// TestOutboundRetriesOnTransportError covers a dropped connection, which has no HTTP status.
func TestOutboundRetriesOnTransportError(t *testing.T) {
	var calls atomic.Int32

	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "cannot hijack", http.StatusInternalServerError)
				return
			}

			conn, _, err := hijacker.Hijack()
			if err != nil {
				return
			}

			_ = conn.Close()

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)

	application := newTestApp(t)

	if _, err := application.store.Exec(t.Context(), `update program set outbound_url = ? where id = ?`, receiver.URL, programID); err != nil {
		t.Fatal(err)
	}

	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	body, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionApproved,
		Version:        1,
		NoteToMaker:    "Retry the dropped connection.",
		IdempotencyKey: "dropped-delivery",
	})

	decode[contract.DecisionOutcome](t, do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", body, cookie, nil))

	first := decode[contract.OutboxResponse](t, do(t, application, http.MethodGet, testBase+"/outbox", nil, cookie, nil))
	if len(first.Deliveries) != 1 || first.Deliveries[0].Status != "pending" || first.Deliveries[0].Attempts != 1 {
		t.Fatalf("outbox after a dropped connection = %+v", first.Deliveries)
	}

	base := application.now()
	application.clock = func() time.Time { return base.Add(10 * time.Minute) }

	if tick := do(t, application, http.MethodPost, "/jobs/tick", nil, nil, nil); tick.Code != http.StatusOK {
		t.Fatalf("second tick = %d: %s", tick.Code, tick.Body.String())
	}

	second := decode[contract.OutboxResponse](t, do(t, application, http.MethodGet, testBase+"/outbox", nil, cookie, nil))
	if len(second.Deliveries) != 1 || second.Deliveries[0].Status != "delivered" || second.Deliveries[0].Attempts != 2 {
		t.Fatalf("outbox after the retry = %+v", second.Deliveries)
	}
}

// TestOutboundDryRun covers a pilot that records deliveries without reaching the receiver.
func TestOutboundDryRun(t *testing.T) {
	var calls atomic.Int32

	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)

	application := newTestApp(t)
	application.config.OutboundDryRun = true

	if _, err := application.store.Exec(t.Context(), `update program set outbound_url = ? where id = ?`, receiver.URL, programID); err != nil {
		t.Fatal(err)
	}

	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	body, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionApproved,
		Version:        1,
		NoteToMaker:    "Dry run this delivery.",
		IdempotencyKey: "dry-run-delivery",
	})

	decode[contract.DecisionOutcome](t, do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", body, cookie, nil))

	outbox := decode[contract.OutboxResponse](t, do(t, application, http.MethodGet, testBase+"/outbox", nil, cookie, nil))
	if len(outbox.Deliveries) != 1 || outbox.Deliveries[0].Status != "delivered" {
		t.Fatalf("outbox = %+v", outbox.Deliveries)
	}

	if calls.Load() != 0 {
		t.Errorf("receiver calls = %d, want 0 in a dry run", calls.Load())
	}
}
