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

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/app"
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

	jobs := []app.JobRequest{}
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

	statuses := checkStatuses(bundle.Checks)
	for _, key := range []string{"repo.accessible", "repo.readme", "repo.bom", "repo.design_files", "repo.commits"} {
		if statuses[key] != statusPass {
			t.Errorf("%s = %q, want pass; checks = %+v", key, statuses[key], bundle.Checks)
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
