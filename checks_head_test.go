package main

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"ire/contract"
)

// TestChecksHead covers the stale snapshot notice when the default branch moves.
func TestChecksHead(t *testing.T) {
	head := "sha1"

	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/example/head/commits" {
			http.NotFound(w, r)
			return
		}

		commits := []map[string]any{{"sha": head, "commit": map[string]any{
			"author":  map[string]string{"name": "Maker", "date": "2026-11-01T10:00:00Z"},
			"message": "First",
		}}}

		body, _ := json.Marshal(commits)
		_, _ = w.Write(body)
	}))
	t.Cleanup(github.Close)

	application := newTestApp(t)
	application.config.GitHubAPIBase = github.URL

	if _, err := application.store.Exec(t.Context(), `insert or replace into repo_snapshot (submission_id, version, forge, full_name, default_branch, head_sha, file_count, truncated, readme_text, files, commits, fetched_at, error)
		values ('sub_fixture_1', 1, 'github', 'example/head', 'main', 'sha1', 1, 0, '# Head', '[]', '[]', '2026-11-01T10:00:00.000000000Z', null)
		on conflict(submission_id, version) do update set head_sha = excluded.head_sha, full_name = excluded.full_name, forge = excluded.forge`); err != nil {
		t.Fatal(err)
	}

	if _, err := application.store.Exec(t.Context(), `update submission set repo_url = 'https://github.com/example/head' where id = 'sub_fixture_1'`); err != nil {
		t.Fatal(err)
	}

	reviewer := login(t, application, "reviewer@example.com")

	clean := decode[contract.CheckHead](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1/checks/head", nil, reviewer, nil))
	if clean.Outdated || clean.HeadSha == nil || *clean.HeadSha != "sha1" {
		t.Fatalf("clean head = %+v", clean)
	}

	head = "sha2"

	moved := decode[contract.CheckHead](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1/checks/head", nil, reviewer, nil))
	if !moved.Outdated || moved.SnapshotSha == nil || *moved.SnapshotSha != "sha1" {
		t.Fatalf("moved head = %+v", moved)
	}
}
