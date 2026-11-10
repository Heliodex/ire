package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// adjustmentSourceHackatime is the ledger source for Hackatime cuts.
const adjustmentSourceHackatime = "hackatime"

// handleHackatimeReview records the approved Hackatime time and deflation note.
func (a *ireApp) handleHackatimeReview(w http.ResponseWriter, r *http.Request) {
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
		app.WriteError(w, http.StatusConflict, "claim the ship before adjusting its Hackatime time")
		return
	}

	var request contract.HackatimeReviewRequest
	if !app.ReadJSON(w, r, maxAPIBody, "approvedSeconds", &request) {
		return
	}

	if request.Version != ship.Version {
		app.WriteErrorCode(w, http.StatusConflict, "stale_version", "this ship changed since you opened it; reload before adjusting its Hackatime time")
		return
	}

	if request.ApprovedSeconds > maxHours {
		app.WriteError(w, http.StatusUnprocessableEntity, "approved time must be no more than 30 days")
		return
	}

	adjustment, total, err := a.saveLedgerCut(r.Context(), ship, person, adjustmentSourceHackatime, ship.HoursHackatim, request.ApprovedSeconds, stringValue(request.DeflationReason), request.ReviewerNote)
	if err != nil {
		writeLedgerCutError(w, err)
		return
	}

	app.WriteJSON(w, http.StatusOK, contract.HackatimeAdjustment{Adjustment: adjustment, HoursApprovedSeconds: total})
}

// handleTimelapseReview records one clip's approved time and deflation note.
func (a *ireApp) handleTimelapseReview(w http.ResponseWriter, r *http.Request) {
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
		app.WriteError(w, http.StatusConflict, "claim the ship before adjusting its timelapse")
		return
	}

	var request contract.TimelapseReviewRequest
	if !app.ReadJSON(w, r, maxAPIBody, "approvedSeconds", &request) {
		return
	}

	if request.Version != ship.Version {
		app.WriteErrorCode(w, http.StatusConflict, "stale_version", "this ship changed since you opened it; reload before adjusting its timelapse")
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

	index, err := strconv.Atoi(r.PathValue("clip"))
	if err != nil || index < 0 || index >= len(entry.Timelapses) {
		app.WriteError(w, http.StatusNotFound, "unknown timelapse clip")
		return
	}

	clip := entry.Timelapses[index]
	externalID := ""
	if clip.ExternalID != nil {
		externalID = *clip.ExternalID
	}

	adjustment, total, err := a.saveLedgerCut(r.Context(), ship, person, clipSource(entry.ID, index, externalID), clip.CoveredSeconds, request.ApprovedSeconds, stringValue(request.DeflationReason), request.ReviewerNote)
	if err != nil {
		writeLedgerCutError(w, err)
		return
	}

	journal, err := a.loadJournalEntry(r.Context(), ship.ID, entry.ID)
	if err != nil {
		writeStorage(w, err)
		return
	}

	app.WriteJSON(w, http.StatusOK, contract.TimelapseAdjustment{Adjustment: adjustment, HoursApprovedSeconds: total, Journal: journal})
}

// errLedgerCutAboveClaim reports an approved figure above the source claim.
var errLedgerCutAboveClaim = errors.New("approved time must not exceed the claimed time")

// saveLedgerCut replaces the cut for one ledger source and returns the new approved total.
// A cut of zero removes the row, so restoring a source clears its deflation.
func (a *ireApp) saveLedgerCut(ctx context.Context, ship submissionRow, person reviewer, source string, claimed, approved int, reason, note string) (*contract.TimeAdjustment, int, error) {
	if approved > claimed {
		return nil, 0, errLedgerCutAboveClaim
	}

	if approved < claimed && strings.TrimSpace(reason) == "" {
		return nil, 0, errLedgerCutNeedsReason
	}

	cut := claimed - approved
	now := a.nowText()

	statements := []storage.TransactionStatement{
		{
			SQL:  `delete from time_adjustment where submission_id = ? and source = ?`,
			Args: []any{ship.ID, source},
		},
	}

	var adjustment *contract.TimeAdjustment

	if cut > 0 {
		id := newID("tadj")
		statements = append(statements, storage.TransactionStatement{
			SQL:  `insert into time_adjustment (id, submission_id, kind, seconds, reason, note, reviewer_id, source, created_at) values (?, ?, 'cut', ?, ?, ?, ?, ?, ?)`,
			Args: []any{id, ship.ID, cut, strings.TrimSpace(reason), nullString(note), person.ID, source, now},
		})
		adjustment = &contract.TimeAdjustment{
			CreatedAt: now,
			ID:        id,
			Kind:      "cut",
			Reason:    strings.TrimSpace(reason),
			Seconds:   cut,
			Source:    source,
		}
		adjustment.Note = optionalText(note)
		adjustment.ReviewerID = optionalText(person.ID)
	}

	base, err := a.ledgerBaseTotal(ctx, ship.ID, source)
	if err != nil {
		return nil, 0, err
	}

	total := base - cut

	statements = append(statements,
		storage.TransactionStatement{
			SQL:  `update submission set hours_approved_seconds = ?, updated_at = ? where id = ?`,
			Args: []any{total, now, ship.ID},
		},
		storage.TransactionStatement{
			SQL: `insert into submission_event (id, submission_id, kind, actor_id, actor_email, data, created_at)
				values (?, ?, 'time_adjusted', ?, ?, ?, ?)`,
			Args: []any{newID("evt"), ship.ID, person.ID, person.Email, marshalMap(map[string]any{"source": source, "approvedSeconds": approved, "reviewerNote": note, "deflationReason": reason}), now},
		},
		storage.TransactionStatement{
			SQL: `insert into audit_log (id, program_id, actor_id, actor_email, action, target_type, target_id, before_json, after_json, created_at)
				values (?, ?, ?, ?, 'time.adjust', 'submission', ?, ?, ?, ?)`,
			Args: []any{newID("aud"), ship.ProgramID, person.ID, person.Email, ship.ID,
				marshalMap(map[string]any{"source": source}),
				marshalMap(map[string]any{"source": source, "approvedSeconds": approved, "reviewerNote": note, "deflationReason": reason}), now},
		},
	)

	if _, err := a.store.Transaction(ctx, statements...); err != nil {
		return nil, 0, err
	}

	return adjustment, total, nil
}

// errLedgerCutNeedsReason reports a cut without its required reason.
var errLedgerCutNeedsReason = errors.New("explain why this time counts for less")

// writeLedgerCutError maps a ledger validation failure to its field error.
func writeLedgerCutError(w http.ResponseWriter, err error) {
	if errors.Is(err, errLedgerCutNeedsReason) {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "deflation_reason_required", "deflationReason", "explain why this time counts for less")
		return
	}

	if errors.Is(err, errLedgerCutAboveClaim) {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "approvedSeconds", "approved time must not exceed the claimed time")
		return
	}

	writeStorage(w, err)
}

// ledgerBaseTotal sums each entry's approved time plus every ledger cut and
// bonus except the source being replaced, so the new row cannot double-count.
func (a *ireApp) ledgerBaseTotal(ctx context.Context, submissionID, excludeSource string) (int, error) {
	rows, err := a.store.Query(ctx, `select coalesce(approved_seconds, -1), seconds from journal_entry where submission_id = ?`, submissionID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var total int

	for rows.Next() {
		var approved, seconds int
		if err := rows.Scan(&approved, &seconds); err != nil {
			return 0, err
		}

		if approved >= 0 {
			total += approved
		} else {
			total += seconds
		}
	}

	if err := rows.Err(); err != nil {
		return 0, err
	}

	var adjustments int
	if err := a.store.QueryRow(ctx, `select coalesce(sum(case kind when 'bonus' then seconds else -seconds end), 0)
		from time_adjustment where submission_id = ? and source != ?`, submissionID, excludeSource).Scan(&adjustments); err != nil {
		return 0, err
	}

	return total + adjustments, nil
}
