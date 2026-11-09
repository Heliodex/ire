package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// maxImportBatch keeps one backfill request below the body cap and the storage statement limit.
const maxImportBatch = 50

// handleImportReviewed backfills ships that Ari already decided. It is machine-only and never writes outbound
// events, reruns checks or opens a resubmission version.
func (a *ireApp) handleImportReviewed(w http.ResponseWriter, r *http.Request) {
	program, ok := a.programFor(w, r)
	if !ok {
		return
	}

	if !a.requireMachine(w, r) {
		return
	}

	var request contract.ImportShipsRequest
	if !app.ReadJSON(w, r, maxIngestBody, "ships", &request) {
		return
	}

	if len(request.Ships) == 0 {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "ships", "ships is required")

		return
	}

	if len(request.Ships) > maxImportBatch {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "ships",
			fmt.Sprintf("at most %d ships fit in one import", maxImportBatch))

		return
	}

	response := contract.ImportResult{Failed: []contract.ImportResultFailedElem{}}

	for _, entry := range request.Ships {
		externalID := strings.TrimSpace(entry.Ship.ExternalID)

		imported, err := a.importReviewedShip(r.Context(), program, entry)
		if err != nil {
			response.Failed = append(response.Failed, contract.ImportResultFailedElem{
				Error:      err.Error(),
				ExternalID: externalID,
			})

			continue
		}

		if imported {
			response.Imported++
		} else {
			response.Skipped++
		}
	}

	app.WriteJSON(w, http.StatusOK, response)
}

// importReviewedShip writes one historical decision, deciding an open copy in place instead of adding a version.
func (a *ireApp) importReviewedShip(ctx context.Context, program programRow, entry contract.ImportShip) (bool, error) {
	externalID := strings.TrimSpace(entry.Ship.ExternalID)
	if externalID == "" {
		return false, errors.New("external_id is required")
	}

	reviews := entry.Reviews
	if len(reviews) == 0 {
		return false, errors.New("at least one review is required")
	}

	// The caller sends the reviews in decision order; the last one is final.
	final := reviews[len(reviews)-1]

	var (
		existingID      string
		existingVersion int
		existingStatus  string
	)

	err := a.store.QueryRow(ctx, `select id, version, status from submission
		where program_id = ? and external_id = ? order by version desc limit 1`, program.ID, externalID).
		Scan(&existingID, &existingVersion, &existingStatus)

	switch {
	case err == nil:
		if existingStatus == statusDecided {
			submittedAt := strings.TrimSpace(stringValue(entry.Ship.SubmittedAt))
			if submittedAt == "" {
				return false, nil
			}

			result, err := a.store.Exec(ctx, `update submission set submitted_at = coalesce(submitted_at, ?), updated_at = ? where id = ?`,
				submittedAt, a.nowText(), existingID)
			if err != nil {
				return false, err
			}

			return result.RowsAffected > 0, nil
		}

		return true, a.decideImportedShip(ctx, program, existingID, existingVersion, entry.Ship.SubmittedAt, reviews, final)
	case !errors.Is(err, storage.ErrNoRows):
		return false, err
	}

	version := 1
	if entry.Version != nil && *entry.Version > 0 {
		version = *entry.Version
	}

	return true, a.createImportedShip(ctx, program, entry.Ship, version, reviews, final)
}

// createImportedShip inserts a decided ship with its journals, BOM and review history, but no checks or outbound delivery.
func (a *ireApp) createImportedShip(
	ctx context.Context,
	program programRow,
	ship contract.IngestRequest,
	version int,
	reviews []contract.ImportReview,
	final contract.ImportReview,
) error {
	id := newID("sub")

	fields := buildSubmissionFields(ship)

	summary, err := json.Marshal(contract.CheckSummary{Overall: checkOverallPass})
	if err != nil {
		return err
	}

	payload, err := json.Marshal(ship)
	if err != nil {
		return err
	}

	statements := make([]storage.TransactionStatement, 0, 8+len(ship.Journals))
	statements = append(statements, submissionStatement(id, program, ship, fields, version, "", statusDecided, string(summary), a.nowText()))
	statements = append(statements,
		storage.TransactionStatement{SQL: `update submission set hours_approved_seconds = ?, decided_at = ?, ingested_at = ?, created_at = ?, updated_at = ? where id = ?`,
			Args: []any{final.HoursApprovedSeconds, final.DecidedAt, final.DecidedAt, final.DecidedAt, final.DecidedAt, id}},
		storage.TransactionStatement{SQL: `insert into submission_version (submission_id, version, payload, payload_sha256, received_at, delivery_id)
				values (?, ?, ?, ?, ?, ?)`,
			Args: []any{id, version, string(payload), sha256Hex(payload), final.DecidedAt, "import:" + id + ":" + strconv.Itoa(version)}},
	)

	statements = append(statements, journalStatements(id, ship.Journals)...)
	statements = append(statements, hackatimeStatements(id, ship.HackatimeProjects)...)
	statements = append(statements, bomStatements(id, ship.Meta)...)
	statements = append(statements, a.importReviewStatements(ctx, program.ID, id, version, reviews)...)
	statements = append(statements, importEventStatements(id, reviews)...)
	statements = append(statements, a.importAuditStatement(program, id, final))

	return a.transactShip(ctx, id, statements)
}

// decideImportedShip marks an existing open copy decided, keeping its version and artifacts.
func (a *ireApp) decideImportedShip(
	ctx context.Context,
	program programRow,
	id string,
	version int,
	submittedAt *string,
	reviews []contract.ImportReview,
	final contract.ImportReview,
) error {
	statements := make([]storage.TransactionStatement, 0, 4+2*len(reviews))
	statements = append(statements,
		storage.TransactionStatement{SQL: `update submission set status = ?, hours_approved_seconds = ?, decided_at = ?, submitted_at = coalesce(submitted_at, ?), updated_at = ? where id = ?`,
			Args: []any{statusDecided, final.HoursApprovedSeconds, final.DecidedAt, nullString(stringValue(submittedAt)), final.DecidedAt, id}},
		storage.TransactionStatement{SQL: `delete from claim where submission_id = ?`, Args: []any{id}},
		storage.TransactionStatement{SQL: `delete from draft where submission_id = ?`, Args: []any{id}},
	)

	statements = append(statements, a.importReviewStatements(ctx, program.ID, id, version, reviews)...)
	statements = append(statements, importEventStatements(id, reviews)...)
	statements = append(statements, a.importAuditStatement(program, id, final))

	_, err := a.store.Transaction(ctx, statements...)

	return err
}

// importReviewStatements inserts the review history, marking only the last review final.
func (a *ireApp) importReviewStatements(
	ctx context.Context,
	programID, submissionID string,
	version int,
	reviews []contract.ImportReview,
) []storage.TransactionStatement {
	policy := a.policyVersion(ctx, programID)
	statements := make([]storage.TransactionStatement, 0, len(reviews))

	for i, review := range reviews {
		statements = append(statements, storage.TransactionStatement{
			SQL: `insert into review (id, submission_id, version, stage, reviewer_id, reviewer_email, reviewer_name, reviewer_role,
					decision, note_to_maker, internal_note, hours_approved_seconds, acknowledgements, policy_version, held, held_reason,
					supersedes_review_id, is_final, idempotency_key, created_at)
				values (?, ?, ?, ?, null, ?, ?, 'reviewer', ?, ?, ?, ?, ?, ?, 0, null, null, ?, ?, ?)`,
			Args: []any{
				newID("rev"), submissionID, version, review.Stage,
				review.ReviewerEmail, nullString(stringValue(review.ReviewerName)),
				review.Decision, strings.TrimSpace(stringValue(review.NoteToMaker)), nullString(stringValue(review.InternalNote)),
				review.HoursApprovedSeconds, marshalStrings(review.Acknowledgements), policy,
				boolInt(i == len(reviews)-1), nullString(importReviewKey(submissionID, review)), review.DecidedAt,
			},
		})
	}

	return statements
}

// importReviewKey keeps a replayed import from writing the same historical review twice.
func importReviewKey(submissionID string, review contract.ImportReview) string {
	return "import:" + submissionID + ":" + strconv.Itoa(review.Stage) + ":" + review.DecidedAt
}

// importEventStatements records the original ingestion and each historical decision on the ship timeline.
func importEventStatements(submissionID string, reviews []contract.ImportReview) []storage.TransactionStatement {
	statements := make([]storage.TransactionStatement, 0, len(reviews)+1)
	statements = append(statements, storage.TransactionStatement{
		SQL: `insert into submission_event (id, submission_id, kind, actor_id, actor_email, data, created_at)
			values (?, ?, 'ingested', null, null, ?, ?)`,
		Args: []any{newID("evt"), submissionID, `{"source":"import"}`, reviews[0].DecidedAt},
	})

	for _, review := range reviews {
		statements = append(statements, storage.TransactionStatement{
			SQL: `insert into submission_event (id, submission_id, kind, actor_id, actor_email, data, created_at)
				values (?, ?, 'decided', null, ?, ?, ?)`,
			Args: []any{newID("evt"), submissionID, review.ReviewerEmail,
				marshalMap(map[string]any{"decision": review.Decision, "imported": true}), review.DecidedAt},
		})
	}

	return statements
}

// importAuditStatement leaves the same audit trail a live decision would, marked as an import.
func (a *ireApp) importAuditStatement(program programRow, submissionID string, review contract.ImportReview) storage.TransactionStatement {
	return storage.TransactionStatement{
		SQL: `insert into audit_log (id, program_id, actor_id, actor_email, action, target_type, target_id, before_json, after_json, created_at)
			values (?, ?, null, ?, 'review.decision', 'submission', ?, null, ?, ?)`,
		Args: []any{newID("aud"), program.ID, review.ReviewerEmail, submissionID,
			marshalMap(map[string]any{"decision": review.Decision, "decided_at": review.DecidedAt,
				"hours": review.HoursApprovedSeconds, "imported": true}), a.nowText()},
	}
}
