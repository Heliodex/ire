package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"strings"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// loadEvidence returns the cached repository snapshot for a submission version and whether one was stored.
func (a *reviewApp) loadEvidence(ctx context.Context, submissionID string, version int) (contract.Evidence, bool, error) {
	var (
		evidence                                contract.Evidence
		defaultBranch, headSha, readme, failure string
		filesJSON, commitsJSON                  string
		truncated                               int
	)

	err := a.store.QueryRow(ctx, `select forge, full_name, coalesce(default_branch, ''), coalesce(head_sha, ''),
			truncated, coalesce(readme_text, ''), files, commits, fetched_at, coalesce(error, '')
		from repo_snapshot where submission_id = ? and version = ?`, submissionID, version).
		Scan(&evidence.Forge, &evidence.FullName, &defaultBranch, &headSha, &truncated, &readme,
			&filesJSON, &commitsJSON, &evidence.FetchedAt, &failure)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			return contract.Evidence{}, false, nil
		}

		return contract.Evidence{}, false, err
	}

	evidence.DefaultBranch = optionalText(defaultBranch)
	evidence.HeadSha = optionalText(headSha)
	evidence.Readme = optionalText(readme)
	evidence.Error = optionalText(failure)
	evidence.Truncated = truncated == 1

	if err := json.Unmarshal([]byte(filesJSON), &evidence.Files); err != nil {
		return contract.Evidence{}, false, err
	}

	if err := json.Unmarshal([]byte(commitsJSON), &evidence.Commits); err != nil {
		return contract.Evidence{}, false, err
	}

	return evidence, true, nil
}

// programStart returns the programme start used as the commit window.
func (a *reviewApp) programStart(ctx context.Context, programID string) string {
	program, err := a.programByID(ctx, programID)
	if err != nil {
		return ""
	}

	return program.CreatedAt
}

// saveEvidence replaces the snapshot for a submission version, so a refresh never leaves two copies.
func (a *reviewApp) saveEvidence(ctx context.Context, submissionID string, version int, evidence contract.Evidence) error {
	files, err := json.Marshal(evidence.Files)
	if err != nil {
		return err
	}

	commits, err := json.Marshal(evidence.Commits)
	if err != nil {
		return err
	}

	_, err = a.store.Exec(ctx, `insert into repo_snapshot (submission_id, version, forge, full_name, default_branch,
			head_sha, file_count, truncated, readme_text, files, commits, fetched_at, error)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(submission_id, version) do update set
			forge = excluded.forge,
			full_name = excluded.full_name,
			default_branch = excluded.default_branch,
			head_sha = excluded.head_sha,
			file_count = excluded.file_count,
			truncated = excluded.truncated,
			readme_text = excluded.readme_text,
			files = excluded.files,
			commits = excluded.commits,
			fetched_at = excluded.fetched_at,
			error = excluded.error`,
		submissionID, version, evidence.Forge, evidence.FullName, stringValue(evidence.DefaultBranch),
		stringValue(evidence.HeadSha), len(evidence.Files), boolInt(evidence.Truncated), stringValue(evidence.Readme),
		string(files), string(commits), evidence.FetchedAt, stringValue(evidence.Error))

	return err
}

// handleEvidenceRefresh refetches the repository snapshot for organisers, leaving the decision and checks untouched.
func (a *reviewApp) handleEvidenceRefresh(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	if !canReviewSecondPass(person.Role) {
		app.WriteError(w, http.StatusForbidden, "only organisers can refresh evidence")
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	if strings.TrimSpace(ship.RepoURL) == "" {
		app.WriteError(w, http.StatusUnprocessableEntity, "this ship has no repository URL")
		return
	}

	_, evidence := a.repoChecks(r.Context(), ship.RepoURL, a.programStart(r.Context(), ship.ProgramID), a.now())
	if evidence == nil {
		app.WriteError(w, http.StatusUnprocessableEntity, "only public GitHub repositories can be refreshed")
		return
	}

	if err := a.saveEvidence(r.Context(), ship.ID, ship.Version, *evidence); err != nil {
		writeStorage(w, err)
		return
	}

	app.WriteJSON(w, http.StatusOK, *evidence)
}
