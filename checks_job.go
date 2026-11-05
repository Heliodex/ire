package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

const (
	checkRunnerVersion  = "review-checks-v1"
	checkRunMaxAttempts = 5
)

// checkJob is the durable job payload for the repository checks.
type checkJob struct {
	SubmissionID string `json:"submission_id"`
	Version      int    `json:"version"`
}

// queueRepositoryChecks enqueues the forge work and falls back to running it inline if the queue is unavailable, so a ship never waits in checking forever.
func (a *reviewApp) queueRepositoryChecks(ctx context.Context, submissionID string, version int, idempotencyKey string) {
	err := a.enqueueCheckJob(ctx, submissionID, version, idempotencyKey)
	if err == nil {
		return
	}

	app.Warnf("checks job enqueue failed, running inline: %v", err)

	ship, loadErr := a.loadSubmission(ctx, submissionID)
	if loadErr != nil || ship.Version != version || ship.Status != statusChecking {
		return
	}

	if runErr := a.runRepositoryChecks(ctx, ship); runErr != nil {
		app.Errorf("inline repository checks: %v", runErr)
	}
}

// enqueueCheckJob adds one idempotent job per submission version.
func (a *reviewApp) enqueueCheckJob(ctx context.Context, submissionID string, version int, idempotencyKey string) error {
	payload, err := json.Marshal(checkJob{SubmissionID: submissionID, Version: version})
	if err != nil {
		return err
	}

	_, err = a.config.Enqueue(ctx, app.JobRequest{
		Path:           "/jobs/checks",
		Method:         http.MethodPost,
		Body:           string(payload),
		Headers:        map[string]string{"Content-Type": "application/json"},
		MaxAttempts:    5,
		IdempotencyKey: idempotencyKey,
	})

	return err
}

// handleJobChecks runs the repository checks the supervisor queued for a checking ship.
func (a *reviewApp) handleJobChecks(w http.ResponseWriter, r *http.Request) {
	var job checkJob
	if !app.ReadJSON(w, r, maxAPIBody, "body", &job) {
		return
	}

	if job.SubmissionID == "" || job.Version < 1 {
		app.WriteError(w, http.StatusUnprocessableEntity, "invalid check job")
		return
	}

	ship, err := a.loadSubmission(r.Context(), job.SubmissionID)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		writeStorage(w, err)

		return
	}

	// A newer version, a decision or a withdrawal owns the ship now; the job has nothing left to write.
	if ship.Version != job.Version || ship.Status != statusChecking || ship.RepoURL == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := a.runRepositoryChecks(r.Context(), ship); err != nil {
		writeStorage(w, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// runRepositoryChecks records one attempt around the repository fetch.
func (a *reviewApp) runRepositoryChecks(ctx context.Context, ship submissionRow) error {
	attempts := a.startCheckRun(ctx, ship)

	if err := a.collectRepositoryChecks(ctx, ship); err != nil {
		a.failCheckRun(ctx, ship, attempts, err)

		return err
	}

	a.finishCheckRun(ctx, ship)

	return nil
}

// startCheckRun marks a new attempt and returns its number.
func (a *reviewApp) startCheckRun(ctx context.Context, ship submissionRow) int {
	now := a.nowText()

	result, err := a.store.Exec(ctx, `update check_run set status = 'running', attempts = attempts + 1,
		started_at = ?, finished_at = null, error = null where submission_id = ? and version = ?`, now, ship.ID, ship.Version)
	if err != nil {
		app.Errorf("start check run: %v", err)

		return 0
	}

	if result.RowsAffected == 0 {
		if _, err := a.store.Exec(ctx, `insert into check_run (submission_id, version, status, attempts, started_at, finished_at, error, runner_version)
			values (?, ?, 'running', 1, ?, null, null, ?)`, ship.ID, ship.Version, now, checkRunnerVersion); err != nil {
			app.Errorf("insert check run: %v", err)

			return 0
		}
	}

	var attempts int
	if err := a.store.QueryRow(ctx, `select attempts from check_run where submission_id = ? and version = ?`, ship.ID, ship.Version).Scan(&attempts); err != nil {
		return 0
	}

	return attempts
}

// finishCheckRun records a successful attempt.
func (a *reviewApp) finishCheckRun(ctx context.Context, ship submissionRow) {
	if _, err := a.store.Exec(ctx, `update check_run set status = 'succeeded', finished_at = ?, error = null
		where submission_id = ? and version = ?`, a.nowText(), ship.ID, ship.Version); err != nil {
		app.Errorf("finish check run: %v", err)
	}
}

// failCheckRun records the error and surfaces a terminal failure without waiting for the stale sweep.
func (a *reviewApp) failCheckRun(ctx context.Context, ship submissionRow, attempts int, runErr error) {
	now := a.nowText()

	if _, err := a.store.Exec(ctx, `update check_run set status = 'failed', finished_at = ?, error = ?
		where submission_id = ? and version = ?`, now, runErr.Error(), ship.ID, ship.Version); err != nil {
		app.Errorf("fail check run: %v", err)
	}

	if attempts < checkRunMaxAttempts {
		return
	}

	result, err := a.store.Exec(ctx, `update submission set status = ?, updated_at = ?
		where id = ? and version = ? and status = 'checking'`, statusNeedsAttention, now, ship.ID, ship.Version)
	if err != nil || result.RowsAffected == 0 {
		return
	}

	_, _ = a.store.Exec(ctx, `insert into submission_event (id, submission_id, kind, data, created_at)
		values (?, ?, 'checks_failed', ?, ?)`, newID("evt"), ship.ID, marshalMap(map[string]any{"error": runErr.Error()}), now)
}

// collectRepositoryChecks fetches the forge, replaces the repository check rows, stores the evidence and moves a checking ship to its real status.
func (a *reviewApp) collectRepositoryChecks(ctx context.Context, ship submissionRow) error {
	remote, evidence := a.repoChecks(ctx, ship.RepoURL, a.commitSince(ctx, ship.ProgramID, ship.PreviousID), a.now(), a.loadBomLines(ctx, ship.ID))
	remote = append(remote, a.hackatimeChecks(ctx, ship)...)
	remote = append(remote, a.demoChecks(ctx, ship)...)

	now := a.nowText()
	statements := make([]storage.TransactionStatement, 0, len(remote)*2)

	for _, check := range remote {
		statements = append(statements,
			storage.TransactionStatement{
				SQL:  `delete from check_result where submission_id = ? and version = ? and check_key = ?`,
				Args: []any{ship.ID, ship.Version, check.Key},
			},
			storage.TransactionStatement{
				SQL: `insert into check_result (id, submission_id, version, check_key, label, group_key, status, severity, summary, detail, data, checked_at)
					values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				Args: []any{newID("chk"), ship.ID, ship.Version, check.Key, check.Label, check.Group, check.Status, check.Severity, check.Summary, stringValue(check.Detail), sourceData(check.Source), check.CheckedAt},
			},
		)
	}

	if _, err := a.store.Transaction(ctx, statements...); err != nil {
		return err
	}

	if evidence != nil {
		if err := a.saveEvidence(ctx, ship.ID, ship.Version, *evidence); err != nil {
			return err
		}
	}

	checks, err := a.loadCheckResults(ctx, ship.ID, ship.Version)
	if err != nil {
		return err
	}

	summary := checkCountsFor(checks)

	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return err
	}

	status := statusReady
	if summary.Overall == checkOverallFail {
		status = statusNeedsAttention
	}

	result, err := a.store.Exec(ctx, `update submission set status = ?, check_summary = ?, updated_at = ?
		where id = ? and version = ? and status = 'checking'`,
		status, string(summaryJSON), now, ship.ID, ship.Version)
	if err != nil {
		return err
	}

	if result.RowsAffected == 0 {
		return nil
	}

	_, err = a.store.Exec(ctx, `insert into submission_event (id, submission_id, kind, data, created_at)
		values (?, ?, 'checks_completed', ?, ?)`, newID("evt"), ship.ID, string(summaryJSON), now)

	return err
}

// loadCheckRun returns the run row for one submission version and whether it exists.
func (a *reviewApp) loadCheckRun(ctx context.Context, submissionID string, version int) (contract.CheckRun, bool, error) {
	var (
		run                 contract.CheckRun
		finishedAt, failure string
	)

	err := a.store.QueryRow(ctx, `select status, attempts, started_at, coalesce(finished_at, ''), coalesce(error, ''), runner_version
		from check_run where submission_id = ? and version = ?`, submissionID, version).
		Scan(&run.Status, &run.Attempts, &run.StartedAt, &finishedAt, &failure, &run.RunnerVersion)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			return contract.CheckRun{}, false, nil
		}

		return contract.CheckRun{}, false, err
	}

	run.FinishedAt = optionalText(finishedAt)
	run.Error = optionalText(failure)

	return run, true, nil
}

// loadCheckResults reads the current rows for one submission version.
func (a *reviewApp) loadCheckResults(ctx context.Context, submissionID string, version int) ([]contract.CheckResult, error) {
	rows, err := a.store.Query(ctx, `select check_key, label, group_key, status, severity, summary, coalesce(detail, ''), coalesce(data, ''), checked_at
		from check_result where submission_id = ? and version = ? order by checked_at asc, id asc`, submissionID, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var checks []contract.CheckResult

	for rows.Next() {
		var (
			check        contract.CheckResult
			detail, data string
		)
		if err := rows.Scan(&check.Key, &check.Label, &check.Group, &check.Status, &check.Severity, &check.Summary, &detail, &data, &check.CheckedAt); err != nil {
			return nil, err
		}

		if detail != "" {
			check.Detail = &detail
		}

		check.Source = sourceFromData(data)

		checks = append(checks, check)
	}

	return checks, rows.Err()
}
