package main

import (
	"context"
	"time"
)

const (
	loginFlowRetention    = 24 * time.Hour
	ingestRetention       = 90 * 24 * time.Hour
	snapshotRetention     = 180 * 24 * time.Hour
	auditRetention        = 400 * 24 * time.Hour
	fakeDeliveryRetention = 30 * 24 * time.Hour
)

// retentionResult counts what one prune pass removed.
type retentionResult struct {
	LoginFlows, Deliveries, Snapshots, AuditRecords, FakeReceipts int
}

// pruneRetention clears short-lived state and the operational history no longer needed after a decision.
func (a *reviewApp) pruneRetention(ctx context.Context) (retentionResult, error) {
	now := a.now()
	result := retentionResult{}

	statements := []struct {
		SQL  string
		Args []any
		Dest *int
	}{
		{`delete from login_flow where created_at < ?`, []any{formatPast(now, loginFlowRetention)}, &result.LoginFlows},
		{`delete from ingest_delivery where received_at < ?`, []any{formatPast(now, ingestRetention)}, &result.Deliveries},
		{
			`delete from repo_snapshot where fetched_at < ? and submission_id in (select id from submission where status in ('decided','withdrawn'))`,
			[]any{formatPast(now, snapshotRetention)},
			&result.Snapshots,
		},
		{`delete from audit_log where created_at < ?`, []any{formatPast(now, auditRetention)}, &result.AuditRecords},
		{`delete from fake_receiver_delivery where received_at < ?`, []any{formatPast(now, fakeDeliveryRetention)}, &result.FakeReceipts},
	}

	for _, statement := range statements {
		outcome, err := a.store.Exec(ctx, statement.SQL, statement.Args...)
		if err != nil {
			return result, err
		}

		*statement.Dest = int(outcome.RowsAffected)
	}

	return result, nil
}

// formatPast is the storage timestamp for a point before now.
func formatPast(now time.Time, age time.Duration) string {
	return now.Add(-age).Format(timestampFormat)
}
