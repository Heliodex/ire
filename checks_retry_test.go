package main

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
)

// flakyGitHub serves one repository whose metadata and commit endpoints can fail on demand.
type flakyGitHub struct {
	server                *httptest.Server
	failMeta, failCommits atomic.Bool
}

func newFlakyGitHub(t *testing.T) *flakyGitHub {
	t.Helper()

	fake := &flakyGitHub{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/repos/example/retry":
			if fake.failMeta.Load() {
				http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
				return
			}

			_, _ = io.WriteString(w, `{"default_branch":"main","full_name":"example/retry","private":false}`)
		case "/repos/example/retry/readme":
			_, _ = io.WriteString(w, strings.Repeat("Build notes and wiring details. ", 12)+"\n![board](https://example.com/board.jpg)\n![schematic](https://example.com/schematic.png)")
		case "/repos/example/retry/contents/BOM.csv":
			_, _ = io.WriteString(w, "Part,Qty,Unit Price,Vendor\nXIAO,1,4.99,Seeed\n")
		case "/repos/example/retry/git/trees/main":
			_, _ = io.WriteString(w, `{"sha":"head1","tree":[{"path":"README.md","type":"blob"},{"path":"BOM.csv","type":"blob"},{"path":"board.kicad_pcb","type":"blob"}]}`)
		case "/repos/example/retry/commits":
			if fake.failCommits.Load() {
				http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
				return
			}

			_, _ = io.WriteString(w, `[{"sha":"sha1","commit":{"author":{"name":"Maker","date":"2026-11-01T10:00:00Z"},"message":"First"}}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.server.Close)

	return fake
}

// retryTestApp builds an app that captures enqueued jobs instead of running them.
func retryTestApp(t *testing.T, fake *flakyGitHub, jobs *[]app.JobRequest) *ireApp {
	t.Helper()

	application := newTestApp(t)
	application.config.DisableRepoChecks = false
	application.config.GitHubAPIBase = fake.server.URL
	application.config.EnableJobs = true
	application.config.Enqueue = func(_ context.Context, request app.JobRequest) (bool, error) {
		*jobs = append(*jobs, request)
		return true, nil
	}

	return application
}

// ingestRetryShip ingests a ship against the flaky forge.
func ingestRetryShip(t *testing.T, application *ireApp, externalID, repo string) string {
	t.Helper()

	body := ingestBody(externalID)
	body = []byte(strings.Replace(string(body), "example/line-follower", repo, 1))

	created := decode[contract.IngestAccepted](t, do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	}))

	return created.ID
}

// runCheckJob runs the queued check job for a ship.
func runCheckJob(t *testing.T, application *ireApp, submissionID string) {
	t.Helper()

	payload, _ := json.Marshal(checkJob{SubmissionID: submissionID, Version: 1})
	if run := do(t, application, http.MethodPost, "/jobs/checks", payload, nil, nil); run.Code != http.StatusNoContent {
		t.Fatalf("job run = %d: %s", run.Code, run.Body.String())
	}
}

// postTick runs the schedule tick.
func postTick(t *testing.T, application *ireApp) {
	t.Helper()

	if recorder := do(t, application, http.MethodPost, "/jobs/tick", nil, nil, nil); recorder.Code != http.StatusOK {
		t.Fatalf("tick = %d: %s", recorder.Code, recorder.Body.String())
	}
}

// shipStatus reads a ship's status as a reviewer.
func shipStatus(t *testing.T, application *ireApp, submissionID string) string {
	t.Helper()

	cookie := login(t, application, "reviewer@example.com")
	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/"+submissionID, nil, cookie, nil))

	return bundle.Submission.Status
}

// retryEventCount counts automatic retry events for a ship.
func retryEventCount(t *testing.T, application *ireApp, submissionID string) int {
	t.Helper()

	var count int
	if err := application.store.QueryRow(context.Background(), `select count(*) from submission_event where submission_id = ? and kind = 'checks_retry'`, submissionID).Scan(&count); err != nil {
		t.Fatal(err)
	}

	return count
}

// TestTickRetriesErroredRepoAccess covers the automatic rerun of a ship stuck by a forge outage.
func TestTickRetriesErroredRepoAccess(t *testing.T) {
	fake := newFlakyGitHub(t)
	fake.failMeta.Store(true)

	var jobs []app.JobRequest

	application := retryTestApp(t, fake, &jobs)
	base := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

	id := ingestRetryShip(t, application, "retry:design", "example/retry")
	runCheckJob(t, application, id)

	if status := shipStatus(t, application, id); status != statusNeedsAttention {
		t.Fatalf("status = %q, want needs_attention", status)
	}

	postTick(t, application)

	if status := shipStatus(t, application, id); status != statusNeedsAttention {
		t.Fatalf("status before backoff = %q, want needs_attention", status)
	}

	if count := retryEventCount(t, application, id); count != 0 {
		t.Fatalf("retries before backoff = %d, want 0", count)
	}

	application.clock = func() time.Time { return base.Add(6 * time.Minute) }

	postTick(t, application)

	if status := shipStatus(t, application, id); status != statusChecking {
		t.Fatalf("status after backoff = %q, want checking", status)
	}

	if count := retryEventCount(t, application, id); count != 1 {
		t.Fatalf("retries = %d, want 1", count)
	}

	if len(jobs) != 2 || jobs[1].IdempotencyKey != "checks:"+id+":1:retry:1" {
		t.Fatalf("jobs = %+v", jobs)
	}

	fake.failMeta.Store(false)
	runCheckJob(t, application, id)

	if status := shipStatus(t, application, id); status != statusReady {
		t.Fatalf("status after recovery = %q, want ready", status)
	}

	application.clock = func() time.Time { return base.Add(48 * time.Hour) }

	postTick(t, application)

	if count := retryEventCount(t, application, id); count != 1 {
		t.Fatalf("retries after recovery = %d, want 1", count)
	}

	if len(jobs) != 2 {
		t.Fatalf("jobs after recovery = %+v", jobs)
	}
}

// TestTickRetriesErroredCommitsOnReadyShip covers the rerun of a reviewable ship with incomplete evidence.
func TestTickRetriesErroredCommitsOnReadyShip(t *testing.T) {
	fake := newFlakyGitHub(t)
	fake.failCommits.Store(true)

	var jobs []app.JobRequest

	application := retryTestApp(t, fake, &jobs)
	base := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

	id := ingestRetryShip(t, application, "retry:commits", "example/retry")
	runCheckJob(t, application, id)

	if status := shipStatus(t, application, id); status != statusReady {
		t.Fatalf("status = %q, want ready", status)
	}

	cookie := login(t, application, "reviewer@example.com")
	if claim := do(t, application, http.MethodPost, testBase+"/submissions/"+id+"/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	application.clock = func() time.Time { return base.Add(6 * time.Minute) }

	postTick(t, application)

	if status := shipStatus(t, application, id); status != statusClaimed {
		t.Fatalf("status while claimed = %q, want claimed", status)
	}

	if count := retryEventCount(t, application, id); count != 0 {
		t.Fatalf("retries while claimed = %d, want 0", count)
	}

	if release := do(t, application, http.MethodDelete, testBase+"/submissions/"+id+"/claim", nil, cookie, nil); release.Code != http.StatusNoContent {
		t.Fatalf("release = %d: %s", release.Code, release.Body.String())
	}

	postTick(t, application)

	if status := shipStatus(t, application, id); status != statusChecking {
		t.Fatalf("status after release = %q, want checking", status)
	}

	if count := retryEventCount(t, application, id); count != 1 {
		t.Fatalf("retries after release = %d, want 1", count)
	}
}

// TestTickSkipsDeterministicFailures ensures a private or missing repository is never retried automatically.
func TestTickSkipsDeterministicFailures(t *testing.T) {
	fake := newFlakyGitHub(t)

	var jobs []app.JobRequest

	application := retryTestApp(t, fake, &jobs)
	base := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

	id := ingestRetryShip(t, application, "retry:missing", "example/missing")
	runCheckJob(t, application, id)

	if status := shipStatus(t, application, id); status != statusNeedsAttention {
		t.Fatalf("status = %q, want needs_attention", status)
	}

	application.clock = func() time.Time { return base.Add(48 * time.Hour) }

	postTick(t, application)

	if status := shipStatus(t, application, id); status != statusNeedsAttention {
		t.Fatalf("status = %q, want needs_attention", status)
	}

	if count := retryEventCount(t, application, id); count != 0 {
		t.Fatalf("retries = %d, want 0", count)
	}

	if len(jobs) != 1 {
		t.Fatalf("jobs = %+v", jobs)
	}
}

// TestTickCapsAutomaticRetries ensures a permanently broken forge stops retrying after five attempts.
func TestTickCapsAutomaticRetries(t *testing.T) {
	fake := newFlakyGitHub(t)
	fake.failMeta.Store(true)

	var jobs []app.JobRequest

	application := retryTestApp(t, fake, &jobs)
	base := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

	id := ingestRetryShip(t, application, "retry:capped", "example/retry")
	runCheckJob(t, application, id)

	for attempt := 1; attempt <= checkRetryMaxAttempts; attempt++ {
		application.clock = func() time.Time { return base.Add(time.Duration(attempt) * 4 * time.Hour) }

		postTick(t, application)

		if status := shipStatus(t, application, id); status != statusChecking {
			t.Fatalf("status on attempt %d = %q, want checking", attempt, status)
		}

		runCheckJob(t, application, id)
	}

	application.clock = func() time.Time { return base.Add(30 * 24 * time.Hour) }

	postTick(t, application)

	if status := shipStatus(t, application, id); status != statusNeedsAttention {
		t.Fatalf("status = %q, want needs_attention", status)
	}

	if count := retryEventCount(t, application, id); count != checkRetryMaxAttempts {
		t.Fatalf("retries = %d, want %d", count, checkRetryMaxAttempts)
	}

	if len(jobs) != 1+checkRetryMaxAttempts {
		t.Fatalf("jobs = %+v", jobs)
	}
}
