package main

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
)

// TestChecksRerun covers the organiser rerun action and the queue round trip.
func TestChecksRerun(t *testing.T) {
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/readme"):
			_, _ = io.WriteString(w, strings.Repeat("Build notes and wiring details. ", 12)+`\n![board](https://example.com/board.jpg)\n![schematic](https://example.com/schematic.png)`)
		case strings.HasSuffix(r.URL.Path, "/contents/BOM.csv"):
			_, _ = io.WriteString(w, "Part,Qty,Unit Price,Vendor\nXIAO,1,4.99,Seeed\n")
		case strings.HasSuffix(r.URL.Path, "/git/trees/main"):
			_, _ = io.WriteString(w, `{"sha":"head1","tree":[{"path":"README.md","type":"blob"},{"path":"BOM.csv","type":"blob"},{"path":"board.kicad_pcb","type":"blob"}]}`)
		case strings.HasSuffix(r.URL.Path, "/commits"):
			_, _ = io.WriteString(w, `[{"sha":"sha1","commit":{"author":{"name":"Maker","date":"2026-11-01T10:00:00Z"},"message":"First"}}]`)
		default:
			_, _ = io.WriteString(w, `{"default_branch":"main","full_name":"C6-H/Starbie","private":false}`)
		}
	}))
	t.Cleanup(github.Close)

	application := newTestApp(t)
	application.config.DisableRepoChecks = false
	application.config.GitHubAPIBase = github.URL
	application.config.EnableJobs = true

	var jobs []app.JobRequest

	application.config.Enqueue = func(_ context.Context, request app.JobRequest) (bool, error) {
		jobs = append(jobs, request)
		return true, nil
	}

	reviewer := login(t, application, "reviewer@example.com")
	organiser := login(t, application, "organiser@example.com")

	if denied := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/checks/rerun", nil, reviewer, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("reviewer rerun = %d: %s", denied.Code, denied.Body.String())
	}

	recorder := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/checks/rerun", nil, organiser, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("rerun = %d: %s", recorder.Code, recorder.Body.String())
	}

	run := decode[contract.CheckRun](t, recorder)
	if run.Status != "queued" || run.Attempts != 0 {
		t.Fatalf("run = %+v", run)
	}

	if len(jobs) != 1 || !strings.HasPrefix(jobs[0].IdempotencyKey, "checks:sub_fixture_1:1:rerun:") {
		t.Fatalf("jobs = %+v", jobs)
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, organiser, nil))
	if bundle.Submission.Status != statusChecking || bundle.CheckRun == nil || bundle.CheckRun.Status != "queued" {
		t.Fatalf("bundle = %+v run = %+v", bundle.Submission, bundle.CheckRun)
	}

	if again := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/checks/rerun", nil, organiser, nil); again.Code != http.StatusConflict {
		t.Fatalf("second rerun = %d: %s", again.Code, again.Body.String())
	}

	var job checkJob
	if err := json.Unmarshal([]byte(jobs[0].Body), &job); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(job)

	if executed := do(t, application, http.MethodPost, "/jobs/checks", payload, nil, nil); executed.Code != http.StatusNoContent {
		t.Fatalf("job run = %d: %s", executed.Code, executed.Body.String())
	}

	bundle = decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, organiser, nil))
	if bundle.Submission.Status != statusReady || bundle.CheckRun == nil || bundle.CheckRun.Status != "succeeded" {
		t.Fatalf("after rerun = %+v run = %+v", bundle.Submission, bundle.CheckRun)
	}
}
