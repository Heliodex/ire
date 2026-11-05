package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"review/contract"
)

const previewBoardText = "(kicad_pcb (version 20211014) (generator pcbnew) (general (thickness 1.6)))"

// fakePreviewForge serves one board file and one oversized file for the preview tests.
func fakePreviewForge(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		switch r.URL.Path {
		case "/repos/example/queued/contents/board.kicad_pcb":
			_, _ = io.WriteString(w, previewBoardText)
		case "/repos/example/queued/contents/big.kicad_pcb":
			_, _ = io.WriteString(w, "(kicad_pcb (version 20211014)")
			_, _ = io.WriteString(w, strings.Repeat("x", maxPreviewBytes+1))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

// seedPreviewShip points a fixture ship at the fake forge with a listed board file.
func seedPreviewShip(t *testing.T, application *reviewApp) {
	t.Helper()

	files := `[{"path":"board.kicad_pcb","kind":"file"},{"path":"big.kicad_pcb","kind":"file"},{"path":"README.md","kind":"file"}]`

	if _, err := application.store.Exec(t.Context(),
		`update submission set repo_url = 'https://github.com/example/queued' where id = 'sub_fixture_1'`); err != nil {
		t.Fatal(err)
	}

	if _, err := application.store.Exec(t.Context(),
		`insert or replace into repo_snapshot (submission_id, version, forge, full_name, default_branch, head_sha, file_count, truncated, readme_text, files, commits, fetched_at, error)
		values ('sub_fixture_1', 1, 'github', 'example/queued', 'main', 'head1', 3, 0, '', ?, '[]', ?, null)`,
		files, application.nowText()); err != nil {
		t.Fatal(err)
	}
}

// TestSubmissionFilePreview covers the in-app board preview: auth, the snapshot allowlist and the size cap.
func TestSubmissionFilePreview(t *testing.T) {
	forge := fakePreviewForge(t)

	application := newTestApp(t)
	application.config.GitHubAPIBase = forge.URL
	seedPreviewShip(t, application)

	cookie := login(t, application, "reviewer@example.com")
	preview := func(path string) *httptest.ResponseRecorder {
		return do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1/file?path="+path, nil, cookie, nil)
	}

	if denied := do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1/file?path=board.kicad_pcb", nil, nil, nil); denied.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous preview = %d, want 401", denied.Code)
	}

	if missing := preview("other.kicad_pcb"); missing.Code != http.StatusNotFound {
		t.Fatalf("unlisted preview = %d, want 404", missing.Code)
	}

	if traversal := preview("../board.kicad_pcb"); traversal.Code != http.StatusBadRequest {
		t.Fatalf("traversal preview = %d, want 400", traversal.Code)
	}

	if text := preview("README.md"); text.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text preview = %d, want 415", text.Code)
	}

	if big := preview("big.kicad_pcb"); big.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized preview = %d, want 413", big.Code)
	}

	ok := preview("board.kicad_pcb")
	if ok.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", ok.Code, ok.Body.String())
	}

	file := decode[contract.SubmissionFileResponse](t, ok).File
	if file.Path != "board.kicad_pcb" || file.Text != previewBoardText || file.SizeBytes != len(previewBoardText) || file.Truncated {
		t.Fatalf("preview file = %+v", file)
	}
}

// TestSubmissionFilePreviewGuards covers ships the preview endpoint must refuse.
func TestSubmissionFilePreviewGuards(t *testing.T) {
	forge := fakePreviewForge(t)

	application := newTestApp(t)
	application.config.GitHubAPIBase = forge.URL
	seedPreviewShip(t, application)

	cookie := login(t, application, "reviewer@example.com")

	if _, err := application.store.Exec(t.Context(),
		`update submission set repo_url = 'https://gitlab.com/example/queued' where id = 'sub_fixture_1'`); err != nil {
		t.Fatal(err)
	}

	if other := do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1/file?path=board.kicad_pcb", nil, cookie, nil); other.Code != http.StatusUnprocessableEntity {
		t.Fatalf("non-github preview = %d, want 422", other.Code)
	}

	if _, err := application.store.Exec(t.Context(), `delete from repo_snapshot where submission_id = 'sub_fixture_2'`); err != nil {
		t.Fatal(err)
	}

	if bare := do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_2/file?path=board.kicad_pcb", nil, cookie, nil); bare.Code != http.StatusNotFound {
		t.Fatalf("snapshotless preview = %d, want 404", bare.Code)
	}
}
