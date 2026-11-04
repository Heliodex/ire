package main

import (
	"net/http"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
)

// handleChecksHead compares the newest commit on the default branch with the snapshot the checks ran against.
func (a *reviewApp) handleChecksHead(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireReviewer(w, r); !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	owner, name, ok := parseGitHubRepo(ship.RepoURL)
	if !ok {
		app.WriteError(w, http.StatusUnprocessableEntity, "only GitHub repositories can be compared")
		return
	}

	var snapshotSha, forge string

	_ = a.store.QueryRow(r.Context(), `select coalesce(head_sha, ''), coalesce(forge, '') from repo_snapshot where submission_id = ? and version = ?`,
		ship.ID, ship.Version).Scan(&snapshotSha, &forge)

	if forge != "github" {
		// Fixture and other offline snapshots cannot drift; only a live forge head can move.
		app.WriteJSON(w, http.StatusOK, contract.CheckHead{SnapshotSha: optionalText(snapshotSha)})
		return
	}

	client := githubClient{http: a.client, base: a.config.GitHubAPIBase, token: a.config.GitHubToken}
	if client.base == "" {
		client.base = githubDefaultAPI
	}

	headSha, status, err := client.head(r.Context(), owner, name)
	if err != nil || status != http.StatusOK {
		app.WriteError(w, http.StatusBadGateway, "could not read the repository head")
		return
	}

	app.WriteJSON(w, http.StatusOK, contract.CheckHead{
		HeadSha:     optionalText(headSha),
		Outdated:    headSha != "" && snapshotSha != "" && headSha != snapshotSha,
		SnapshotSha: optionalText(snapshotSha),
	})
}
