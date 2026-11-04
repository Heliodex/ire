package main

import (
	"testing"
)

// TestPruneRetention covers short-lived rows and closed-ship snapshots without touching open ships.
func TestPruneRetention(t *testing.T) {
	application := newTestApp(t)
	ctx := t.Context()

	old := "2025-01-01T00:00:00.000000000Z"
	fresh := application.nowText()

	statements := []struct {
		SQL  string
		Args []any
	}{
		{`insert into login_flow (state, verifier, nonce, redirect_uri, created_at) values ('old', 'v', 'n', 'http://x', ?)`, []any{old}},
		{`insert into login_flow (state, verifier, nonce, redirect_uri, created_at) values ('new', 'v', 'n', 'http://x', ?)`, []any{fresh}},
		{`insert into ingest_delivery (delivery_id, program_id, status, http_status, received_at) values ('dl_old', ?, 'accepted', 201, ?)`, []any{programID, old}},
		{`insert into ingest_delivery (delivery_id, program_id, status, http_status, received_at) values ('dl_new', ?, 'accepted', 201, ?)`, []any{programID, fresh}},
		{`insert into audit_log (id, program_id, action, created_at) values ('aud_old', ?, 'test', ?)`, []any{programID, old}},
		{`insert into audit_log (id, program_id, action, created_at) values ('aud_new', ?, 'test', ?)`, []any{programID, fresh}},
		{`insert into fake_receiver_delivery (id, delivery_id, event, timestamp, body, signature, received_at) values ('frd_old', 'd_old', 'review.approved', ?, '{}', 'sig', ?)`, []any{old, old}},
		{`update submission set status = 'decided' where id = 'sub_fixture_1'`, nil},
		{`update repo_snapshot set fetched_at = ? where submission_id in ('sub_fixture_1', 'sub_fixture_2')`, []any{old}},
	}

	for _, statement := range statements {
		if _, err := application.store.Exec(ctx, statement.SQL, statement.Args...); err != nil {
			t.Fatal(err)
		}
	}

	result, err := application.pruneRetention(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if result.LoginFlows != 1 || result.Deliveries != 1 || result.AuditRecords != 1 || result.FakeReceipts != 1 {
		t.Fatalf("result = %+v", result)
	}

	if result.Snapshots != 1 {
		t.Fatalf("snapshots = %d", result.Snapshots)
	}

	var remaining int

	if err := application.store.QueryRow(ctx, `select count(*) from login_flow`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}

	if remaining != 1 {
		t.Errorf("login flows = %d", remaining)
	}

	if err := application.store.QueryRow(ctx, `select count(*) from repo_snapshot where submission_id = 'sub_fixture_2'`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}

	if remaining != 1 {
		t.Errorf("open snapshot = %d", remaining)
	}
}
