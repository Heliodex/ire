package main

import (
	"net/http"
	"path"
	"strings"
	"unicode/utf8"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
)

// maxPreviewBytes caps one previewed file so a board file cannot exhaust the tab or the runner.
const maxPreviewBytes = 2 << 20

// previewableBoardSuffix is the only file kind the review screen previews in this slice: a self-contained KiCad board.
const previewableBoardSuffix = ".kicad_pcb"

// handleSubmissionFile returns the capped text of one snapshot-listed board file for in-app preview. The path must appear in the ship's evidence snapshot, so reviewers cannot walk the repository or read unrelated files.
func (a *reviewApp) handleSubmissionFile(w http.ResponseWriter, r *http.Request) {
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

	if !strings.HasSuffix(strings.ToLower(trimmed), previewableBoardSuffix) {
		app.WriteError(w, http.StatusUnsupportedMediaType, "only KiCad board files can be previewed")
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
