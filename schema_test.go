package main

import (
	"context"
	"testing"
)

// TestMigrationReplay applies the full statement list twice more at higher
// versions. The storage service replays every statement on each deploy, so
// every statement must be rerun-safe: the second and third applies have to
// succeed and preserve rows.
func TestMigrationReplay(t *testing.T) {
	application := newTestApp(t)
	ctx := context.Background()

	for _, version := range []int{schemaVersion + 1, schemaVersion + 2} {
		if err := application.store.Migrate(ctx, version, schemaStatements()...); err != nil {
			t.Fatalf("replay at v%d: %s", version, err)
		}
	}

	var members int

	if err := application.store.QueryRow(ctx, `select count(*) from membership`).Scan(&members); err != nil {
		t.Fatal(err)
	}

	if members != 3 {
		t.Fatalf("members after replay = %d", members)
	}

	definitions, err := application.listRoleDefinitions(ctx, programID)
	if err != nil {
		t.Fatal(err)
	}

	if len(definitions) != 4 {
		t.Fatalf("roles after replay = %+v", definitions)
	}

	var journalColumns int

	if err := application.store.QueryRow(ctx, `select count(*) from pragma_table_info('journal_entry')
		where name in ('approved_seconds', 'reviewer_note')`).Scan(&journalColumns); err != nil {
		t.Fatal(err)
	}

	if journalColumns != 2 {
		t.Fatalf("journal columns after replay = %d", journalColumns)
	}
}
