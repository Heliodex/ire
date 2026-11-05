package main

import (
	"net/http"
	"path"
	"strings"
	"unicode/utf8"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
)

// maxPreviewBytes caps one previewed file so a board file cannot exhaust the tab or the runner.
const maxPreviewBytes = 2 << 20

// previewableKicadSuffixes lists the file kinds the review screen previews in this slice: self-contained KiCad boards, schematics and projects, all rendered by the vendored KiCanvas bundle.
var previewableKicadSuffixes = []string{".kicad_pcb", ".kicad_sch", ".kicad_pro"}

// isPreviewableKicad reports whether the review screen can render a file in-app. The frontend mirrors the same suffix set.
func isPreviewableKicad(path string) bool {
	lowered := strings.ToLower(path)
	for _, suffix := range previewableKicadSuffixes {
		if strings.HasSuffix(lowered, suffix) {
			return true
		}
	}

	return false
}

// handleSubmissionFile returns the capped text of one snapshot-listed board file for in-app preview. The path must appear in the ship's evidence snapshot, so reviewers cannot walk the repository or read unrelated files.
func (a *ireApp) handleSubmissionFile(w http.ResponseWriter, r *http.Request) {
	_, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	requested := strings.TrimSpace(r.URL.Query().Get("path"))
	if requested == "" || path.Clean("/"+requested) != "/"+requested {
		app.WriteError(w, http.StatusBadRequest, "a snapshot file path is required")
		return
	}

	trimmed := strings.TrimPrefix(path.Clean("/"+requested), "/")

	if !isPreviewableKicad(trimmed) {
		app.WriteError(w, http.StatusUnsupportedMediaType, "only KiCad files can be previewed")
		return
	}

	evidence, found, err := a.loadEvidence(r.Context(), ship.ID, ship.Version)
	if err != nil {
		writeStorage(w, err)

		return
	}

	if !found {
		app.WriteError(w, http.StatusNotFound, "this ship has no repository snapshot")
		return
	}

	listed := false

	for _, file := range evidence.Files {
		if file.Path == trimmed {
			listed = true

			break
		}
	}

	if !listed {
		app.WriteError(w, http.StatusNotFound, "that file is not in the snapshot")
		return
	}

	if strings.TrimSpace(ship.RepoURL) == "" {
		app.WriteError(w, http.StatusUnprocessableEntity, "this ship has no repository URL")
		return
	}

	owner, name, ok := parseGitHubRepo(ship.RepoURL)
	if !ok {
		app.WriteError(w, http.StatusUnprocessableEntity, "only GitHub repositories can be previewed")
		return
	}

	body, status, err := a.githubClient().file(r.Context(), owner, name, trimmed)
	if err != nil {
		writeStorage(w, err)

		return
	}

	if status == http.StatusNotFound {
		app.WriteError(w, http.StatusNotFound, "that file is not in the repository")

		return
	}

	if status != http.StatusOK {
		app.WriteError(w, http.StatusBadGateway, "GitHub did not return that file")

		return
	}

	if len(body) > maxPreviewBytes {
		app.WriteError(w, http.StatusRequestEntityTooLarge, "that file is too large to preview")
		return
	}

	if !utf8.Valid(body) {
		app.WriteError(w, http.StatusUnsupportedMediaType, "only text board files can be previewed")
		return
	}

	app.WriteJSON(w, http.StatusOK, contract.SubmissionFileResponse{
		File: contract.SubmissionFile{
			Path:      trimmed,
			SizeBytes: len(body),
			Text:      string(body),
			Truncated: false,
		},
	})
}
