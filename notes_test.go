package main

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	"review/contract"
)

// TestNotesAreSharedAndRemovable covers creating an internal note, seeing it in the bundle and removing it as its author.
func TestNotesAreSharedAndRemovable(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")
	admin := login(t, application, "admin@example.com")

	body, _ := json.Marshal(contract.NoteRequest{Body: "The maker reused the last board. Check the schematic.", SubjectType: noteSubjectProject})

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

// TestNoteRejectsEmptyBody covers the blank note guard.
func TestNoteRejectsEmptyBody(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")

	body, _ := json.Marshal(contract.NoteRequest{Body: "   ", SubjectType: noteSubjectMaker})

	created := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/notes", body, reviewer, nil)
	if created.Code != http.StatusUnprocessableEntity {
		t.Fatalf("blank note = %d: %s", created.Code, created.Body.String())
	}
}
