package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

const (
	noteSubjectSubmission = "submission"
	noteSubjectProject    = "project"
	noteSubjectMaker      = "maker"
)

// handleNoteCreate stores an internal note on a ship, its project or its maker.
func (a *reviewApp) handleNoteCreate(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewerWrite(w, r)
	if !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	var request contract.NoteRequest
	if !app.ReadJSON(w, r, maxAPIBody, "body", &request) {
		return
	}

	body := strings.TrimSpace(request.Body)
	if body == "" {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "body", "write a note first")
		return
	}

	subjectID := noteSubjectID(ship, request.SubjectType)
	if subjectID == "" {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "subjectType", "choose submission, project or maker")
		return
	}

	now := a.nowText()
	id := newID("note")

	if _, err := a.store.Transaction(r.Context(),
		storage.TransactionStatement{
			SQL: `insert into review_note (id, program_id, subject_type, subject_id, author_id, author_email, author_name, body, created_at)
				values (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{id, ship.ProgramID, request.SubjectType, subjectID, person.ID, person.Email, nullString(person.Name), body, now},
		},
		storage.TransactionStatement{
			SQL: `insert into audit_log (id, program_id, actor_id, actor_email, action, target_type, target_id, before_json, after_json, created_at)
				values (?, ?, ?, ?, 'note.create', 'submission', ?, '{}', ?, ?)`,
			Args: []any{newID("aud"), ship.ProgramID, person.ID, person.Email, ship.ID, marshalMap(map[string]any{"subjectType": request.SubjectType, "subjectId": subjectID, "body": body}), now},
		},
	); err != nil {
		writeStorage(w, err)

		return
	}

	app.WriteJSON(w, http.StatusCreated, contract.NoteCreated{Note: contract.Note{
		ID:          id,
		SubjectType: request.SubjectType,
		SubjectID:   subjectID,
		AuthorEmail: person.Email,
		AuthorName:  optionalText(person.Name),
		Body:        body,
		CreatedAt:   now,
	}})
}

// handleNoteDelete removes a note the caller wrote, or any note for an admin.
func (a *reviewApp) handleNoteDelete(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewerWrite(w, r)
	if !ok {
		return
	}

	program, ok := a.programFor(w, r)
	if !ok {
		return
	}

	var authorID, body string

	err := a.store.QueryRow(r.Context(), `select coalesce(author_id, ''), body from review_note where id = ? and program_id = ?`, r.PathValue("id"), program.ID).
		Scan(&authorID, &body)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			app.WriteError(w, http.StatusNotFound, "unknown note")
			return
		}

		writeStorage(w, err)

		return
	}

	if authorID != person.ID && person.Role != roleAdmin {
		app.WriteError(w, http.StatusForbidden, "only the author or an admin can remove this note")
		return
	}

	now := a.nowText()

	if _, err := a.store.Transaction(r.Context(),
		storage.TransactionStatement{
			SQL:  `delete from review_note where id = ?`,
			Args: []any{r.PathValue("id")},
		},
		storage.TransactionStatement{
			SQL: `insert into audit_log (id, program_id, actor_id, actor_email, action, target_type, target_id, before_json, after_json, created_at)
				values (?, ?, ?, ?, 'note.delete', 'note', ?, ?, '{}', ?)`,
			Args: []any{newID("aud"), program.ID, person.ID, person.Email, r.PathValue("id"), marshalMap(map[string]any{"body": body}), now},
		},
	); err != nil {
		writeStorage(w, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// loadNotes returns the notes on this ship, its project and its maker, newest first.
func (a *reviewApp) loadNotes(ctx context.Context, ship submissionRow) ([]contract.Note, error) {
	rows, err := a.store.Query(ctx, `select id, subject_type, subject_id, author_email, coalesce(author_name, ''), body, created_at
		from review_note
		where program_id = ? and (
			(subject_type = 'submission' and subject_id = ?) or
			(subject_type = 'project' and subject_id = ?) or
			(subject_type = 'maker' and subject_id = ?)
		)
		order by created_at desc, id desc`, ship.ProgramID, noteSubjectID(ship, noteSubjectSubmission), noteSubjectID(ship, noteSubjectProject), noteSubjectID(ship, noteSubjectMaker))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []contract.Note{}

	for rows.Next() {
		var (
			note       contract.Note
			authorName string
		)

		if err := rows.Scan(&note.ID, &note.SubjectType, &note.SubjectID, &note.AuthorEmail, &authorName, &note.Body, &note.CreatedAt); err != nil {
			return nil, err
		}

		note.AuthorName = optionalText(authorName)
		out = append(out, note)
	}

	return out, rows.Err()
}

// noteSubjectID maps a note subject to the stable key that crosses versions.
func noteSubjectID(ship submissionRow, subjectType string) string {
	switch subjectType {
	case noteSubjectSubmission:
		return ship.ID
	case noteSubjectProject:
		return ship.ExternalID
	case noteSubjectMaker:
		return strings.ToLower(strings.TrimSpace(ship.MakerEmail))
	default:
		return ""
	}
}
