package main

import (
	"context"
	"errors"
	"net/http"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// handleJournalReview records a reviewer's approved time and deflation note for one journal entry.
func (a *reviewApp) handleJournalReview(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewerWrite(w, r)
	if !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	if ship.Status == statusDecided || ship.Status == statusWithdrawn {
		app.WriteError(w, http.StatusConflict, "this ship has been closed")
		return
	}

	if !a.holdsClaim(r.Context(), ship.ID, person.ID) {
		app.WriteError(w, http.StatusConflict, "claim the ship before adjusting its journal")
		return
	}

	var request contract.JournalReviewRequest
	if !app.ReadJSON(w, r, maxAPIBody, "approvedSeconds", &request) {
		return
	}

	if request.Version != ship.Version {
		app.WriteErrorCode(w, http.StatusConflict, "stale_version", "this ship changed since you opened it; reload before adjusting its journal")
		return
	}

	if request.ApprovedSeconds > maxHours {
		app.WriteError(w, http.StatusUnprocessableEntity, "approved time must be no more than 30 days")
		return
	}

	entry, err := a.loadJournalEntry(r.Context(), ship.ID, r.PathValue("entry"))
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			app.WriteError(w, http.StatusNotFound, "unknown journal entry")
			return
		}

		writeStorage(w, err)

		return
	}

	total, err := a.journalApprovedTotal(r.Context(), ship.ID, entry.ID, request.ApprovedSeconds)
	if err != nil {
		writeStorage(w, err)

		return
	}

	now := a.nowText()
	note := request.ReviewerNote

	if _, err := a.store.Transaction(r.Context(),
		storage.TransactionStatement{
			SQL:  `update journal_entry set approved_seconds = ?, reviewer_note = ? where id = ? and submission_id = ?`,
			Args: []any{request.ApprovedSeconds, nullString(note), entry.ID, ship.ID},
		},
		storage.TransactionStatement{
			SQL:  `update submission set hours_approved_seconds = ?, updated_at = ? where id = ?`,
			Args: []any{total, now, ship.ID},
		},
		storage.TransactionStatement{
			SQL: `insert into submission_event (id, submission_id, kind, actor_id, actor_email, data, created_at)
				values (?, ?, 'journal_adjusted', ?, ?, ?, ?)`,
			Args: []any{newID("evt"), ship.ID, person.ID, person.Email, marshalMap(map[string]any{"entry": entry.ID, "approvedSeconds": request.ApprovedSeconds, "reviewerNote": note}), now},
		},
		storage.TransactionStatement{
			SQL: `insert into audit_log (id, program_id, actor_id, actor_email, action, target_type, target_id, before_json, after_json, created_at)
				values (?, ?, ?, ?, 'journal.adjust', 'submission', ?, ?, ?, ?)`,
			Args: []any{newID("aud"), ship.ProgramID, person.ID, person.Email, ship.ID,
				marshalMap(map[string]any{"entry": entry.ID, "approvedSeconds": entry.ApprovedSeconds, "reviewerNote": entry.ReviewerNote}),
				marshalMap(map[string]any{"entry": entry.ID, "approvedSeconds": request.ApprovedSeconds, "reviewerNote": note}), now},
		},
	); err != nil {
		writeStorage(w, err)

		return
	}

	entry.ApprovedSeconds = &request.ApprovedSeconds
	entry.ReviewerNote = optionalText(note)

	app.WriteJSON(w, http.StatusOK, contract.JournalAdjustment{Journal: entry, HoursApprovedSeconds: total})
}

// loadJournalEntry reads one journal entry for a ship.
func (a *reviewApp) loadJournalEntry(ctx context.Context, submissionID, entryID string) (contract.JournalEntry, error) {
	var (
		item                                        contract.JournalEntry
		title, text, markdown, mediaJSON, clipsJSON string
		approvedSeconds                             int
		reviewerNote                                string
	)

	err := a.store.QueryRow(ctx, `select id, at, seconds, coalesce(title, ''), coalesce(text, ''), coalesce(markdown, ''),
			coalesce(media, '[]'), coalesce(timelapses, '[]'), coalesce(approved_seconds, -1), coalesce(reviewer_note, '')
		from journal_entry where id = ? and submission_id = ?`, entryID, submissionID).
		Scan(&item.ID, &item.At, &item.Seconds, &title, &text, &markdown, &mediaJSON, &clipsJSON, &approvedSeconds, &reviewerNote)
	if err != nil {
		return contract.JournalEntry{}, err
	}

	item.Title = stringPtr(title)
	item.Text = stringPtr(text)
	item.Markdown = stringPtr(markdown)
	item.MediaCount = jsonArrayLength(mediaJSON)
	item.ClipCount = jsonArrayLength(clipsJSON)
	item.CoveredSeconds = jsonCoveredSeconds(clipsJSON)

	if approvedSeconds >= 0 {
		item.ApprovedSeconds = &approvedSeconds
	}

	item.ReviewerNote = optionalText(reviewerNote)

	return item, nil
}

// journalApprovedTotal sums each entry's approved time, using the claimed time where an entry has no adjustment.
func (a *reviewApp) journalApprovedTotal(ctx context.Context, submissionID, entryID string, approved int) (int, error) {
	rows, err := a.store.Query(ctx, `select id, seconds, coalesce(approved_seconds, -1) from journal_entry where submission_id = ?`, submissionID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var total int

	for rows.Next() {
		var (
			id                string
			seconds, existing int
		)

		if err := rows.Scan(&id, &seconds, &existing); err != nil {
			return 0, err
		}

		switch {
		case id == entryID:
			total += approved
		case existing >= 0:
			total += existing
		default:
			total += seconds
		}
	}

	return total, rows.Err()
}
