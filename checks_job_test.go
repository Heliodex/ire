package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
)

// fakeGitHub serves one healthy repository for the check job tests.
func fakeGitHub(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/repos/example/queued":
			_, _ = io.WriteString(w, `{"default_branch":"main","full_name":"example/queued","private":false}`)
		case "/repos/example/queued/readme":
			_, _ = io.WriteString(w, strings.Repeat("Build notes and wiring details. ", 12)+`\n![board](https://example.com/board.jpg)\n![schematic](https://example.com/schematic.png)`)
		case "/repos/example/queued/contents/BOM.csv":
			_, _ = io.WriteString(w, "Part,Qty,Unit Price,Vendor\nXIAO,1,4.99,Seeed\n")
		case "/repos/example/queued/git/trees/main":
			_, _ = io.WriteString(w, `{"sha":"head1","tree":[{"path":"README.md","type":"blob"},{"path":"BOM.csv","type":"blob"},{"path":"board.kicad_pcb","type":"blob"}]}`)
		case "/repos/example/queued/commits":
			_, _ = io.WriteString(w, `[{"sha":"sha1","commit":{"author":{"name":"Maker","date":"2026-11-01T10:00:00Z"},"message":"First"}}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

// TestIngestQueuesRepositoryChecks covers the checking status, the idempotent job and the worker that finishes it.
func TestIngestQueuesRepositoryChecks(t *testing.T) {
	github := fakeGitHub(t)

	application := newTestApp(t)
	application.config.DisableRepoChecks = false
	application.config.GitHubAPIBase = github.URL
	application.config.EnableJobs = true

	var jobs []app.JobRequest

	application.config.Enqueue = func(_ context.Context, request app.JobRequest) (bool, error) {
		jobs = append(jobs, request)
		return true, nil
	}

	body := ingestBody("queued:design")
	body = []byte(strings.Replace(string(body), "example/line-follower", "example/queued", 1))

	created := decode[contract.IngestAccepted](t, do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	}))

	cookie := login(t, application, "reviewer@example.com")
	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/"+created.ID, nil, cookie, nil))

	if bundle.Submission.Status != statusChecking {
		t.Fatalf("status = %q, want checking", bundle.Submission.Status)
	}

	if bundle.CheckRun == nil || bundle.CheckRun.Status != "queued" || bundle.CheckRun.Attempts != 0 {
		t.Fatalf("check run = %+v", bundle.CheckRun)
	}

	queue := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all", nil, cookie, nil))

	if queue.Counts.Checking != 1 {
		t.Errorf("checking count = %d, want 1", queue.Counts.Checking)
	}

	var found bool

	for _, item := range queue.Items {
		if item.ID == created.ID {
			found = item.Status == statusChecking && item.CheckRun != nil && item.CheckRun.Status == "queued"
		}
	}

	if !found {
		t.Errorf("checking ship is missing from the queue: %+v", queue.Items)
	}

	if bundle.Evidence != nil {
		t.Fatalf("evidence before the job = %+v", bundle.Evidence)
	}

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/"+created.ID+"/claim", nil, cookie, nil); claim.Code != http.StatusConflict {
		t.Fatalf("claim while checking = %d: %s", claim.Code, claim.Body.String())
	}

	if len(jobs) != 1 || jobs[0].Path != "/jobs/checks" || jobs[0].IdempotencyKey != "checks:"+created.ID+":1" {
		t.Fatalf("jobs = %+v", jobs)
	}

	var job checkJob
	if err := json.Unmarshal([]byte(jobs[0].Body), &job); err != nil {
		t.Fatal(err)
	}

	if job.SubmissionID != created.ID || job.Version != 1 {
		t.Fatalf("job body = %+v", job)
	}

	payload, _ := json.Marshal(job)

	if run := do(t, application, http.MethodPost, "/jobs/checks", payload, nil, nil); run.Code != http.StatusNoContent {
		t.Fatalf("job run = %d: %s", run.Code, run.Body.String())
	}

	bundle = decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/"+created.ID, nil, cookie, nil))

	if bundle.Submission.Status != statusReady {
		t.Fatalf("status after the job = %q", bundle.Submission.Status)
	}

	if bundle.Evidence == nil || bundle.Evidence.FullName != "example/queued" {
		t.Fatalf("evidence after the job = %+v", bundle.Evidence)
	}

	if bundle.CheckRun == nil || bundle.CheckRun.Status != "succeeded" || bundle.CheckRun.Attempts != 1 || bundle.CheckRun.FinishedAt == nil {
		t.Fatalf("check run after the job = %+v", bundle.CheckRun)
	}

	status := decode[contract.StatusResponse](t, do(t, application, http.MethodGet, "/api/ingest/half-life/status?external_id=queued:design", nil, nil, map[string]string{
		"Authorization": "Bearer test-ingest",
	}))

	if status.CheckRun == nil || status.CheckRun.Status != "succeeded" {
		t.Fatalf("status check run = %+v", status.CheckRun)
	}

	statuses := checkStatuses(bundle.Checks)
	for _, key := range []string{"repo.accessible", "repo.readme", "repo.bom", "repo.design_files", "repo.commits"} {
		if statuses[key] != statusPass {
			t.Errorf("%s = %q, want pass; checks = %+v", key, statuses[key], bundle.Checks)
		}
	}

	for _, check := range bundle.Checks {
		if check.Key == "repo.accessible" && (check.Source == nil || *check.Source != "forge") {
			t.Errorf("repo source = %v", check.Source)
		}

		if check.Key == "evidence.hours" && (check.Source == nil || *check.Source != "payload") {
			t.Errorf("payload source = %v", check.Source)
		}
	}

	// A retried job for a ship that has already moved on must not write twice.
	bundleChecks := len(bundle.Checks)

	if rerun := do(t, application, http.MethodPost, "/jobs/checks", payload, nil, nil); rerun.Code != http.StatusNoContent {
		t.Fatalf("second job run = %d: %s", rerun.Code, rerun.Body.String())
	}

	again := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/"+created.ID, nil, cookie, nil))
	if len(again.Checks) != bundleChecks {
		t.Errorf("checks after rerun = %d, want %d", len(again.Checks), bundleChecks)
	}
}

// TestIngestFallsBackWhenQueueFails covers the synchronous fallback so a ship never sticks in checking.
func TestIngestFallsBackWhenQueueFails(t *testing.T) {
	github := fakeGitHub(t)

	application := newTestApp(t)
	application.config.DisableRepoChecks = false
	application.config.GitHubAPIBase = github.URL
	application.config.EnableJobs = true
	application.config.Enqueue = func(context.Context, app.JobRequest) (bool, error) {
		return false, errors.New("queue unavailable")
	}

	body := ingestBody("queued:design")
	body = []byte(strings.Replace(string(body), "example/line-follower", "example/queued", 1))

	created := decode[contract.IngestAccepted](t, do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	}))

	cookie := login(t, application, "reviewer@example.com")
	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/"+created.ID, nil, cookie, nil))

	if bundle.Submission.Status != statusReady {
		t.Fatalf("status = %q, want ready", bundle.Submission.Status)
	}

	if bundle.Evidence == nil {
		t.Fatal("fallback did not store evidence")
	}
}

// queuedCheckSummary reads one ship's check counts from the queue.
func queuedCheckSummary(t *testing.T, application *ireApp, submissionID string) contract.CheckSummary {
	t.Helper()

	cookie := login(t, application, "reviewer@example.com")
	queue := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all", nil, cookie, nil))

	for _, item := range queue.Items {
		if item.ID == submissionID {
			return item.CheckSummary
		}
	}

	t.Fatalf("ship %s is missing from the queue", submissionID)

	return contract.CheckSummary{}
}

// TestChecksLandAfterStaleSweep covers a job that arrives after the sweep moved the ship to needs_attention: the checks and the queue summary must still be written, and a clean run must return the ship to ready.
func TestChecksLandAfterStaleSweep(t *testing.T) {
	github := fakeGitHub(t)

	application := newTestApp(t)
	application.config.DisableRepoChecks = false
	application.config.GitHubAPIBase = github.URL
	application.config.EnableJobs = true
	application.config.Enqueue = func(context.Context, app.JobRequest) (bool, error) { return true, nil }

	body := ingestBody("queued:design")
	body = []byte(strings.Replace(string(body), "example/line-follower", "example/queued", 1))

	created := decode[contract.IngestAccepted](t, do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	}))

	application.clock = func() time.Time { return time.Date(2026, 11, 3, 12, 11, 0, 0, time.UTC) }
	postTick(t, application)

	if status := shipStatus(t, application, created.ID); status != statusNeedsAttention {
		t.Fatalf("status after the stale sweep = %q, want needs_attention", status)
	}

	runCheckJob(t, application, created.ID)

	if status := shipStatus(t, application, created.ID); status != statusReady {
		t.Fatalf("status after the late job = %q, want ready", status)
	}

	summary := queuedCheckSummary(t, application, created.ID)
	if summary.Error != 0 || summary.Fail != 0 {
		t.Fatalf("late job summary = %+v, want no errors or failures", summary)
	}

	if summary.Pass == 0 {
		t.Fatalf("late job summary = %+v, want passing checks", summary)
	}
}

// TestChecksSettleAfterSweepWithoutClobberingClaim covers a late job landing while another reviewer has already claimed the swept ship: the counts still refresh but the claim survives.
func TestChecksSettleAfterSweepWithoutClobberingClaim(t *testing.T) {
	github := fakeGitHub(t)

	application := newTestApp(t)
	application.config.DisableRepoChecks = false
	application.config.GitHubAPIBase = github.URL
	application.config.EnableJobs = true
	application.config.Enqueue = func(context.Context, app.JobRequest) (bool, error) { return true, nil }

	body := ingestBody("queued:design")
	body = []byte(strings.Replace(string(body), "example/line-follower", "example/queued", 1))

	created := decode[contract.IngestAccepted](t, do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	}))

	application.clock = func() time.Time { return time.Date(2026, 11, 3, 12, 11, 0, 0, time.UTC) }
	postTick(t, application)

	cookie := login(t, application, "reviewer@example.com")
	if claim := do(t, application, http.MethodPost, testBase+"/submissions/"+created.ID+"/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	runCheckJob(t, application, created.ID)

	if status := shipStatus(t, application, created.ID); status != statusReady {
		t.Fatalf("status after the late job = %q, want ready", status)
	}

	var claims int
	if err := application.store.QueryRow(context.Background(), `select count(*) from claim where submission_id = ?`, created.ID).Scan(&claims); err != nil {
		t.Fatal(err)
	}

	if claims != 1 {
		t.Fatalf("claims after the late job = %d, want the reviewer's claim kept", claims)
	}

	if summary := queuedCheckSummary(t, application, created.ID); summary.Error != 0 || summary.Pass == 0 {
		t.Fatalf("late job summary = %+v, want the refreshed checks", summary)
	}
}

// TestCheckSummaryIgnoresSupersededRows covers the queue counts following the newest row per key, so an older error cannot outlive the pass that replaced it.
func TestCheckSummaryIgnoresSupersededRows(t *testing.T) {
	github := fakeGitHub(t)

	application := newTestApp(t)
	application.config.DisableRepoChecks = false
	application.config.GitHubAPIBase = github.URL
	application.config.EnableJobs = true
	application.config.Enqueue = func(context.Context, app.JobRequest) (bool, error) { return true, nil }

	body := ingestBody("queued:design")
	body = []byte(strings.Replace(string(body), "example/line-follower", "example/queued", 1))

	created := decode[contract.IngestAccepted](t, do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	}))

	if _, err := application.store.Exec(context.Background(), `insert into check_result (id, submission_id, version, check_key, label, group_key, status, severity, summary, detail, data, checked_at)
		values (?, ?, 1, 'repo.description', 'Project description', 'repo', 'error', 'warn', 'Stale failure', '', '', '2026-11-03T11:00:00Z')`,
		newID("chk"), created.ID); err != nil {
		t.Fatal(err)
	}

	runCheckJob(t, application, created.ID)

	summary := queuedCheckSummary(t, application, created.ID)
	if summary.Error != 0 {
		t.Fatalf("summary = %+v, want the superseded error ignored", summary)
	}
}

// TestTickKeepsFreshCheckRun covers the stale sweep leaving a just-queued retry alone, so its job can still land.
func TestTickKeepsFreshCheckRun(t *testing.T) {
	fake := newFlakyGitHub(t)
	fake.failMeta.Store(true)

	var jobs []app.JobRequest

	application := retryTestApp(t, fake, &jobs)
	id := ingestRetryShip(t, application, "retry:fresh", "example/retry")
	runCheckJob(t, application, id)

	if status := shipStatus(t, application, id); status != statusNeedsAttention {
		t.Fatalf("status after the failed run = %q, want needs_attention", status)
	}

	application.clock = func() time.Time { return time.Date(2026, 11, 3, 12, 6, 0, 0, time.UTC) }
	postTick(t, application)

	if status := shipStatus(t, application, id); status != statusChecking {
		t.Fatalf("status after the retry = %q, want checking", status)
	}

	application.clock = func() time.Time { return time.Date(2026, 11, 3, 12, 7, 0, 0, time.UTC) }
	postTick(t, application)

	if status := shipStatus(t, application, id); status != statusChecking {
		t.Fatalf("status after a fresh retry = %q, want checking", status)
	}
}

// TestQueueCountsFollowChecks covers a stored summary that lagged behind the check rows: the queue must show the counts its own dots draw.
func TestQueueCountsFollowChecks(t *testing.T) {
	github := fakeGitHub(t)

	application := newTestApp(t)
	application.config.DisableRepoChecks = false
	application.config.GitHubAPIBase = github.URL
	application.config.EnableJobs = true
	application.config.Enqueue = func(context.Context, app.JobRequest) (bool, error) { return true, nil }

	body := ingestBody("queued:design")
	body = []byte(strings.Replace(string(body), "example/line-follower", "example/queued", 1))

	created := decode[contract.IngestAccepted](t, do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	}))

	if _, err := application.store.Exec(context.Background(), `update submission set check_summary = ? where id = ?`,
		`{"overall":"warn","pass":10,"warn":3,"fail":0,"error":1,"running":0}`, created.ID); err != nil {
		t.Fatal(err)
	}

	cookie := login(t, application, "reviewer@example.com")
	queue := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all", nil, cookie, nil))

	for _, item := range queue.Items {
		if item.ID != created.ID {
			continue
		}

		var pass, warn, fail, errored int

		for _, check := range item.Checks {
			switch check.Status {
			case statusPass:
				pass++
			case statusWarn:
				warn++
			case statusFail:
				fail++
			case statusError:
				errored++
			}
		}

		if item.CheckSummary.Error != errored || item.CheckSummary.Fail != fail || item.CheckSummary.Warn != warn || item.CheckSummary.Pass != pass {
			t.Fatalf("summary = %+v, dots count pass=%d warn=%d fail=%d error=%d", item.CheckSummary, pass, warn, fail, errored)
		}

		if item.CheckSummary.Error != 0 {
			t.Fatalf("summary = %+v, want the stale error ignored", item.CheckSummary)
		}

		return
	}

	t.Fatalf("ship %s is missing from the queue", created.ID)
}
