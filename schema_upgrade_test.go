package main

import (
	"context"
	"strings"
	"testing"

	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// TestLateColumnsUpgrade simulates a database created before journal adjudication added its columns. The create statement is skipped for an existing table, so migrate has to add them after the versioned migration, which stays a no-op.
func TestLateColumnsUpgrade(t *testing.T) {
	application := newTestApp(t)
	ctx := context.Background()

	for _, column := range []string{"approved_seconds", "reviewer_note"} {
		if _, err := application.store.Exec(ctx, `alter table journal_entry drop column `+column); err != nil {
			t.Skipf("storage cannot drop a column: %v", err)
		}
	}

	if _, err := application.store.Exec(ctx, `alter table submission drop column submitted_at`); err != nil {
		t.Skipf("storage cannot drop a column: %v", err)
	}

	if err := application.migrate(ctx); err != nil {
		t.Fatal(err)
	}

	for _, late := range lateColumns {
		has, err := application.store.HasColumn(ctx, late.table, late.column)
		if err != nil {
			t.Fatal(err)
		}

		if !has {
			t.Errorf("%s.%s is missing after migrate", late.table, late.column)
		}
	}
}

// TestMigrateIsStable re-runs migrate on a current database. The versioned statement set must not change, or the storage service would reject it as an edited migration.
func TestMigrateIsStable(t *testing.T) {
	application := newTestApp(t)

	for range 2 {
		if err := application.migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

// TestStationAnswerReviewerRebuild simulates a database from before answers were kept per reviewer and checks that migrate swaps in the per-reviewer key and keeps the rows.
func TestStationAnswerReviewerRebuild(t *testing.T) {
	application := newTestApp(t)
	ctx := context.Background()

	if _, err := application.store.Transaction(ctx, oldStationAnswerTable()...); err != nil {
		t.Fatal(err)
	}

	if err := application.migrate(ctx); err != nil {
		t.Fatal(err)
	}

	definition, ok, err := application.store.TableSQL(ctx, "station_answer")
	if err != nil || !ok || !strings.Contains(definition, stationAnswerReviewerKey) {
		t.Fatalf("station_answer = %q %v %v, want the per-reviewer key", definition, ok, err)
	}

	var reviewer string
	if err := application.store.QueryRow(ctx, `select reviewer_id from station_answer where id = 'old-answer'`).Scan(&reviewer); err != nil || reviewer != "legacy" {
		t.Errorf("copied reviewer = %q, %v; want legacy", reviewer, err)
	}

	columns, err := application.store.Columns(ctx, "station_answer")
	if err != nil || len(columns) != 10 {
		t.Errorf("columns = %v, %v", columns, err)
	}
}

// oldStationAnswerTable replaces station_answer with its shape from before per-reviewer answers, holding one answer without a reviewer.
func oldStationAnswerTable() []storage.TransactionStatement {
	return []storage.TransactionStatement{
		{SQL: `drop table station_answer`},
		{SQL: `create table station_answer (
			id text primary key,
			submission_id text not null references submission(id) on delete cascade,
			station_key text not null,
			question_key text not null,
			answer text not null check (answer in ('pass','fail','na')),
			evidence text not null default '',
			artifact_hash text,
			reviewer_id text,
			created_at text not null,
			updated_at text not null,
			unique (submission_id, station_key, question_key)
		)`},
		{SQL: `insert into station_answer (id, submission_id, station_key, question_key, answer, created_at, updated_at)
			select 'old-answer', id, 'narrative', 'q1', 'pass', created_at, created_at from submission limit 1`},
	}
}
