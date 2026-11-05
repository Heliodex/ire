package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"ire/contract"
)

const machineAuth = "Bearer test-ingest"

// TestLogoutClearsSession covers the session delete and cookie clear.
func TestLogoutClearsSession(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	if me := do(t, application, http.MethodGet, "/api/v1/me", nil, cookie, nil); me.Code != http.StatusOK {
		t.Fatalf("me before logout = %d: %s", me.Code, me.Body.String())
	}

	if logout := do(t, application, http.MethodPost, "/api/v1/logout", nil, cookie, nil); logout.Code != http.StatusNoContent {
		t.Fatalf("logout = %d: %s", logout.Code, logout.Body.String())
	}

	if me := do(t, application, http.MethodGet, "/api/v1/me", nil, cookie, nil); me.Code != http.StatusUnauthorized {
		t.Errorf("me after logout = %d, want 401", me.Code)
	}

	if logout := do(t, application, http.MethodPost, "/api/v1/logout", nil, nil, nil); logout.Code != http.StatusNoContent {
		t.Errorf("anonymous logout = %d, want 204", logout.Code)
	}
}

// TestDevResetReseeds covers the rehearsal reset and its disabled branch.
func TestDevResetReseeds(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	if reset := do(t, application, http.MethodPost, "/api/v1/dev/reset", nil, cookie, nil); reset.Code != http.StatusOK {
		t.Fatalf("reset = %d: %s", reset.Code, reset.Body.String())
	}

	queue := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue", nil, cookie, nil))
	if len(queue.Items) != 3 {
		t.Errorf("queue after reset = %d items, want 3", len(queue.Items))
	}

	application.config.AllowDevAccounts = false

	if reset := do(t, application, http.MethodPost, "/api/v1/dev/reset", nil, cookie, nil); reset.Code != http.StatusNotFound {
		t.Errorf("disabled reset = %d, want 404", reset.Code)
	}
}

// TestEventsFeed covers the machine guard, the since cursor and the happy path.
func TestEventsFeed(t *testing.T) {
	application := newTestApp(t)

	if unauthorized := do(t, application, http.MethodGet, "/api/v1/events/half-life", nil, nil, nil); unauthorized.Code != http.StatusUnauthorized {
		t.Errorf("events without token = %d, want 401", unauthorized.Code)
	}

	headers := map[string]string{"Authorization": machineAuth}

	if bad := do(t, application, http.MethodGet, "/api/v1/events/half-life?since=nope", nil, nil, headers); bad.Code != http.StatusBadRequest {
		t.Errorf("events bad since = %d, want 400", bad.Code)
	}

	if unknown := do(t, application, http.MethodGet, "/api/v1/events/unknown?since=2026-01-01T00:00:00Z", nil, nil, headers); unknown.Code != http.StatusNotFound {
		t.Errorf("events unknown program = %d, want 404", unknown.Code)
	}

	response := decode[contract.EventsResponse](t, do(t, application, http.MethodGet, "/api/v1/events/half-life?since=2020-01-01T00:00:00Z", nil, nil, headers))
	if len(response.Events) == 0 {
		t.Error("events feed should not be empty for fixtures")
	}
}

// TestStatusBranches covers the machine status lookup errors and the happy path.
func TestStatusBranches(t *testing.T) {
	application := newTestApp(t)
	headers := map[string]string{"Authorization": machineAuth}

	if unauthorized := do(t, application, http.MethodGet, "/api/v1/status/half-life?external_id=fix-clean:design", nil, nil, nil); unauthorized.Code != http.StatusUnauthorized {
		t.Errorf("status without token = %d, want 401", unauthorized.Code)
	}

	if missing := do(t, application, http.MethodGet, "/api/v1/status/half-life", nil, nil, headers); missing.Code != http.StatusBadRequest {
		t.Errorf("status without id = %d, want 400", missing.Code)
	}

	if bad := do(t, application, http.MethodGet, "/api/v1/status/half-life?external_id=fix-clean:design&version=nope", nil, nil, headers); bad.Code != http.StatusBadRequest {
		t.Errorf("status bad version = %d, want 400", bad.Code)
	}

	if unknown := do(t, application, http.MethodGet, "/api/v1/status/half-life?external_id=no-such-ship", nil, nil, headers); unknown.Code != http.StatusNotFound {
		t.Errorf("status unknown ship = %d, want 404", unknown.Code)
	}

	response := decode[contract.StatusResponse](t, do(t, application, http.MethodGet, "/api/v1/status/half-life?external_id=fix-clean:design", nil, nil, headers))
	if response.ExternalID != "fix-clean:design" {
		t.Errorf("status external id = %q", response.ExternalID)
	}
}

// TestValidateIngestBranches covers every rejection in the payload validator.
func TestValidateIngestBranches(t *testing.T) {
	valid := func() contract.IngestRequest {
		var request contract.IngestRequest
		if err := json.Unmarshal(ingestBody("validate-1"), &request); err != nil {
			t.Fatal(err)
		}

		return request
	}

	cases := []struct {
		name   string
		mutate func(*contract.IngestRequest)
		field  string
	}{
		{name: "external", mutate: func(r *contract.IngestRequest) { r.ExternalID = " " }, field: "external_id"},
		{name: "title", mutate: func(r *contract.IngestRequest) { r.Title = "" }, field: "title"},
		{name: "email", mutate: func(r *contract.IngestRequest) { r.Maker.Email = "" }, field: "maker.email"},
		{name: "repo", mutate: func(r *contract.IngestRequest) { r.RepoURL = stringPtr("ftp://example.com") }, field: "repo_url"},
		{name: "demo", mutate: func(r *contract.IngestRequest) { r.DemoURL = stringPtr("notaurl") }, field: "demo_url"},
		{name: "thumb", mutate: func(r *contract.IngestRequest) { r.ThumbnailURL = stringPtr("notaurl") }, field: "thumbnail_url"},
		{name: "hours", mutate: func(r *contract.IngestRequest) { r.Meta.Hours.ClaimedSeconds = -1 }, field: "meta.hours"},
	}

	for _, test := range cases {
		recorder := httptest.NewRecorder()
		request := valid()
		test.mutate(&request)

		if validateIngest(recorder, request) {
			t.Errorf("%s should not validate", test.name)
		}

		if recorder.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d, want 422 (%s)", test.name, recorder.Code, test.field)
		}
	}

	if !validateIngest(httptest.NewRecorder(), valid()) {
		t.Error("valid request should validate")
	}
}

// TestCheckRunLifecycle covers start, finish and fail branches.
func TestCheckRunLifecycle(t *testing.T) {
	application := newTestApp(t)
	ctx := context.Background()

	ship, err := application.loadSubmission(ctx, "sub_fixture_1")
	if err != nil {
		t.Fatal(err)
	}

	if attempts := application.startCheckRun(ctx, ship); attempts < 1 {
		t.Errorf("start attempts = %d, want >= 1", attempts)
	}

	application.finishCheckRun(ctx, ship)

	var status string
	if err := application.store.QueryRow(ctx, `select status from check_run where submission_id = ? and version = ?`, ship.ID, ship.Version).Scan(&status); err != nil || status != "succeeded" {
		t.Errorf("check run status = %q %v, want succeeded", status, err)
	}

	application.failCheckRun(ctx, ship, 1, errors.New("boom"))

	if err := application.store.QueryRow(ctx, `select status from check_run where submission_id = ? and version = ?`, ship.ID, ship.Version).Scan(&status); err != nil || status != "failed" {
		t.Errorf("failed run status = %q %v, want failed", status, err)
	}

	// At max attempts the ship leaves checking and records an event.
	if _, err := application.store.Exec(ctx, `update submission set status = 'checking' where id = ?`, ship.ID); err != nil {
		t.Fatal(err)
	}

	application.failCheckRun(ctx, ship, checkRunMaxAttempts, errors.New("boom"))

	var shipStatus string
	if err := application.store.QueryRow(ctx, `select status from submission where id = ?`, ship.ID).Scan(&shipStatus); err != nil || shipStatus != statusNeedsAttention {
		t.Errorf("ship after max attempts = %q %v, want needs_attention", shipStatus, err)
	}
}

// TestJobChecksBranches covers the supervisor job validation.
func TestJobChecksBranches(t *testing.T) {
	application := newTestApp(t)

	if bad := do(t, application, http.MethodPost, "/jobs/checks", []byte(`{"submission_id":"","version":0}`), nil, nil); bad.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid job = %d, want 422", bad.Code)
	}

	if missing := do(t, application, http.MethodPost, "/jobs/checks", []byte(`{"submission_id":"sub_missing","version":1}`), nil, nil); missing.Code != http.StatusNoContent {
		t.Errorf("unknown ship job = %d, want 204", missing.Code)
	}

	// Fixture ships are not checking, so the job has nothing to write.
	if stale := do(t, application, http.MethodPost, "/jobs/checks", []byte(`{"submission_id":"sub_fixture_1","version":1}`), nil, nil); stale.Code != http.StatusNoContent {
		t.Errorf("stale job = %d, want 204", stale.Code)
	}
}

// TestHeartbeat covers the claim extension and its conflicts.
func TestHeartbeat(t *testing.T) {
	application := newTestApp(t)
	holder := login(t, application, "reviewer@example.com")
	other := login(t, application, "organiser@example.com")

	if anonymous := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/heartbeat", nil, nil, nil); anonymous.Code != http.StatusUnauthorized {
		t.Errorf("anonymous heartbeat = %d, want 401", anonymous.Code)
	}

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, holder, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	if beat := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/heartbeat", nil, holder, nil); beat.Code != http.StatusOK {
		t.Errorf("heartbeat = %d: %s", beat.Code, beat.Body.String())
	}

	if beat := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/heartbeat", nil, other, nil); beat.Code != http.StatusConflict {
		t.Errorf("foreign heartbeat = %d, want 409", beat.Code)
	}

	if missing := do(t, application, http.MethodPost, testBase+"/submissions/sub_missing/heartbeat", nil, holder, nil); missing.Code != http.StatusNotFound {
		t.Errorf("missing ship heartbeat = %d, want 404", missing.Code)
	}
}

// TestOutboundURLFallbacks covers the delivery URL chain.
func TestOutboundURLFallbacks(t *testing.T) {
	application := newTestApp(t)

	if got := application.outboundURL("missing-program"); got != "fake://receiver" {
		t.Errorf("fallback outbound = %q, want fake://receiver", got)
	}

	application.config.OutboundURL = "https://example.com/hook"
	if got := application.outboundURL("missing-program"); got != "https://example.com/hook" {
		t.Errorf("config outbound = %q", got)
	}
}

// TestProgramLookup covers the unknown-program branch.
func TestProgramLookup(t *testing.T) {
	application := newTestApp(t)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/programs/nope/queue", nil)
	request.SetPathValue("program", "nope")

	if _, ok := application.programFor(recorder, request); ok {
		t.Error("unknown program should fail lookup")
	}

	if recorder.Code != http.StatusNotFound {
		t.Errorf("unknown program = %d, want 404", recorder.Code)
	}
}

// TestRemoveHelpers covers the deploy cleanup and the secret display.
func TestRemoveHelpers(t *testing.T) {
	application := newTestApp(t)
	ctx := context.Background()

	if err := application.removePlaceholderAccounts(ctx); err != nil {
		t.Errorf("removePlaceholderAccounts: %v", err)
	}

	if err := application.removeFixtureShips(ctx); err != nil {
		t.Errorf("removeFixtureShips: %v", err)
	}

	if last4("abcdef") != "cdef" || last4("ab") != "ab" {
		t.Error("last4 mismatch")
	}
}

// TestLogAndStorageHelpers covers the two trivial error paths.
func TestLogAndStorageHelpers(t *testing.T) {
	application := newTestApp(t)
	application.logAuthProblem("test-stage", errors.New("boom"))

	recorder := httptest.NewRecorder()
	writeStorage(recorder, errors.New("db down"))

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("writeStorage = %d, want 500", recorder.Code)
	}
}
