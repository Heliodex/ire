package main

import (
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	"ire/contract"
)

// TestNotesAreSharedAndRemovable covers creating an internal note, seeing it in the bundle and removing it as its author.
func TestNotesAreSharedAndRemovable(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	admin := login(t, application, "admin@example.com")

	body, _ := json.Marshal(contract.NoteRequest{Body: "The maker reused the last board. Check the schematic.", SubjectType: noteSubjectProject})

	if unclaimed := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/notes", body, reviewer, nil); unclaimed.Code != http.StatusConflict {
		t.Fatalf("unclaimed note = %d: %s", unclaimed.Code, unclaimed.Body.String())
	}

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	created := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/notes", body, reviewer, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create note = %d: %s", created.Code, created.Body.String())
	}

	note := decode[contract.NoteCreated](t, created).Note
	if note.SubjectID != "fix-clean:design" || note.Body == "" {
		t.Errorf("note = %+v", note)
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, reviewer, nil))
	if len(bundle.Notes) != 1 || bundle.Notes[0].ID != note.ID {
		t.Fatalf("bundle notes = %+v", bundle.Notes)
	}

	// An organiser who did not write the note cannot delete it.
	organiser := login(t, application, "organiser@example.com")
	if denied := do(t, application, http.MethodDelete, testBase+"/notes/"+note.ID, nil, organiser, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("organiser delete = %d: %s", denied.Code, denied.Body.String())
	}

	if removed := do(t, application, http.MethodDelete, testBase+"/notes/"+note.ID, nil, reviewer, nil); removed.Code != http.StatusNoContent {
		t.Fatalf("author delete = %d: %s", removed.Code, removed.Body.String())
	}

	reloaded := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, admin, nil))
	if len(reloaded.Notes) != 0 {
		t.Errorf("notes after delete = %+v", reloaded.Notes)
	}
}

// TestClosedShipsRefuseNotesAndFindingUpdates covers the closed-ship guard on notes
// and finding updates, which used to report a misleading missing claim.
func TestClosedShipsRefuseNotesAndFindingUpdates(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	createBody, _ := json.Marshal(contract.FindingCreateRequest{
		MakerAction: stringPtr("Add the Gerber ZIP."),
		Severity:    "fixable",
		Station:     "triage",
		Summary:     "The Gerber ZIP is missing.",
	})

	made := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/findings", createBody, reviewer, nil)
	if made.Code != http.StatusOK {
		t.Fatalf("create finding = %d: %s", made.Code, made.Body.String())
	}

	finding := decode[contract.FindingResponse](t, made).Finding

	withdraw := []byte(`{"external_id":"fix-clean:design"}`)
	if closed := do(t, application, http.MethodPost, "/api/ingest/half-life/withdraw", withdraw, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", withdraw),
	}); closed.Code != http.StatusOK {
		t.Fatalf("withdraw = %d: %s", closed.Code, closed.Body.String())
	}

	noteBody, _ := json.Marshal(contract.NoteRequest{Body: "Late note.", SubjectType: noteSubjectProject})

	if noted := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/notes", noteBody, reviewer, nil); noted.Code != http.StatusConflict {
		t.Fatalf("note on closed ship = %d: %s", noted.Code, noted.Body.String())
	} else if !strings.Contains(noted.Body.String(), "closed") {
		t.Fatalf("note on closed ship = %s, want a closed-ship message", noted.Body.String())
	}

	editBody, _ := json.Marshal(contract.FindingUpdateRequest{Status: "fixed"})

	if edited := do(t, application, http.MethodPatch, testBase+"/submissions/sub_fixture_1/findings/"+finding.ID, editBody, reviewer, nil); edited.Code != http.StatusConflict {
		t.Fatalf("finding update on closed ship = %d: %s", edited.Code, edited.Body.String())
	} else if !strings.Contains(edited.Body.String(), "closed") {
		t.Fatalf("finding update on closed ship = %s, want a closed-ship message", edited.Body.String())
	}
}

// TestNoteRejectsEmptyBody covers the blank note guard.
func TestNoteRejectsEmptyBody(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	body, _ := json.Marshal(contract.NoteRequest{Body: "   ", SubjectType: noteSubjectMaker})

	created := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/notes", body, reviewer, nil)
	if created.Code != http.StatusUnprocessableEntity {
		t.Fatalf("blank note = %d: %s", created.Code, created.Body.String())
	}
}
