package main

import (
	"fmt"
	"net/http"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// handleChecksRerun queues a fresh repository check for an organiser without changing the review.
func (a *ireApp) handleChecksRerun(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	if !canReviewSecondPass(person.Role) {
		app.WriteError(w, http.StatusForbidden, "only organisers can rerun checks")
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	switch ship.Status {
	case statusChecking:
		app.WriteError(w, http.StatusConflict, "checks are already running")
		return
	case statusDecided, statusWithdrawn:
		app.WriteError(w, http.StatusConflict, "this ship is closed")
		return
	}

	if ship.RepoURL == "" {
		app.WriteError(w, http.StatusUnprocessableEntity, "this ship has no repository URL")
		return
	}

	now := a.nowText()

	if _, err := a.store.Transaction(r.Context(),
		storage.TransactionStatement{
			SQL:  `update submission set status = 'checking', updated_at = ? where id = ?`,
			Args: []any{now, ship.ID},
		},
		storage.TransactionStatement{
			SQL: `insert or replace into check_run (submission_id, version, status, attempts, started_at, finished_at, error, runner_version)
				values (?, ?, 'queued', 0, ?, null, null, ?)`,
			Args: []any{ship.ID, ship.Version, now, checkRunnerVersion},
		},
		storage.TransactionStatement{
			SQL: `insert into submission_event (id, submission_id, kind, actor_id, actor_email, data, created_at)
				values (?, ?, 'checks_rerun', ?, ?, ?, ?)`,
			Args: []any{newID("evt"), ship.ID, person.ID, person.Email, marshalMap(map[string]any{"version": ship.Version}), now},
		},
	); err != nil {
		writeStorage(w, err)
		return
	}

	a.forgeCache.clear()
	a.queueRepositoryChecks(r.Context(), ship.ID, ship.Version, fmt.Sprintf("checks:%s:%d:rerun:%s", ship.ID, ship.Version, newID("rerun")))

	run, found, err := a.loadCheckRun(r.Context(), ship.ID, ship.Version)
	if err != nil {
		writeStorage(w, err)
		return
	}

	if !found {
		app.WriteError(w, http.StatusInternalServerError, "checks were queued but the run could not be read back")
		return
	}

	app.WriteJSON(w, http.StatusOK, run)
}
