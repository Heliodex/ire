package main

import (
	"net/http"
	"testing"

	"review/contract"
)

// TestResubmissionDiffShowsWhatChanged covers the previous decision, commits, files, journal entries and BOM changes shown above a resubmitted ship.
func TestResubmissionDiffShowsWhatChanged(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	now := application.nowText()

	if _, err := application.store.Exec(t.Context(), `update submission set previous_submission_id = 'sub_fixture_1' where id = 'sub_fixture_2'`); err != nil {
		t.Fatal(err)
	}

	var reviewerID string
	if err := application.store.QueryRow(t.Context(), `select id from user where email = ?`, "reviewer@example.com").Scan(&reviewerID); err != nil {
		t.Fatal(err)
	}

	if _, err := application.store.Exec(t.Context(), `insert into review (id, submission_id, version, stage, reviewer_id, reviewer_email, reviewer_role, decision, note_to_maker, policy_version, is_final, created_at)
		values ('rev_previous', 'sub_fixture_1', 1, 1, ?, 'reviewer@example.com', 'reviewer', 'changes', 'Add the BOM and organise the repository.', 1, 1, ?)`, reviewerID, now); err != nil {
		t.Fatal(err)
	}

	statements := []struct {
		sql  string
		args []any
	}{
		{`insert or replace into repo_snapshot (submission_id, version, forge, full_name, head_sha, files, commits, fetched_at)
			values ('sub_fixture_1', 1, 'github', 'maker/old', 'oldsha', ?, '[]', ?)`,
			[]any{`[{"path":"README.md","kind":"text"},{"path":"board.kicad_pcb","kind":"pcb"}]`, now}},
		{`insert or replace into repo_snapshot (submission_id, version, forge, full_name, head_sha, files, commits, fetched_at)
			values ('sub_fixture_2', 1, 'github', 'maker/new', 'newsha', ?, ?, ?)`,
			[]any{`[{"path":"README.md","kind":"text"},{"path":"firmware/main.ino","kind":"firmware"}]`, `[{"sha":"abc123","message":"Add firmware","author":"Maker","date":"` + now + `"}]`, now}},
	}

	for _, statement := range statements {
		if _, err := application.store.Exec(t.Context(), statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_2", nil, reviewer, nil))
	if bundle.Resubmission == nil {
		t.Fatal("resubmission diff missing")
	}

	diff := bundle.Resubmission
	if diff.PreviousDecision == nil || *diff.PreviousDecision != "changes" {
		t.Errorf("previous decision = %v", diff.PreviousDecision)
	}
	if diff.PreviousNote == nil || *diff.PreviousNote == "" {
		t.Errorf("previous note = %v", diff.PreviousNote)
	}
	if len(diff.Commits) != 1 || diff.Commits[0].Sha != "abc123" {
		t.Errorf("commits = %+v", diff.Commits)
	}
	if !contains(diff.AddedFiles, "firmware/main.ino") {
		t.Errorf("added files = %v", diff.AddedFiles)
	}
	if !contains(diff.RemovedFiles, "board.kicad_pcb") {
		t.Errorf("removed files = %v", diff.RemovedFiles)
	}
	if len(diff.NewEntries) == 0 {
		t.Errorf("new entries = %+v", diff.NewEntries)
	}
	if len(diff.BomAdded) == 0 || len(diff.BomRemoved) == 0 {
		t.Errorf("BOM changes = added %v, removed %v", diff.BomAdded, diff.BomRemoved)
	}
}

// contains reports whether a list carries a value.
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}
