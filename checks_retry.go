package main

import (
	"context"
	"fmt"
	"time"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

const (
	// checkRetryMaxAttempts caps automatic retries per submission version and manual rerun, so a permanently broken forge stops paging the timeline.
	checkRetryMaxAttempts = 5
	// checkRetryBaseDelay is the wait after the first errored check before retrying; it doubles with every attempt.
	checkRetryBaseDelay = 5 * time.Minute
)

// retryableCheckKeys are the GitHub checks worth rerunning: an error here means the forge flaked (rate limit, timeout, outage), while a failure means the repository itself needs work.
var retryableCheckKeys = []string{"repo.accessible", "repo.tree", "repo.commits", "repo.readme"}

// erroredCheck is one open ship with error-status GitHub checks on its current version.
type erroredCheck struct {
	submissionID, status, repoURL, errorAt string
	version                                int
}

// retryErroredChecks requeues ships whose GitHub checks errored, with exponential backoff and a bounded number of attempts. It runs on the schedule tick; deterministic failures (fail, not error) and closed ships are never touched.
func (a *reviewApp) retryErroredChecks(ctx context.Context) {
	for _, candidate := range a.erroredChecks(ctx) {
		a.maybeRetryChecks(ctx, candidate)
	}
}

// erroredChecks lists open ships with error-status GitHub checks on their current version.
func (a *reviewApp) erroredChecks(ctx context.Context) []erroredCheck {
	rows, err := a.store.Query(ctx, `select s.id, s.version, s.status, s.repo_url, max(c.checked_at)
		from submission s
		join check_result c on c.submission_id = s.id and c.version = s.version
		where s.status in ('needs_attention','ready')
		  and coalesce(s.repo_url, '') <> ''
		  and c.status = 'error'
		  and c.check_key in ('repo.accessible','repo.tree','repo.commits','repo.readme')
		group by s.id, s.version, s.status, s.repo_url`)
	if err != nil {
		app.Errorf("checks retry candidates: %v", err)

		return nil
	}
	defer rows.Close()

	var out []erroredCheck

	for rows.Next() {
		var candidate erroredCheck
		if err := rows.Scan(&candidate.submissionID, &candidate.version, &candidate.status, &candidate.repoURL, &candidate.errorAt); err != nil {
			continue
		}

		out = append(out, candidate)
	}

	return out
}

// maybeRetryChecks runs one candidate when its backoff has elapsed and its budget remains.
func (a *reviewApp) maybeRetryChecks(ctx context.Context, candidate erroredCheck) {
	// A ready ship under an active claim is being reviewed; leave it alone. A stuck needs_attention ship is retried even when claimed, because its reviewer cannot rerun checks themselves.
	if candidate.status == statusReady {
		var claimed string
		if err := a.store.QueryRow(ctx, `select submission_id from claim where submission_id = ?`, candidate.submissionID).Scan(&claimed); err == nil {
			return
		}
	}

	epoch, ok := a.retryEpoch(ctx, candidate.submissionID, candidate.version)
	if !ok {
		return
	}

	count := a.retryCount(ctx, candidate.submissionID, epoch)
	if count >= checkRetryMaxAttempts {
		return
	}

	errored, err := time.Parse(timestampFormat, candidate.errorAt)
	if err != nil || a.now().Before(errored.Add(checkRetryBaseDelay<<count)) {
		return
	}

	keys := a.errorCheckKeys(ctx, candidate)
	now := a.nowText()

	result, err := a.store.Exec(ctx, `update submission set status = 'checking', updated_at = ?
		where id = ? and version = ? and status in ('needs_attention','ready')`, now, candidate.submissionID, candidate.version)
	if err != nil || result.RowsAffected == 0 {
		return
	}

	if _, err := a.store.Transaction(ctx,
		storage.TransactionStatement{
			SQL:  `insert or replace into check_run (submission_id, version, status, attempts, started_at, finished_at, error, runner_version) values (?, ?, 'queued', 0, ?, null, null, ?)`,
			Args: []any{candidate.submissionID, candidate.version, now, checkRunnerVersion},
		},
		storage.TransactionStatement{
			SQL:  `insert into submission_event (id, submission_id, kind, data, created_at) values (?, ?, 'checks_retry', ?, ?)`,
			Args: []any{newID("evt"), candidate.submissionID, marshalMap(map[string]any{"version": candidate.version, "retry": count + 1, "keys": keys}), now},
		},
	); err != nil {
		app.Errorf("checks retry record: %v", err)

		return
	}

	a.queueRepositoryChecks(ctx, candidate.submissionID, candidate.version, fmt.Sprintf("checks:%s:%d:retry:%d", candidate.submissionID, candidate.version, count+1))
}

// retryEpoch returns the point a ship's automatic retry budget restarts from: a new version or a manual rerun.
func (a *reviewApp) retryEpoch(ctx context.Context, submissionID string, version int) (string, bool) {
	var received string
	if err := a.store.QueryRow(ctx, `select received_at from submission_version where submission_id = ? and version = ?`, submissionID, version).Scan(&received); err != nil || received == "" {
		return "", false
	}

	var rerun string
	if err := a.store.QueryRow(ctx, `select coalesce(max(created_at), '') from submission_event
		where submission_id = ? and kind = 'checks_rerun' and actor_id is not null`, submissionID).Scan(&rerun); err != nil {
		return "", false
	}

	return max(received, rerun), true
}

// retryCount counts automatic retries since the budget restarted. A read failure closes the gate rather than retrying blind.
func (a *reviewApp) retryCount(ctx context.Context, submissionID, epoch string) int {
	var count int
	if err := a.store.QueryRow(ctx, `select count(*) from submission_event
		where submission_id = ? and kind = 'checks_retry' and created_at > ?`, submissionID, epoch).Scan(&count); err != nil {
		return checkRetryMaxAttempts
	}

	return count
}

// errorCheckKeys lists the errored GitHub checks a retry is chasing, for the timeline event.
func (a *reviewApp) errorCheckKeys(ctx context.Context, candidate erroredCheck) []string {
	rows, err := a.store.Query(ctx, `select distinct check_key from check_result
		where submission_id = ? and version = ? and status = 'error'
		  and check_key in ('repo.accessible','repo.tree','repo.commits','repo.readme')
		order by check_key`, candidate.submissionID, candidate.version)
	if err != nil {
		return []string{}
	}
	defer rows.Close()

	keys := []string{}

	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err == nil {
			keys = append(keys, key)
		}
	}

	return keys
}
