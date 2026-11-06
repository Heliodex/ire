package main

import (
	"context"
	"testing"

	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// TestLateColumnsUpgrade simulates a database created before journal adjudication added its columns. The create statement is skipped for an existing table, so migrate has to add them.
func TestLateColumnsUpgrade(t *testing.T) {
	application := newTestApp(t)
	ctx := context.Background()

	for _, column := range []string{"approved_seconds", "reviewer_note"} {
		if _, err := application.store.Exec(ctx, `alter table journal_entry drop column `+column); err != nil {
			t.Skipf("storage cannot drop a column: %v", err)
		}
	}

	late, err := lateColumnStatements(ctx, application.store)
	if err != nil {
		t.Fatal(err)
	}

	if len(late) != 2 {
		t.Fatalf("late statements = %d, want 2", len(late))
	}

	if err := application.store.Migrate(ctx, schemaVersion+1, append(schemaStatements(), late...)...); err != nil {
		t.Fatal(err)
	}

	var columns int

	if err := application.store.QueryRow(ctx, `select count(*) from pragma_table_info('journal_entry')
		where name in ('approved_seconds', 'reviewer_note')`).Scan(&columns); err != nil {
		t.Fatal(err)
	}

	if columns != 2 {
		t.Fatalf("journal columns = %d, want 2", columns)
	}
}

// TestLateColumnsFreshDatabase checks that a database without the table gets its columns from the create statement alone, so no alter is appended that would duplicate them.
func TestLateColumnsFreshDatabase(t *testing.T) {
	server, err := storage.NewTestServer()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(server.Close)

	late, err := lateColumnStatements(context.Background(), server.NewClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	if len(late) != 0 {
		t.Fatalf("late statements on an empty database = %d, want 0", len(late))
	}
}
