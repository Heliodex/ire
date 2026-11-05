package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
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
		IngestSecret:      "test-ingest",
		OutboundSecret:    "test-outbound",
		OutboundURL:       "fake://receiver",
		SeedFixtures:      true,
		AllowDevAccounts:  true,
		DisableRepoChecks: true,
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
		ExternalID:        externalID,
		Title:             "Line follower",
		Description:       "A line-following robot with a custom RP2040 board, an array of reflective sensors, a motor driver and a small OLED. The board is routed on two layers, the firmware is a first pass and the BOM prices every part.",
		RepoURL:           stringPtr("https://github.com/example/line-follower"),
		ThumbnailURL:      stringPtr("https://example.com/cover.jpg"),
		Track:             stringPtr("hardware"),
		Maker:             contract.IngestMaker{Email: "maker@example.com", Name: "Maker"},
		HackatimeProjects: []string{"line-follower"},
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

// TestIngestRunsGitHubChecks covers the real repository checks against a fake GitHub API.
func TestIngestRunsGitHubChecks(t *testing.T) {
	var commitSince atomic.Value

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/repos/example/line-follower":
			_, _ = io.WriteString(w, `{"default_branch":"main","full_name":"example/line-follower","private":false}`)
		case "/repos/example/line-follower/readme":
			_, _ = io.WriteString(w, strings.Repeat("Build notes and wiring details. ", 12)+`\n![board](https://example.com/board.jpg)\n<img src="https://example.com/schematic.png">`)
		case "/repos/example/line-follower/contents/BOM.csv":
			_, _ = io.WriteString(w, "Part,Qty,Unit Price,Vendor\nXIAO,1,4.99,Seeed\nResistor,10,0.02,DigiKey\n")
		case "/repos/example/line-follower/git/trees/main":
			_, _ = io.WriteString(w, `{"sha":"head1","tree":[{"path":"README.md","type":"blob"},{"path":"BOM.csv","type":"blob"},{"path":"hardware/board.kicad_pcb","type":"blob"},{"path":"hardware/gerbers.zip","type":"blob"},{"path":"hardware","type":"tree"}]}`)
		case "/repos/example/line-follower/commits":
			commitSince.Store(r.URL.Query().Get("since"))

			_, _ = io.WriteString(w, `[{"sha":"sha1","commit":{"author":{"name":"Maker","date":"2026-11-01T10:00:00Z"},"message":"a"}},{"sha":"sha2","commit":{"author":{"name":"Maker","date":"2026-11-02T10:00:00Z"},"message":"b"}},{"sha":"sha3","commit":{"author":{"name":"Maker","date":"2026-11-03T10:00:00Z"},"message":"c"}}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)

	application := newTestApp(t)
	application.config.DisableRepoChecks = false
	application.config.GitHubAPIBase = fake.URL

	body := ingestBody("checks:design")

	recorder := do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("ingest = %d: %s", recorder.Code, recorder.Body.String())
	}

	created := decode[contract.IngestAccepted](t, recorder)
	cookie := login(t, application, "reviewer@example.com")
	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/"+created.ID, nil, cookie, nil))
	statuses := checkStatuses(bundle.Checks)

	for _, key := range []string{"repo.accessible", "repo.readme", "repo.readme.images", "repo.bom", "repo.design_files", "repo.commits"} {
		if statuses[key] != statusPass {
			t.Errorf("%s = %q, want pass; checks = %+v", key, statuses[key], bundle.Checks)
		}
	}

	if bundle.Evidence == nil {
		t.Fatal("bundle has no evidence snapshot")
	}

	programme, err := application.programByID(t.Context(), programID)
	if err != nil {
		t.Fatal(err)
	}

	if since, _ := commitSince.Load().(string); since == "" || since != programme.CreatedAt {
		t.Errorf("commit since = %q, want %q", since, programme.CreatedAt)
	}

	if bundle.Evidence.FullName != "example/line-follower" || len(bundle.Evidence.Files) != 4 || len(bundle.Evidence.Commits) != 3 {
		t.Fatalf("evidence = %+v", bundle.Evidence)
	}

	if bundle.Evidence.HeadSha == nil || *bundle.Evidence.HeadSha != "sha1" {
		t.Errorf("head sha = %v", bundle.Evidence.HeadSha)
	}

	missingBody := ingestBody("missing:design")
	missingBody = bytes.Replace(missingBody, []byte("example/line-follower"), []byte("missing/absent"), 1)

	missing := do(t, application, http.MethodPost, "/api/ingest/half-life", missingBody, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", missingBody),
	})
	if missing.Code != http.StatusCreated {
		t.Fatalf("missing ingest = %d: %s", missing.Code, missing.Body.String())
	}

	missingCreated := decode[contract.IngestAccepted](t, missing)

	missingBundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/"+missingCreated.ID, nil, cookie, nil))
	if missingBundle.Submission.Status != statusNeedsAttention || checkStatuses(missingBundle.Checks)["repo.accessible"] != statusFail {
		t.Fatalf("missing repo = %+v", missingBundle.Submission)
	}
}

// TestEvidenceRefresh covers the organiser-only refetch of a cached repository snapshot.
func TestEvidenceRefresh(t *testing.T) {
	var reads atomic.Int32

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/repos/example/line-follower":
			_, _ = io.WriteString(w, `{"default_branch":"main","full_name":"example/line-follower","private":false}`)
		case "/repos/example/line-follower/readme":
			reads.Add(1)

			_, _ = io.WriteString(w, strings.Repeat("Build notes and wiring details. ", 12)+`\n![board](https://example.com/board.jpg)`)
		case "/repos/example/line-follower/contents/BOM.csv":
			_, _ = io.WriteString(w, "Part,Qty,Unit Price,Vendor\nXIAO,1,4.99,Seeed\nResistor,10,0.02,DigiKey\n")
		case "/repos/example/line-follower/git/trees/main":
			_, _ = io.WriteString(w, `{"sha":"head1","tree":[{"path":"README.md","type":"blob"},{"path":"BOM.csv","type":"blob"}]}`)
		case "/repos/example/line-follower/commits":
			_, _ = io.WriteString(w, `[{"sha":"sha1","commit":{"author":{"name":"Maker","date":"2026-11-01T10:00:00Z"},"message":"First"}}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)

	application := newTestApp(t)
	application.config.DisableRepoChecks = false
	application.config.GitHubAPIBase = fake.URL

	body := ingestBody("refresh:design")

	created := decode[contract.IngestAccepted](t, do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	}))
	if created.ID == "" {
		t.Fatal("ingest did not create a ship")
	}

	reviewer := login(t, application, "reviewer@example.com")
	organiser := login(t, application, "organiser@example.com")

	forbidden := do(t, application, http.MethodPost, testBase+"/submissions/"+created.ID+"/evidence/refresh", nil, reviewer, nil)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("reviewer refresh = %d: %s", forbidden.Code, forbidden.Body.String())
	}

	before := reads.Load()

	refreshed := do(t, application, http.MethodPost, testBase+"/submissions/"+created.ID+"/evidence/refresh", nil, organiser, nil)
	if refreshed.Code != http.StatusOK {
		t.Fatalf("refresh = %d: %s", refreshed.Code, refreshed.Body.String())
	}

	evidence := decode[contract.Evidence](t, refreshed)
	if evidence.FullName != "example/line-follower" || len(evidence.Files) != 2 {
		t.Fatalf("evidence = %+v", evidence)
	}

	if reads.Load() != before+1 {
		t.Errorf("readme reads = %d, want %d", reads.Load(), before+1)
	}

	var rows int
	if err := application.store.QueryRow(t.Context(), `select count(*) from repo_snapshot where submission_id = ?`, created.ID).Scan(&rows); err != nil {
		t.Fatal(err)
	}

	if rows != 1 {
		t.Errorf("snapshot rows = %d, want 1", rows)
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/"+created.ID, nil, reviewer, nil))
	if bundle.Evidence == nil || bundle.Evidence.FetchedAt != evidence.FetchedAt {
		t.Fatalf("bundle evidence = %+v", bundle.Evidence)
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
	if bundle.Submission.MakerEmail != "c6h@example.com" {
		t.Errorf("maker email = %q, want full email", bundle.Submission.MakerEmail)
	}
	if bundle.Submission.MakerSlackID == nil || *bundle.Submission.MakerSlackID != "U0C6HSTAR" {
		t.Errorf("maker slack = %+v, want U0C6HSTAR", bundle.Submission.MakerSlackID)
	}

	if bundle.Policy.HoldOnWarn != 1 || bundle.Policy.FraudMinStage != 2 {
		t.Errorf("policy = %+v", bundle.Policy)
	}

	if len(bundle.Checks) == 0 || checkStatuses(bundle.Checks)["repo.accessible"] != statusPass {
		t.Errorf("checks = %+v", bundle.Checks)
	}

	if len(queue.Items) == 0 || len(queue.Items[0].Checks) != 7 {
		t.Errorf("queue check dots = %+v", queue.Items)
	}

	if len(queue.Items) == 0 || queue.Items[0].ThumbnailURL == nil || *queue.Items[0].ThumbnailURL == "" {
		t.Errorf("queue thumbnails = %+v", queue.Items)
	}

	if len(queue.Items) > 1 && queueCheckStatuses(queue.Items[1].Checks)["repo.bom"] != statusFail {
		t.Errorf("warned queue checks = %+v", queue.Items[1].Checks)
	}

	statusRecorder := do(t, application, http.MethodGet, "/api/ingest/half-life/status?external_id=fix-clean:design", nil, nil, map[string]string{
		"Authorization": "Bearer test-ingest",
	})
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", statusRecorder.Code, statusRecorder.Body.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}

	if value, ok := raw["decision"]; !ok || value != nil {
		t.Errorf("decision = %v, want explicit null", value)
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

// TestChangesOnWarnedShipIsFinal covers a non-approval decision: it must not be swallowed by the stage 2 hold.
func TestChangesOnWarnedShipIsFinal(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	body, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionChanges,
		Version:        1,
		NoteToMaker:    "Add the BOM to the repository and replace the generic journal entry.",
		IdempotencyKey: "changes-warned-1",
	})

	recorder := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", body, cookie, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("changes = %d: %s", recorder.Code, recorder.Body.String())
	}

	outcome := decode[contract.DecisionOutcome](t, recorder)
	if outcome.Held != 0 || outcome.Status != statusDecided || outcome.DeliveryStatus == nil || *outcome.DeliveryStatus != "delivered" {
		t.Fatalf("outcome = %+v", outcome)
	}

	outbox := decode[contract.OutboxResponse](t, do(t, application, http.MethodGet, testBase+"/outbox", nil, cookie, nil))
	if len(outbox.Deliveries) != 1 || outbox.Deliveries[0].Event != "review.changes" {
		t.Fatalf("outbox = %+v", outbox.Deliveries)
	}
}

// TestHeldShipCannotBeDecidedAtStageOne covers the stage boundary: a second-pass ship accepts no stage 1 claim or decision.
func TestHeldShipCannotBeDecidedAtStageOne(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	approve, _ := json.Marshal(contract.DecisionRequest{
		Decision:         decisionApproved,
		Version:          1,
		NoteToMaker:      "Nice board.",
		Acknowledgements: []string{"repo.readme.images", "repo.bom", "evidence.journals"},
		IdempotencyKey:   "held-once",
	})

	held := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", approve, cookie, nil)
	if held.Code != http.StatusOK {
		t.Fatalf("held approval = %d: %s", held.Code, held.Body.String())
	}

	again, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionApproved,
		Version:        1,
		NoteToMaker:    "Approved again with fewer hours.",
		IdempotencyKey: "held-twice",
	})

	redecide := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", again, cookie, nil)
	if redecide.Code != http.StatusForbidden {
		t.Fatalf("stage 1 redecision = %d: %s", redecide.Code, redecide.Body.String())
	}

	reclaim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, cookie, nil)
	if reclaim.Code != http.StatusForbidden {
		t.Fatalf("stage 1 reclaim = %d: %s", reclaim.Code, reclaim.Body.String())
	}
}

// TestStageTwoConfirmsHeldApproval covers the full second-pass loop: reviewer holds, organiser claims and confirms, one final event.
func TestStageTwoConfirmsHeldApproval(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	organiser := login(t, application, "organiser@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("first-pass claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_2", nil, reviewer, nil))

	warnings := warningKeys(bundle.Checks)
	if len(warnings) == 0 {
		t.Fatal("fixture has no warnings")
	}

	approve, _ := json.Marshal(contract.DecisionRequest{
		Decision:         decisionApproved,
		Version:          1,
		NoteToMaker:      "First pass approves the hours.",
		Acknowledgements: warnings,
		IdempotencyKey:   "stage1-hold",
	})

	held := decode[contract.DecisionOutcome](t, do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", approve, reviewer, nil))
	if held.Held != 1 || held.Status != statusStage2Hold {
		t.Fatalf("held outcome = %+v", held)
	}

	queueRecorder := do(t, application, http.MethodGet, testBase+"/queue?stage=2&filter=all", nil, organiser, nil)
	if queueRecorder.Code != http.StatusOK {
		t.Fatalf("second-pass queue = %d: %s", queueRecorder.Code, queueRecorder.Body.String())
	}

	queue := decode[contract.QueueResponse](t, queueRecorder)
	if len(queue.Items) != 1 || queue.Items[0].Status != statusStage2Hold {
		t.Fatalf("second-pass queue = %+v", queue.Items)
	}

	reviewerQueue := do(t, application, http.MethodGet, testBase+"/queue?stage=2&filter=all", nil, reviewer, nil)
	if reviewerQueue.Code != http.StatusForbidden {
		t.Fatalf("reviewer second-pass queue = %d: %s", reviewerQueue.Code, reviewerQueue.Body.String())
	}

	reviewerClaim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, reviewer, nil)
	if reviewerClaim.Code != http.StatusForbidden {
		t.Fatalf("reviewer second-pass claim = %d: %s", reviewerClaim.Code, reviewerClaim.Body.String())
	}

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, organiser, nil); claim.Code != http.StatusOK {
		t.Fatalf("second-pass claim = %d: %s", claim.Code, claim.Body.String())
	}

	confirm, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionApproved,
		Version:        1,
		NoteToMaker:    "Second pass confirms the approval.",
		InternalNote:   "Checked the held warnings.",
		IdempotencyKey: "stage2-1",
	})

	confirmed := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", confirm, organiser, nil)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("second-pass decision = %d: %s", confirmed.Code, confirmed.Body.String())
	}

	outcome := decode[contract.DecisionOutcome](t, confirmed)
	if outcome.Held != 0 || outcome.Status != statusDecided || outcome.DeliveryStatus == nil || *outcome.DeliveryStatus != "delivered" {
		t.Fatalf("second-pass outcome = %+v", outcome)
	}

	replayed := decode[contract.DecisionOutcome](t, do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", confirm, organiser, nil))
	if replayed.ID != outcome.ID {
		t.Errorf("replay id = %q, want %q", replayed.ID, outcome.ID)
	}

	outbox := decode[contract.OutboxResponse](t, do(t, application, http.MethodGet, testBase+"/outbox", nil, organiser, nil))
	if len(outbox.Deliveries) != 1 {
		t.Fatalf("outbox = %+v", outbox.Deliveries)
	}

	received := decode[contract.FakeDeliveriesResponse](t, do(t, application, http.MethodGet, "/api/v1/dev/outbox/received", nil, organiser, nil))
	if len(received.Deliveries) != 1 {
		t.Fatalf("received %d deliveries", len(received.Deliveries))
	}

	var event struct {
		Event        string `json:"event"`
		Stage        int    `json:"stage"`
		StageHistory []struct {
			Stage    int    `json:"stage"`
			Decision string `json:"decision"`
		} `json:"stage_history"`
		Review struct {
			ApprovedSeconds int    `json:"approved_seconds"`
			NoteToMaker     string `json:"note_to_maker"`
		} `json:"review"`
	}
	if err := json.Unmarshal([]byte(received.Deliveries[0].Body), &event); err != nil {
		t.Fatal(err)
	}

	if event.Event != "review.approved" || event.Stage != 2 || len(event.StageHistory) != 2 {
		t.Fatalf("event = %+v", event)
	}

	if event.StageHistory[0].Stage != 1 || event.StageHistory[1].Stage != 2 {
		t.Errorf("stage history = %+v", event.StageHistory)
	}

	if event.Review.ApprovedSeconds != bundle.Submission.HoursClaimedSeconds {
		t.Errorf("approved seconds = %d, want %d", event.Review.ApprovedSeconds, bundle.Submission.HoursClaimedSeconds)
	}

	status := decode[contract.StatusResponse](t, do(t, application, http.MethodGet, "/api/ingest/half-life/status?external_id=fix-warned:design", nil, nil, map[string]string{
		"Authorization": "Bearer test-ingest",
	}))
	if status.Phase != "reviewed" || status.Decision == nil || *status.Decision != decisionApproved || status.Stage != 2 {
		t.Fatalf("status = %+v", status)
	}

	if len(status.StageHistory) != 2 {
		t.Errorf("status stage history = %+v", status.StageHistory)
	}
}

// TestStageTwoRejectsOwnDecision covers the separation of duties: the organiser who held the ship cannot confirm it.
func TestStageTwoRejectsOwnDecision(t *testing.T) {
	application := newTestApp(t)
	organiser := login(t, application, "organiser@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, organiser, nil); claim.Code != http.StatusOK {
		t.Fatalf("first-pass claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_2", nil, organiser, nil))
	approve, _ := json.Marshal(contract.DecisionRequest{
		Decision:         decisionApproved,
		Version:          1,
		NoteToMaker:      "Held by the organiser.",
		Acknowledgements: warningKeys(bundle.Checks),
		IdempotencyKey:   "own-hold",
	})

	held := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", approve, organiser, nil)
	if held.Code != http.StatusOK {
		t.Fatalf("held decision = %d: %s", held.Code, held.Body.String())
	}

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/claim", nil, organiser, nil); claim.Code != http.StatusOK {
		t.Fatalf("second-pass claim = %d: %s", claim.Code, claim.Body.String())
	}

	confirm, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionApproved,
		Version:        1,
		NoteToMaker:    "Confirm my own decision.",
		IdempotencyKey: "own-confirm",
	})

	rejected := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_2/decision", confirm, organiser, nil)
	if rejected.Code != http.StatusUnprocessableEntity {
		t.Fatalf("own decision = %d: %s", rejected.Code, rejected.Body.String())
	}

	var body contract.ErrorResponse
	if err := json.Unmarshal(rejected.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if body.Code != "own_decision" {
		t.Errorf("error code = %q", body.Code)
	}
}

// TestAuthConfigReportsMethods covers the unauthenticated method discovery used by the login screen.
func TestAuthConfigReportsMethods(t *testing.T) {
	application := newTestApp(t)

	config := decode[contract.AuthConfig](t, do(t, application, http.MethodGet, "/api/v1/auth/config", nil, nil, nil))
	if !config.Dev || config.Hca {
		t.Fatalf("config = %+v", config)
	}

	application.config.HCClientID = "client"
	application.config.HCClientSecret = "secret"
	application.config.HCRedirectURI = "http://review.localhost/auth/callback"

	config = decode[contract.AuthConfig](t, do(t, application, http.MethodGet, "/api/v1/auth/config", nil, nil, nil))
	if !config.Hca {
		t.Fatalf("config = %+v", config)
	}
}

// TestHTTPOutboundDelivery covers a real receiver checking the signature.
func TestHTTPOutboundDelivery(t *testing.T) {
	var (
		received  string
		forwarded bool
	)

	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		timestamp := r.Header.Get("X-Ari-Timestamp")
		delivery := r.Header.Get("X-Ari-Delivery-Id")
		signature := r.Header.Get("X-Ari-Signature")

		if hmacHex("test-outbound", []byte(timestamp+"."+delivery+"."+string(body))) != signature {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}

		received = string(body)
		forwarded = true

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
		NoteToMaker:    "Sent over HTTP.",
		IdempotencyKey: "http-delivery",
	})

	outcome := decode[contract.DecisionOutcome](t, do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", body, cookie, nil))
	if outcome.DeliveryStatus == nil || *outcome.DeliveryStatus != "delivered" || !forwarded {
		t.Fatalf("outcome = %+v forwarded=%v", outcome, forwarded)
	}

	if !strings.Contains(received, `"event":"review.approved"`) {
		t.Fatalf("received = %s", received)
	}
}

// findCookie returns a named cookie from a response.
func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}

	return nil
}

// TestPageGate covers the server-selected login and protected pages.
func TestPageGate(t *testing.T) {
	application := newTestApp(t)

	anonymous := do(t, application, http.MethodGet, "/", nil, nil, nil)
	if anonymous.Code != http.StatusOK || anonymous.Header().Get(app.StaticPageHeader) != "/" {
		t.Fatalf("anonymous root = %d %q", anonymous.Code, anonymous.Header().Get(app.StaticPageHeader))
	}

	protected := do(t, application, http.MethodGet, "/queue", nil, nil, nil)
	if protected.Code != http.StatusFound || protected.Header().Get("Location") != "/" {
		t.Fatalf("anonymous queue = %d %q", protected.Code, protected.Header().Get("Location"))
	}

	cookie := login(t, application, "reviewer@example.com")

	signedIn := do(t, application, http.MethodGet, "/", nil, cookie, nil)
	if signedIn.Code != http.StatusFound || signedIn.Header().Get("Location") != "/queue" {
		t.Fatalf("signed-in root = %d %q", signedIn.Code, signedIn.Header().Get("Location"))
	}

	queue := do(t, application, http.MethodGet, "/queue", nil, cookie, nil)
	if queue.Code != http.StatusOK || queue.Header().Get(app.StaticPageHeader) != "/queue" {
		t.Fatalf("signed-in queue = %d %q", queue.Code, queue.Header().Get(app.StaticPageHeader))
	}

	review := do(t, application, http.MethodGet, "/review/sub_fixture_1", nil, cookie, nil)
	if review.Code != http.StatusOK || review.Header().Get(app.StaticPageHeader) != "/review" {
		t.Fatalf("signed-in review = %d %q", review.Code, review.Header().Get(app.StaticPageHeader))
	}

	oldReview := do(t, application, http.MethodGet, "/review?id=sub_fixture_1", nil, cookie, nil)
	if oldReview.Code != http.StatusFound || oldReview.Header().Get("Location") != "/review/sub_fixture_1" {
		t.Fatalf("old review = %d %q", oldReview.Code, oldReview.Header().Get("Location"))
	}

	guestReview := do(t, application, http.MethodGet, "/review/sub_fixture_1", nil, nil, nil)
	if guestReview.Code != http.StatusFound || guestReview.Header().Get("Location") != "/" {
		t.Fatalf("guest review = %d %q", guestReview.Code, guestReview.Header().Get("Location"))
	}

	login := do(t, application, http.MethodGet, "/login", nil, cookie, nil)
	if login.Code != http.StatusFound || login.Header().Get("Location") != "/queue" {
		t.Fatalf("signed-in login = %d %q", login.Code, login.Header().Get("Location"))
	}

	guestLogin := do(t, application, http.MethodGet, "/login?error=state", nil, nil, nil)
	if guestLogin.Code != http.StatusFound || guestLogin.Header().Get("Location") != "/?error=state" {
		t.Fatalf("guest login = %d %q", guestLogin.Code, guestLogin.Header().Get("Location"))
	}
}

// TestCommitSinceUsesPreviousApproval covers the resubmission commit window.
func TestCommitSinceUsesPreviousApproval(t *testing.T) {
	application := newTestApp(t)
	approved := "2026-10-01T09:00:00.000000000Z"

	if _, err := application.store.Exec(t.Context(), `update submission set decided_at = ? where id = ?`, approved, "sub_fixture_1"); err != nil {
		t.Fatal(err)
	}

	if got := application.commitSince(t.Context(), programID, "sub_fixture_1"); got != approved {
		t.Errorf("since = %q, want %q", got, approved)
	}

	programme, err := application.programByID(t.Context(), programID)
	if err != nil {
		t.Fatal(err)
	}

	if got := application.commitSince(t.Context(), programID, ""); got != programme.CreatedAt {
		t.Errorf("first version since = %q, want %q", got, programme.CreatedAt)
	}
}

// TestDraftSurvivesReload covers the server-side draft the review page restores.
func TestDraftSurvivesReload(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	draft, _ := json.Marshal(contract.DraftRequest{Note: "Check the USB pads.", InternalNote: "Routing looks fine."})
	if saved := do(t, application, http.MethodPut, testBase+"/submissions/sub_fixture_1/draft", draft, cookie, nil); saved.Code != http.StatusOK {
		t.Fatalf("draft = %d: %s", saved.Code, saved.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))
	if bundle.Draft == nil || bundle.Draft.Note != "Check the USB pads." || bundle.Draft.InternalNote != "Routing looks fine." {
		t.Fatalf("draft = %+v", bundle.Draft)
	}
}

// TestOutboxRedactsMakerEmail covers delivery debug output: reviewers see a redacted address, organisers see the bytes that were signed.
func TestOutboxRedactsMakerEmail(t *testing.T) {
	application := newTestApp(t)
	reviewerCookie := login(t, application, "reviewer@example.com")
	organiserCookie := login(t, application, "organiser@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, reviewerCookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	body, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionApproved,
		Version:        1,
		NoteToMaker:    "Clean.",
		IdempotencyKey: "redact-1",
	})

	if decision := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", body, reviewerCookie, nil); decision.Code != http.StatusOK {
		t.Fatalf("decision = %d: %s", decision.Code, decision.Body.String())
	}

	reviewerView := decode[contract.OutboxResponse](t, do(t, application, http.MethodGet, testBase+"/outbox", nil, reviewerCookie, nil))
	if len(reviewerView.Deliveries) != 1 || reviewerView.Deliveries[0].Payload == nil {
		t.Fatalf("reviewer outbox = %+v", reviewerView.Deliveries)
	}

	var reviewerPayload map[string]any
	if err := json.Unmarshal([]byte(*reviewerView.Deliveries[0].Payload), &reviewerPayload); err != nil {
		t.Fatal(err)
	}

	if email := payloadMakerEmail(t, reviewerPayload); email != "c***@example.com" {
		t.Errorf("reviewer maker email = %v", email)
	}

	organiserView := decode[contract.OutboxResponse](t, do(t, application, http.MethodGet, testBase+"/outbox", nil, organiserCookie, nil))

	var organiserPayload map[string]any
	if err := json.Unmarshal([]byte(*organiserView.Deliveries[0].Payload), &organiserPayload); err != nil {
		t.Fatal(err)
	}

	if email := payloadMakerEmail(t, organiserPayload); email != "c6h@example.com" {
		t.Errorf("organiser maker email = %v", email)
	}
}

// payloadMakerEmail reads the maker email from a decoded outbound event, failing the test when the shape is wrong.
func payloadMakerEmail(t *testing.T, event map[string]any) any {
	t.Helper()

	maker, ok := event["maker"].(map[string]any)
	if !ok {
		t.Fatalf("maker = %v", event["maker"])
	}

	return maker["email"]
}

// TestOutboundCarriesHackatimeProjects covers the field Half Life sends and expects mirrored back.
func TestOutboundCarriesHackatimeProjects(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")
	body := ingestBody("hackatime:design")

	ingested := do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	})
	if ingested.Code != http.StatusCreated {
		t.Fatalf("ingest = %d: %s", ingested.Code, ingested.Body.String())
	}

	created := decode[contract.IngestAccepted](t, ingested)
	if claim := do(t, application, http.MethodPost, testBase+"/submissions/"+created.ID+"/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	decide, _ := json.Marshal(contract.DecisionRequest{Decision: decisionApproved, Version: 1, NoteToMaker: "Good.", IdempotencyKey: "hackatime-1"})
	if decision := do(t, application, http.MethodPost, testBase+"/submissions/"+created.ID+"/decision", decide, cookie, nil); decision.Code != http.StatusOK {
		t.Fatalf("decision = %d: %s", decision.Code, decision.Body.String())
	}

	received := decode[contract.FakeDeliveriesResponse](t, do(t, application, http.MethodGet, "/api/v1/dev/outbox/received", nil, cookie, nil))
	if len(received.Deliveries) != 1 {
		t.Fatalf("received = %d", len(received.Deliveries))
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(received.Deliveries[0].Body), &event); err != nil {
		t.Fatal(err)
	}

	projects, ok := event["ship"].(map[string]any)["hackatime_projects"].([]any)
	if !ok || len(projects) != 1 || projects[0] != "line-follower" {
		t.Errorf("hackatime projects = %v", event["ship"])
	}
}

// TestLegacySignatureFailsClosed covers a missing setting: a body-only signature is rejected rather than accepted.
func TestLegacySignatureFailsClosed(t *testing.T) {
	application := newTestApp(t)

	if _, err := application.store.Exec(t.Context(), `delete from program_setting where key = 'allow_legacy_signature'`); err != nil {
		t.Fatal(err)
	}

	body := ingestBody("legacy:design")

	recorder := do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	})
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("legacy signature = %d: %s", recorder.Code, recorder.Body.String())
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

// queueCheckStatuses indexes the compact queue checks by key.
func queueCheckStatuses(checks []contract.QueueCheck) map[string]string {
	out := make(map[string]string, len(checks))
	for _, check := range checks {
		out[check.Key] = check.Status
	}

	return out
}

// warningKeys returns the keys that need acknowledgement.
func warningKeys(checks []contract.CheckResult) []string {
	var out []string

	for _, check := range checks {
		if isWarning(check) {
			out = append(out, check.Key)
		}
	}

	return out
}
