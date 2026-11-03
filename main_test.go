package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/storage"
)

const testBase = "/api/v1/programs/half-life"

// newTestApp builds the prototype over an in-memory storage service with a fixed clock.
func newTestApp(t *testing.T) *reviewApp {
	t.Helper()

	server, err := storage.NewTestServer()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(server.Close)

	cfg := config{
		IngestSecret:     "test-ingest",
		OutboundSecret:   "test-outbound",
		OutboundURL:      "fake://receiver",
		SeedFixtures:     true,
		AllowDevAccounts: true,
	}

	application := newReviewApp(server.NewClient(server.Client()), server.Client(), cfg)
	application.clock = func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }

	if err := application.migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	return application
}

// do sends one request to the app and returns the recorder.
func do(t *testing.T, application *reviewApp, method, path string, body []byte, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	if cookie != nil {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	application.handler().ServeHTTP(recorder, request)

	return recorder
}

// login signs in through the dev endpoint and returns the session cookie.
func login(t *testing.T, application *reviewApp, email string) *http.Cookie {
	t.Helper()

	body, _ := json.Marshal(contract.LoginRequest{Email: email})
	recorder := do(t, application, http.MethodPost, "/api/v1/dev/login", body, nil, nil)

	if recorder.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", recorder.Code, recorder.Body.String())
	}

	cookies := recorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login returned no cookie")
	}

	return cookies[0]
}

// decode decodes a JSON response body.
//
//nolint:ireturn // A generic test helper has to return the caller's type.
func decode[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()

	var value T
	if err := json.Unmarshal(recorder.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}

	return value
}

// ingestBody builds a realistic PCB hand-in.
func ingestBody(externalID string) []byte {
	hours := &contract.IngestHours{ClaimedSeconds: 7 * 3600, JournalSeconds: 7 * 3600, HackatimeSeconds: 0}
	request := contract.IngestRequest{
		ExternalID:   externalID,
		Title:        "Line follower",
		Description:  "A line-following robot with a custom RP2040 board, an array of reflective sensors, a motor driver and a small OLED. The board is routed on two layers, the firmware is a first pass and the BOM prices every part.",
		RepoURL:      stringPtr("https://github.com/example/line-follower"),
		ThumbnailURL: stringPtr("https://example.com/cover.jpg"),
		Track:        stringPtr("hardware"),
		Maker:        contract.IngestMaker{Email: "maker@example.com", Name: "Maker"},
		Journals: []contract.IngestJournal{
			{At: "2026-11-02T10:00:00Z", Seconds: new(3 * 3600), Text: "Placed the RP2040, the sensor array and the motor driver. Routed the board and fixed the clearance errors the DRC found near the USB pads.", Media: []contract.IngestRequestJournalsElemMediaElem{{URL: "https://example.com/board.jpg"}}},
			{At: "2026-11-02T15:00:00Z", Seconds: new(4 * 3600), Text: "Wrote the first firmware pass and sampled the sensor array over serial. The PID loop still needs tuning, but the board boots and the display works."},
		},
		Meta: &contract.IngestMeta{
			Phase:         stringPtr("design"),
			Theme:         stringPtr("pcb"),
			Week:          new(1),
			RequestedTier: new(1),
			Hours:         hours,
			Bom: []contract.IngestBomLine{
				{Name: "RP2040", Qty: new(1), UnitUsd: new(1.20)},
				{Name: "PCB fabrication", Qty: new(5), UnitUsd: new(4.20)},
			},
		},
	}

	body, _ := json.Marshal(request)

	return body
}

// TestIngestIsSignedAndIdempotent covers the Ari edge: a valid legacy signature is accepted, a replay returns the same ship, and a second payload for an open ship conflicts.
func TestIngestIsSignedAndIdempotent(t *testing.T) {
	application := newTestApp(t)
	body := ingestBody("demo:design")

	recorder := do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("ingest = %d: %s", recorder.Code, recorder.Body.String())
	}

	created := decode[contract.IngestAccepted](t, recorder)
	if created.Status != "accepted" || created.ID == "" || created.Version != 1 {
		t.Fatalf("created = %+v", created)
	}

	replay := do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	})
	if replay.Code != http.StatusOK {
		t.Fatalf("replay = %d: %s", replay.Code, replay.Body.String())
	}

	replayed := decode[contract.IngestAccepted](t, replay)
	if replayed.ID != created.ID || replayed.Status != "duplicate" {
		t.Fatalf("replayed = %+v, created = %+v", replayed, created)
	}

	bad := do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": "deadbeef",
	})
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature = %d: %s", bad.Code, bad.Body.String())
	}

	conflictBody := ingestBody("demo:design")
	conflictBody = bytes.Replace(conflictBody, []byte("Line follower"), []byte("Line follower v2"), 1)

	conflict := do(t, application, http.MethodPost, "/api/ingest/half-life", conflictBody, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", conflictBody),
	})
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict = %d: %s", conflict.Code, conflict.Body.String())
	}

	var already map[string]any
	if err := json.Unmarshal(conflict.Body.Bytes(), &already); err != nil {
		t.Fatal(err)
	}

	if already["error"] != "already_queued" || already["id"] != created.ID {
		t.Fatalf("conflict body = %+v", already)
	}
}

// TestFixturesAppearInQueue covers the seeded queue and the redacted maker view.
func TestFixturesAppearInQueue(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	recorder := do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all", nil, cookie, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("queue = %d: %s", recorder.Code, recorder.Body.String())
	}

	queue := decode[contract.QueueResponse](t, recorder)
	if queue.Counts.Ready != 2 || queue.Counts.NeedsAttention != 1 || len(queue.Items) != 3 {
		t.Fatalf("queue counts = %+v, items = %d", queue.Counts, len(queue.Items))
	}

	bundleRecorder := do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil)
	if bundleRecorder.Code != http.StatusOK {
		t.Fatalf("bundle = %d: %s", bundleRecorder.Code, bundleRecorder.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, bundleRecorder)
	if bundle.Submission.MakerEmail != "c***@example.com" {
		t.Errorf("maker email = %q, want redacted", bundle.Submission.MakerEmail)
	}

	if bundle.Policy.HoldOnWarn != 1 || bundle.Policy.FraudMinStage != 2 {
		t.Errorf("policy = %+v", bundle.Policy)
	}

	if len(bundle.Checks) == 0 || checkStatuses(bundle.Checks)["repo.accessible"] != statusPass {
		t.Errorf("checks = %+v", bundle.Checks)
	}
}

// TestApprovalRequiresAcknowledgementAndHolds covers the warning gate and the stage 2 hold.
func TestApprovalRequiresAcknowledgementAndHolds(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, cookie, nil)
	if claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundleRecorder := do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_2", nil, cookie, nil)
	bundle := decode[contract.ReviewBundle](t, bundleRecorder)

	warnings := warningKeys(bundle.Checks)
	if len(warnings) == 0 {
		t.Fatal("fixture has no warnings")
	}

	body, _ := json.Marshal(contract.DecisionRequest{
		Decision:         decisionApproved,
		Version:          bundle.Submission.Version,
		NoteToMaker:      "Nice board.",
		InternalNote:     "Checked the routing.",
		Acknowledgements: []string{},
		IdempotencyKey:   "dec-warned-1",
	})

	refused := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", body, cookie, nil)
	if refused.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unacknowledged approval = %d: %s", refused.Code, refused.Body.String())
	}

	body, _ = json.Marshal(contract.DecisionRequest{
		Decision:         decisionApproved,
		Version:          bundle.Submission.Version,
		NoteToMaker:      "Nice board.",
		InternalNote:     "Checked the routing.",
		Acknowledgements: warnings,
		IdempotencyKey:   "dec-warned-1",
	})

	heldRecorder := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", body, cookie, nil)
	if heldRecorder.Code != http.StatusOK {
		t.Fatalf("held approval = %d: %s", heldRecorder.Code, heldRecorder.Body.String())
	}

	held := decode[contract.DecisionOutcome](t, heldRecorder)
	if held.Held != 1 || held.Status != statusStage2Hold {
		t.Fatalf("held = %+v", held)
	}

	again := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", body, cookie, nil)
	if again.Code != http.StatusOK {
		t.Fatalf("idempotent replay = %d: %s", again.Code, again.Body.String())
	}

	if replayed := decode[contract.DecisionOutcome](t, again); replayed.ID != held.ID {
		t.Errorf("replay id = %q, want %q", replayed.ID, held.ID)
	}

	outbox := do(t, application, http.MethodGet, testBase+"/outbox", nil, cookie, nil)

	deliveries := decode[contract.OutboxResponse](t, outbox)
	if len(deliveries.Deliveries) != 0 {
		t.Errorf("held decision wrote %d outbox rows", len(deliveries.Deliveries))
	}
}

// TestCleanApprovalDeliversSignedPayload covers the vertical slice: approve in one call, deliver the exact signed bytes, and read the decision back from both recovery endpoints.
func TestCleanApprovalDeliversSignedPayload(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil)
	if claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	body, _ := json.Marshal(contract.DecisionRequest{
		Decision:         decisionApproved,
		Version:          1,
		NoteToMaker:      "Clean board, clear BOM. Approved.",
		InternalNote:     "Routing and gerbers checked.",
		Acknowledgements: []string{},
		IdempotencyKey:   "dec-clean-1",
	})

	recorder := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", body, cookie, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("decision = %d: %s", recorder.Code, recorder.Body.String())
	}

	outcome := decode[contract.DecisionOutcome](t, recorder)
	if outcome.Held != 0 || outcome.Status != statusDecided || outcome.DeliveryStatus == nil || *outcome.DeliveryStatus != "delivered" {
		t.Fatalf("outcome = %+v", outcome)
	}

	replayed := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", body, cookie, nil)
	if replayed.Code != http.StatusOK {
		t.Fatalf("final idempotent replay = %d: %s", replayed.Code, replayed.Body.String())
	}

	if again := decode[contract.DecisionOutcome](t, replayed); again.ID != outcome.ID {
		t.Errorf("final replay id = %q, want %q", again.ID, outcome.ID)
	}

	receivedRecorder := do(t, application, http.MethodGet, "/api/v1/dev/outbox/received", nil, cookie, nil)
	if receivedRecorder.Code != http.StatusOK {
		t.Fatalf("received = %d: %s", receivedRecorder.Code, receivedRecorder.Body.String())
	}

	received := decode[contract.FakeDeliveriesResponse](t, receivedRecorder)
	if len(received.Deliveries) != 1 {
		t.Fatalf("received %d deliveries", len(received.Deliveries))
	}

	delivery := received.Deliveries[0]
	if delivery.Verified != 1 {
		t.Error("receiver did not verify the signature")
	}

	want := hmacHex("test-outbound", []byte(delivery.Timestamp+"."+delivery.DeliveryID+"."+delivery.Body))
	if want != delivery.Signature {
		t.Errorf("signature = %q, want %q", delivery.Signature, want)
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(delivery.Body), &event); err != nil {
		t.Fatal(err)
	}

	if event["event"] != "review.approved" {
		t.Errorf("event = %v", event["event"])
	}

	review, _ := event["review"].(map[string]any)
	if review["note_to_maker"] != "Clean board, clear BOM. Approved." {
		t.Errorf("note = %v", review["note_to_maker"])
	}

	if review["approved_seconds"] != float64(23040) {
		t.Errorf("approved seconds = %v", review["approved_seconds"])
	}

	reviewsRecorder := do(t, application, http.MethodGet, "/api/v1/reviews/half-life?since=2026-11-01T00:00:00Z", nil, nil, map[string]string{
		"Authorization": "Bearer test-ingest",
	})
	if reviewsRecorder.Code != http.StatusOK {
		t.Fatalf("reviews = %d: %s", reviewsRecorder.Code, reviewsRecorder.Body.String())
	}

	reviews := decode[contract.ReviewsResponse](t, reviewsRecorder)
	if len(reviews.Reviews) != 1 || reviews.Reviews[0].Decision != decisionApproved {
		t.Fatalf("reviews = %+v", reviews.Reviews)
	}

	statusRecorder := do(t, application, http.MethodGet, "/api/ingest/half-life/status?external_id=fix-clean:design", nil, nil, map[string]string{
		"Authorization": "Bearer test-ingest",
	})
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", statusRecorder.Code, statusRecorder.Body.String())
	}

	status := decode[contract.StatusResponse](t, statusRecorder)
	if status.Phase != "reviewed" || status.Decision == nil || *status.Decision != decisionApproved {
		t.Fatalf("status = %+v", status)
	}

	if status.Review == nil || status.Review.ApprovedSeconds != 23040 {
		t.Fatalf("status review = %+v", status.Review)
	}
}

// TestBlockingCheckRefusesApproval covers a ship the checks keep in needs_attention.
func TestBlockingCheckRefusesApproval(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_3/claim", nil, cookie, nil)
	if claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	approve, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionApproved,
		Version:        1,
		NoteToMaker:    "Approved.",
		InternalNote:   "",
		IdempotencyKey: "dec-blocked-1",
	})

	refused := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_3/decision", approve, cookie, nil)
	if refused.Code != http.StatusUnprocessableEntity {
		t.Fatalf("blocked approval = %d: %s", refused.Code, refused.Body.String())
	}

	noNote, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionRejected,
		Version:        1,
		IdempotencyKey: "dec-blocked-2",
	})

	missingNote := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_3/decision", noNote, cookie, nil)
	if missingNote.Code != http.StatusUnprocessableEntity {
		t.Fatalf("rejection without note = %d: %s", missingNote.Code, missingNote.Body.String())
	}

	reject, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionRejected,
		Version:        1,
		NoteToMaker:    "The repository is private, so the board cannot be reviewed. Make it public and resubmit.",
		IdempotencyKey: "dec-blocked-3",
	})

	rejected := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_3/decision", reject, cookie, nil)
	if rejected.Code != http.StatusOK {
		t.Fatalf("rejection = %d: %s", rejected.Code, rejected.Body.String())
	}

	outcome := decode[contract.DecisionOutcome](t, rejected)
	if outcome.Status != statusDecided || outcome.Held != 0 {
		t.Fatalf("outcome = %+v", outcome)
	}
}

// TestClaimConflict covers two reviewers racing for the same ship.
func TestClaimConflict(t *testing.T) {
	application := newTestApp(t)
	first := login(t, application, "reviewer@example.com")
	second := login(t, application, "organiser@example.com")

	claimed := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, first, nil)
	if claimed.Code != http.StatusOK {
		t.Fatalf("first claim = %d: %s", claimed.Code, claimed.Body.String())
	}

	refreshed := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, first, nil)
	if refreshed.Code != http.StatusOK {
		t.Fatalf("claim refresh = %d: %s", refreshed.Code, refreshed.Body.String())
	}

	conflict := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, second, nil)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("second claim = %d: %s", conflict.Code, conflict.Body.String())
	}
}

// TestWithdrawIsIdempotent covers the Ari withdraw path.
func TestWithdrawIsIdempotent(t *testing.T) {
	application := newTestApp(t)
	body := []byte(`{"external_id":"fix-warned:design"}`)

	for range 2 {
		recorder := do(t, application, http.MethodPost, "/api/ingest/half-life/withdraw", body, nil, map[string]string{
			"X-Ari-Signature": hmacHex("test-ingest", body),
		})
		if recorder.Code != http.StatusOK {
			t.Fatalf("withdraw = %d: %s", recorder.Code, recorder.Body.String())
		}

		outcome := decode[contract.WithdrawOutcome](t, recorder)
		if outcome.Status != "withdrawn" || outcome.ID == "" {
			t.Fatalf("withdraw outcome = %+v", outcome)
		}
	}

	status := do(t, application, http.MethodGet, "/api/ingest/half-life/status?external_id=fix-warned:design", nil, nil, map[string]string{
		"Authorization": "Bearer test-ingest",
	})
	if status.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", status.Code, status.Body.String())
	}

	state := decode[contract.StatusResponse](t, status)
	if state.Phase != "withdrawn" {
		t.Errorf("phase = %q", state.Phase)
	}
}

// TestResubmissionAfterWithdraw covers a closed ship accepting a new version.
func TestResubmissionAfterWithdraw(t *testing.T) {
	application := newTestApp(t)
	first := ingestBody("resubmit:design")

	created := do(t, application, http.MethodPost, "/api/ingest/half-life", first, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", first),
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("first ingest = %d: %s", created.Code, created.Body.String())
	}

	withdraw := []byte(`{"external_id":"resubmit:design"}`)

	closed := do(t, application, http.MethodPost, "/api/ingest/half-life/withdraw", withdraw, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", withdraw),
	})
	if closed.Code != http.StatusOK {
		t.Fatalf("withdraw = %d: %s", closed.Code, closed.Body.String())
	}

	second := bytes.Replace(first, []byte("Line follower"), []byte("Line follower v2"), 1)

	resubmitted := do(t, application, http.MethodPost, "/api/ingest/half-life", second, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", second),
	})
	if resubmitted.Code != http.StatusCreated {
		t.Fatalf("resubmit = %d: %s", resubmitted.Code, resubmitted.Body.String())
	}

	versioned := decode[contract.IngestAccepted](t, resubmitted)
	if versioned.Version != 2 {
		t.Errorf("version = %d, want 2", versioned.Version)
	}
}

// TestTimestampSignature covers the newer body signature scheme.
func TestTimestampSignature(t *testing.T) {
	application := newTestApp(t)
	body := ingestBody("timestamp:design")
	timestamp := strconv.FormatInt(application.now().Unix(), 10)
	message := append([]byte(timestamp+"."), body...)

	recorder := do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Timestamp": timestamp,
		"X-Ari-Signature": hmacHex("test-ingest", message),
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("timestamped ingest = %d: %s", recorder.Code, recorder.Body.String())
	}

	stale := do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Timestamp": "1",
		"X-Ari-Signature": hmacHex("test-ingest", []byte("1."+string(body))),
	})
	if stale.Code != http.StatusUnauthorized {
		t.Fatalf("stale timestamp = %d: %s", stale.Code, stale.Body.String())
	}
}

// TestTickIsQuietWhenNothingIsDue covers the scheduled endpoint.
func TestTickIsQuietWhenNothingIsDue(t *testing.T) {
	application := newTestApp(t)

	recorder := do(t, application, http.MethodPost, "/jobs/tick", nil, nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("tick = %d: %s", recorder.Code, recorder.Body.String())
	}

	var result map[string]int
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}

	if result["delivered"] != 0 || result["failed"] != 0 {
		t.Errorf("tick = %+v", result)
	}
}

// checkStatuses indexes checks by key.
func checkStatuses(checks []contract.CheckResult) map[string]string {
	out := make(map[string]string, len(checks))
	for _, check := range checks {
		out[check.Key] = check.Status
	}

	return out
}

// warningKeys returns the keys that need acknowledgement.
func warningKeys(checks []contract.CheckResult) []string {
	out := []string{}

	for _, check := range checks {
		if isWarning(check) {
			out = append(out, check.Key)
		}
	}

	return out
}
