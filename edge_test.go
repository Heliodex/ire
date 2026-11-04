package main

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIngestRejectsMalformedJSON covers a signed body that is not an ingest request.
func TestIngestRejectsMalformedJSON(t *testing.T) {
	application := newTestApp(t)
	body := []byte(`{"external_id":`)

	recorder := do(t, application, http.MethodPost, "/api/ingest/half-life", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	})

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("malformed ingest = %d: %s", recorder.Code, recorder.Body.String())
	}

	var deliveries int
	if err := application.store.QueryRow(t.Context(), `select count(*) from ingest_delivery`).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}

	if deliveries != 0 {
		t.Errorf("deliveries = %d", deliveries)
	}
}

// TestRepoChecksReportTransportError covers a forge that drops the connection instead of returning a status.
func TestRepoChecksReportTransportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	}))
	t.Cleanup(server.Close)

	application := newTestApp(t)
	application.config.GitHubAPIBase = server.URL

	checks, evidence := application.repoChecks(t.Context(), "https://github.com/example/broken", "", application.now(), nil)
	if evidence != nil {
		t.Errorf("evidence = %+v", evidence)
	}

	if statuses := checkStatuses(checks); statuses["repo.accessible"] != statusError {
		t.Fatalf("checks = %+v", checks)
	}
}

// TestRepoChecksTruncateLargeTree covers the snapshot cap on a repository with more files than the limit.
func TestRepoChecksTruncateLargeTree(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/readme"):
			_, _ = io.WriteString(w, strings.Repeat("Build notes. ", 40))
		case strings.HasSuffix(r.URL.Path, "/git/trees/main"):
			files := make([]map[string]string, 0, maxSnapshotFiles+100)
			for i := range maxSnapshotFiles + 100 {
				files = append(files, map[string]string{"path": fmt.Sprintf("hardware/part-%04d.kicad_pcb", i), "type": "blob"})
			}

			body, _ := json.Marshal(map[string]any{"sha": "head1", "tree": files, "truncated": false})
			_, _ = w.Write(body)
		case strings.HasSuffix(r.URL.Path, "/commits"):
			_, _ = io.WriteString(w, `[]`)
		default:
			_, _ = io.WriteString(w, `{"default_branch":"main","full_name":"example/large","private":false}`)
		}
	}))
	t.Cleanup(server.Close)

	application := newTestApp(t)
	application.config.GitHubAPIBase = server.URL

	_, evidence := application.repoChecks(t.Context(), "https://github.com/example/large", "", application.now(), nil)
	if evidence == nil || !evidence.Truncated {
		t.Fatalf("evidence = %+v", evidence)
	}

	if len(evidence.Files) > maxSnapshotFiles {
		t.Errorf("files = %d", len(evidence.Files))
	}
}
