package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"ire/contract"
)

// TestDevLoginBranches covers the disabled, unknown-email and bad-payload paths.
func TestDevLoginBranches(t *testing.T) {
	application := newTestApp(t)

	application.config.AllowDevAccounts = false
	if disabled := do(t, application, http.MethodPost, "/api/v1/dev/login", []byte(`{"email":"reviewer@example.com"}`), nil, nil); disabled.Code != http.StatusNotFound {
		t.Errorf("disabled dev login = %d, want 404", disabled.Code)
	}

	application.config.AllowDevAccounts = true

	if unknown := do(t, application, http.MethodPost, "/api/v1/dev/login", []byte(`{"email":"nobody@example.com"}`), nil, nil); unknown.Code != http.StatusUnauthorized {
		t.Errorf("unknown dev login = %d, want 401", unknown.Code)
	}

	if bad := do(t, application, http.MethodPost, "/api/v1/dev/login", []byte(`{"email":""}`), nil, nil); bad.Code != http.StatusBadRequest {
		t.Errorf("blank dev login = %d, want 400", bad.Code)
	}
}

// TestCachedGetRevalidates covers the 304 path that refreshes a stale entry.
func TestCachedGetRevalidates(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("ETag", `"v2"`)

		if r.Header.Get("If-None-Match") == `"v2"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		_, _ = io.WriteString(w, `{"default_branch":"main","full_name":"example/reval","private":false}`)
	}))
	t.Cleanup(server.Close)

	application := newTestApp(t)
	application.config.GitHubAPIBase = server.URL
	client := application.githubClient()
	ctx := t.Context()

	first, status, err := client.cachedGet(ctx, "/repos/example/reval", "application/vnd.github+json")
	if err != nil || status != http.StatusOK {
		t.Fatalf("first fetch = %d %v", status, err)
	}

	// Expire the entry by hand so the next read revalidates with the ETag.
	for key, entry := range client.cache.entries {
		entry.expires = application.now().Add(-1)
		client.cache.entries[key] = entry
	}

	second, status, err := client.cachedGet(ctx, "/repos/example/reval", "application/vnd.github+json")
	if err != nil || status != http.StatusOK {
		t.Fatalf("revalidated fetch = %d %v", status, err)
	}

	if string(first) != string(second) {
		t.Error("revalidated body should match the cached body")
	}

	if calls.Load() != 2 {
		t.Errorf("requests = %d, want 2 (initial + revalidation)", calls.Load())
	}
}

// TestDemoChecksRejectsBadURL covers the error branch without any network.
func TestDemoChecksRejectsBadURL(t *testing.T) {
	application := newTestApp(t)

	checks := application.demoChecks(t.Context(), submissionRow{ID: "x", DemoURL: "http://example.com/demo"})
	if len(checks) != 1 || checks[0].Status != statusError {
		t.Fatalf("plain-http demo = %+v, want one error check", checks)
	}

	if checks[0].Summary == "" {
		t.Error("demo error check should explain the problem")
	}
}

// TestWithdrawBranches covers the machine withdraw validation.
func TestWithdrawBranches(t *testing.T) {
	application := newTestApp(t)

	sign := func(body []byte) map[string]string {
		return map[string]string{"X-Ari-Signature": hmacHex("test-ingest", body)}
	}

	if noSig := do(t, application, http.MethodPost, "/api/ingest/half-life/withdraw", []byte(`{"external_id":"x"}`), nil, nil); noSig.Code != http.StatusUnauthorized {
		t.Errorf("unsigned withdraw = %d, want 401", noSig.Code)
	}

	unknownBody := []byte(`{"external_id":"no-such-ship"}`)
	if unknown := do(t, application, http.MethodPost, "/api/ingest/half-life/withdraw", unknownBody, nil, sign(unknownBody)); unknown.Code != http.StatusNotFound {
		t.Errorf("unknown withdraw = %d, want 404", unknown.Code)
	}

	body := []byte(`{"external_id":"fix-clean:design"}`)
	if ok := do(t, application, http.MethodPost, "/api/ingest/half-life/withdraw", body, nil, sign(body)); ok.Code != http.StatusOK {
		t.Fatalf("withdraw = %d: %s", ok.Code, ok.Body.String())
	}

	// Withdraw is idempotent: repeating it reports the same withdrawn ship.
	if again := do(t, application, http.MethodPost, "/api/ingest/half-life/withdraw", body, nil, sign(body)); again.Code != http.StatusOK {
		t.Fatalf("second withdraw = %d, want 200", again.Code)
	} else if outcome := decode[contract.WithdrawOutcome](t, again); outcome.Status != "withdrawn" {
		t.Errorf("second withdraw status = %q, want withdrawn", outcome.Status)
	}
}

// TestHCAStartWithoutConfig covers the unconfigured sign-in path.
func TestHCAStartWithoutConfig(t *testing.T) {
	application := newTestApp(t)

	if start := do(t, application, http.MethodGet, "/auth/login", nil, nil, nil); start.Code != http.StatusServiceUnavailable {
		t.Errorf("hca start without config = %d, want 503", start.Code)
	}

	if callback := do(t, application, http.MethodGet, "/auth/callback?code=x&state=y", nil, nil, nil); callback.Code != http.StatusServiceUnavailable {
		t.Errorf("hca callback without config = %d, want 503", callback.Code)
	}
}

// TestFailCheckRunTracksAttempts covers the error recording directly.
func TestFailCheckRunTracksAttempts(t *testing.T) {
	application := newTestApp(t)
	ctx := t.Context()

	ship, err := application.loadSubmission(ctx, "sub_fixture_2")
	if err != nil {
		t.Fatal(err)
	}

	// Fixture ships have no check run row until one starts, so open it first.
	application.startCheckRun(ctx, ship)
	application.failCheckRun(ctx, ship, 1, errors.New("dns boom"))

	var message string
	if err := application.store.QueryRow(ctx, `select error from check_run where submission_id = ? and version = ?`, ship.ID, ship.Version).Scan(&message); err != nil || !strings.Contains(message, "dns boom") {
		t.Errorf("check run error = %q %v", message, err)
	}
}

// TestReleaseWithoutClaim covers the no-op release path.
func TestReleaseWithoutClaim(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	if release := do(t, application, http.MethodDelete, testBase+"/submissions/sub_fixture_3/claim", nil, cookie, nil); release.Code != http.StatusNoContent {
		t.Errorf("release without claim = %d, want 204", release.Code)
	}
}

// TestQueueCounts covers the counts helper behind the queue header.
func TestQueueCounts(t *testing.T) {
	application := newTestApp(t)

	counts, err := application.queueCounts(t.Context(), programID)
	if err != nil {
		t.Fatal(err)
	}

	if counts.Ready+counts.Claimed+counts.NeedsAttention+counts.Checking+counts.Stage2Hold == 0 {
		t.Error("fixture queue should have counted ships")
	}

	var _ contract.QueueResponse
}
