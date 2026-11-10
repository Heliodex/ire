package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/outbox"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// finalReviewRow is the current final, non-invalidated decision for a ship.
type finalReviewRow struct {
	ID, ReviewerID, ReviewerEmail, ReviewerName string
	Stage                                       int
	Decision                                    string
	Hours                                       int
	NoteToMaker                                 string
	TechnicalFeatures                           string
	DeflationReason                             string
	SupersedesID                                string
	HeldReason                                  string
}

// loadFinalReview returns the ship's current final review.
func (a *ireApp) loadFinalReview(ctx context.Context, submissionID string) (finalReviewRow, bool, error) {
	var row finalReviewRow

	err := a.store.QueryRow(ctx, `select id, coalesce(reviewer_id, ''), reviewer_email, coalesce(reviewer_name, ''),
			stage, decision, coalesce(hours_approved_seconds, 0), note_to_maker,
			coalesce(technical_features, ''), coalesce(deflation_reason, ''),
			coalesce(supersedes_review_id, ''), coalesce(held_reason, '')
		from review where submission_id = ? and is_final = 1 and invalidated_at is null
		order by created_at desc limit 1`, submissionID).
		Scan(&row.ID, &row.ReviewerID, &row.ReviewerEmail, &row.ReviewerName,
			&row.Stage, &row.Decision, &row.Hours, &row.NoteToMaker,
			&row.TechnicalFeatures, &row.DeflationReason,
			&row.SupersedesID, &row.HeldReason)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			return finalReviewRow{}, false, nil
		}

		return finalReviewRow{}, false, err
	}

	return row, true, nil
}

// canEditDecision reports whether the actor may revert or amend this final review.
func canEditDecision(person reviewer, review finalReviewRow) bool {
	if person.Has(permWildcard) {
		return true
	}

	if review.ReviewerID != "" && review.ReviewerID == person.ID {
		return true
	}

	return strings.EqualFold(review.ReviewerEmail, person.Email)
}

// handleRevertDecision invalidates the final review and reopens the ship for review.
func (a *ireApp) handleRevertDecision(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewerWrite(w, r)
	if !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	var request contract.RevertDecisionRequest
	if !app.ReadJSON(w, r, maxAPIBody, "reason", &request) {
		return
	}

	if ship.Status != statusDecided {
		app.WriteError(w, http.StatusConflict, "only a decided ship can be reverted")
		return
	}

	if request.Version != ship.Version {
		app.WriteErrorCode(w, http.StatusConflict, "stale_version", "this ship changed since you opened it; reload before reverting")
		return
	}

	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "reason_required", "reason", "explain why this decision is being reverted")
		return
	}

	if len(reason) > maxNote {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "reason", "the reason is limited to 4000 characters")
		return
	}

	final, found, err := a.loadFinalReview(r.Context(), ship.ID)
	if err != nil {
		writeStorage(w, err)
		return
	}

	if !found {
		app.WriteError(w, http.StatusConflict, "no final review to revert")
		return
	}

	if !canEditDecision(person, final) {
		app.WriteError(w, http.StatusForbidden, "only an admin or the reviewer who made this decision can revert it")
		return
	}

	status, stage, hours, heldReason, err := a.revertState(r.Context(), ship, final)
	if err != nil {
		writeStorage(w, err)
		return
	}

	now := a.nowText()
	statements := []storage.TransactionStatement{
		{SQL: `update review set invalidated_at = ?, invalidated_by = ?, invalidation_reason = ?
				where id = ? and invalidated_at is null`,
			Args: []any{now, person.ID, reason, final.ID}},
		{SQL: `update submission set status = ?, stage = ?, hours_approved_seconds = ?, held_reason = ?,
				decided_at = null, updated_at = ? where id = ? and status = 'decided'`,
			Args: []any{status, stage, hours, nullString(heldReason), now, ship.ID}},
		{SQL: `delete from claim where submission_id = ?`, Args: []any{ship.ID}},
		{SQL: `delete from draft where submission_id = ?`, Args: []any{ship.ID}},
		{SQL: `insert into submission_event (id, submission_id, kind, actor_id, actor_email, data, created_at)
				values (?, ?, 'decision_reverted', ?, ?, ?, ?)`,
			Args: []any{newID("evt"), ship.ID, person.ID, person.Email,
				marshalMap(map[string]any{"reason": reason, "review": final.ID, "decision": final.Decision}), now}},
		{SQL: `insert into audit_log (id, program_id, actor_id, actor_email, action, target_type, target_id, before_json, after_json, created_at)
				values (?, ?, ?, ?, 'review.revert', 'submission', ?, ?, ?, ?)`,
			Args: []any{newID("aud"), ship.ProgramID, person.ID, person.Email, ship.ID,
				marshalMap(map[string]any{"status": ship.Status, "stage": ship.Stage, "version": ship.Version, "decision": final.Decision}),
				marshalMap(map[string]any{"status": status, "stage": stage, "invalidated": final.ID, "reason": reason}), now}},
	}

	if _, err := a.store.Transaction(r.Context(), statements...); err != nil {
		writeStorage(w, err)
		return
	}

	app.WriteJSON(w, http.StatusOK, contract.RevertDecisionResult{ID: ship.ID, Status: status, Stage: stage})
}

// revertState returns the submission state to restore when a final decision is reverted.
func (a *ireApp) revertState(ctx context.Context, ship submissionRow, final finalReviewRow) (status string, stage int, hours any, heldReason string, err error) {
	if final.Stage == 2 || ship.Stage == 2 {
		return a.revertStageTwoState(ctx, final)
	}

	return statusReady, stageOne, nil, "", nil
}

// revertStageTwoState restores a stage 2 decided ship to its held state.
func (a *ireApp) revertStageTwoState(ctx context.Context, final finalReviewRow) (status string, stage int, hours any, heldReason string, err error) {
	if final.SupersedesID == "" {
		return statusStage2Hold, 2, final.Hours, "", nil
	}

	var heldHours int

	err = a.store.QueryRow(ctx, `select coalesce(hours_approved_seconds, 0), coalesce(held_reason, '')
		from review where id = ?`, final.SupersedesID).Scan(&heldHours, &heldReason)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			return statusStage2Hold, 2, final.Hours, "", nil
		}

		return "", 0, nil, "", err
	}

	return statusStage2Hold, 2, heldHours, heldReason, nil
}

// handleAmendDecision replaces the final review with a corrected decision.
func (a *ireApp) handleAmendDecision(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewerWrite(w, r)
	if !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	var request contract.AmendDecisionRequest
	if !app.ReadJSON(w, r, maxAPIBody, "decision", &request) {
		return
	}

	if ship.Status != statusDecided {
		app.WriteError(w, http.StatusConflict, "only a decided ship can be amended")
		return
	}

	if request.Version != ship.Version {
		app.WriteErrorCode(w, http.StatusConflict, "stale_version", "this ship changed since you opened it; reload before amending")
		return
	}

	if existing, found, err := a.findReview(r.Context(), ship.ID, request.IdempotencyKey); err != nil {
		writeStorage(w, err)
		return
	} else if found {
		a.writeDecisionAgain(w, r, ship, existing)
		return
	}

	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "reason_required", "reason", "explain why this decision is being amended")
		return
	}

	if len(reason) > maxNote {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "reason", "the reason is limited to 4000 characters")
		return
	}

	note := strings.TrimSpace(request.NoteToMaker)
	if len(note) > maxNote || len(request.InternalNote) > maxNote {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "noteToMaker", "notes are limited to 4000 characters")
		return
	}

	final, found, err := a.loadFinalReview(r.Context(), ship.ID)
	if err != nil {
		writeStorage(w, err)
		return
	}

	if !found {
		app.WriteError(w, http.StatusConflict, "no final review to amend")
		return
	}

	if !canEditDecision(person, final) {
		app.WriteError(w, http.StatusForbidden, "only an admin or the reviewer who made this decision can amend it")
		return
	}

	if problem := a.amendNoteProblem(r.Context(), request, note, ship.ProgramID); problem != nil {
		problem.write(w)
		return
	}

	checks, err := a.latestChecks(r.Context(), ship.ID, ship.Version)
	if err != nil {
		writeStorage(w, err)
		return
	}

	secondPass := final.Stage == 2
	if problem := amendApprovalProblem(request, checks, secondPass); problem != nil {
		problem.write(w)
		return
	}

	if problem := a.findingProblem(r.Context(), contract.DecisionRequest{Decision: request.Decision, FindingIds: request.FindingIds}, ship); problem != nil {
		problem.write(w)
		return
	}

	hours := final.Hours
	if hours <= 0 {
		hours = ship.HoursClaimed
	}

	if request.HoursApprovedSeconds != nil {
		hours = *request.HoursApprovedSeconds
	}

	if hours < 0 || hours > maxHours {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "hoursApprovedSeconds", "approved hours must be between 0 and 30 days")
		return
	}

	technicalFeatures := strings.TrimSpace(stringValue(request.TechnicalFeatures))
	if technicalFeatures == "" {
		technicalFeatures = final.TechnicalFeatures
	}

	deflationReason := strings.TrimSpace(stringValue(request.DeflationReason))
	if deflationReason == "" {
		deflationReason = final.DeflationReason
	}

	if problem := justificationProblem(request.Decision, hours, capturedSeconds(ship), technicalFeatures, deflationReason, note, a.policyFor(r.Context(), ship.ProgramID)); problem != nil {
		problem.write(w)
		return
	}

	now := a.nowText()
	reviewID := newID("rev")

	stage := final.Stage
	if stage < stageOne {
		stage = ship.Stage
	}

	decisionReq := contract.DecisionRequest{
		Acknowledgements:     request.Acknowledgements,
		Decision:             request.Decision,
		DeflationReason:      &deflationReason,
		FindingIds:           request.FindingIds,
		HoursApprovedSeconds: request.HoursApprovedSeconds,
		IdempotencyKey:       request.IdempotencyKey,
		InternalNote:         request.InternalNote,
		NoteToMaker:          request.NoteToMaker,
		OverrideReason:       &reason,
		TechnicalFeatures:    &technicalFeatures,
		Version:              request.Version,
	}

	payloadJSON, err := a.decisionPayloadJSON(r.Context(), ship, person, checks, decisionReq, hours, note, technicalFeatures, deflationReason, reason, stage, now)
	if err != nil {
		writeStorage(w, err)
		return
	}

	event := outboundEventName(request.Decision)
	record := decisionRecord{
		ship: ship, person: person, request: decisionReq,
		reviewID: reviewID, status: statusDecided, stage: stage, submissionStage: ship.Stage,
		hours: hours, note: note, technicalFeatures: technicalFeatures, deflationReason: deflationReason,
		reason: "", overrideReason: reason, supersedes: final.ID, findingIDs: request.FindingIds,
		held: false, secondPass: secondPass, event: event, payload: payloadJSON, now: now,
	}

	statements, outboxID, problem := a.amendStatements(r.Context(), record, final.ID, person, now)
	if problem != nil {
		problem.write(w)
		return
	}

	if _, err := a.store.Transaction(r.Context(), statements...); err != nil {
		if existing, found, findErr := a.findReview(r.Context(), ship.ID, request.IdempotencyKey); findErr == nil && found {
			a.writeDecisionAgain(w, r, ship, existing)
			return
		}

		writeStorage(w, err)

		return
	}

	var deliveryStatus string
	if outboxID != "" {
		deliveryStatus = a.deliverOutboxNow(r.Context(), outboxID)
	}

	app.WriteJSON(w, http.StatusOK, contract.DecisionOutcome{
		ID:             reviewID,
		Status:         statusDecided,
		Held:           0,
		OutboxID:       optionalID(outboxID),
		DeliveryStatus: stringPtr(deliveryStatus),
		NextID:         nil,
	})
}

func (a *ireApp) amendNoteProblem(ctx context.Context, request contract.AmendDecisionRequest, note, programID string) *decisionProblem {
	if request.Decision != decisionApproved {
		policy := a.policyFor(ctx, programID)
		if policy.NoteRequiredForChanges && note == "" {
			return &decisionProblem{status: http.StatusUnprocessableEntity, code: "note_required", field: "noteToMaker", message: "a note to the maker is required for changes or rejection"}
		}
	}

	return nil
}

func amendApprovalProblem(request contract.AmendDecisionRequest, checks []contract.CheckResult, secondPass bool) *decisionProblem {
	if request.Decision != decisionApproved {
		return nil
	}

	if !secondPass {
		if missing := missingAcknowledgements(checks, request.Acknowledgements); len(missing) > 0 {
			return &decisionProblem{status: http.StatusUnprocessableEntity, code: "acknowledgement_required", field: "acknowledgements",
				message: "acknowledge the warnings before approving: " + strings.Join(missing, ", ")}
		}
	}

	if blocked := blockedChecks(checks); len(blocked) > 0 {
		return &decisionProblem{status: http.StatusUnprocessableEntity, code: "check_blocked", field: "decision",
			message: "approval is blocked: " + blocked[0].Summary + ". Ask the maker to resubmit or reject with a note."}
	}

	return nil
}

func (a *ireApp) amendStatements(ctx context.Context, record decisionRecord, finalID string, person reviewer, now string) ([]storage.TransactionStatement, string, *decisionProblem) {
	statements := []storage.TransactionStatement{
		{SQL: `update review set invalidated_at = ?, invalidated_by = ?, invalidation_reason = ?
				where id = ? and invalidated_at is null`,
			Args: []any{now, person.ID, record.overrideReason, finalID}},
		{SQL: `insert into review (id, submission_id, version, stage, reviewer_id, reviewer_email, reviewer_name, reviewer_role,
				decision, note_to_maker, internal_note, technical_features, deflation_reason, hours_approved_seconds,
				acknowledgements, policy_version, held, held_reason, supersedes_review_id, is_final, idempotency_key, created_at)
			values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{record.reviewID, record.ship.ID, record.ship.Version, record.stage, record.person.ID, record.person.Email, record.person.Name, record.person.Role,
				record.request.Decision, record.note, strings.TrimSpace(record.request.InternalNote), record.technicalFeatures, record.deflationReason, record.hours,
				marshalStrings(record.request.Acknowledgements), a.policyVersion(ctx, record.ship.ProgramID), 0, nil,
				nullString(record.supersedes), 1, record.request.IdempotencyKey, record.now}},
		{SQL: `update submission set status = ?, stage = ?, hours_approved_seconds = ?, held_reason = null,
				decided_at = ?, updated_at = ? where id = ?`,
			Args: []any{statusDecided, record.submissionStage, record.hours, record.now, record.now, record.ship.ID}},
		{SQL: `delete from claim where submission_id = ?`, Args: []any{record.ship.ID}},
		{SQL: `delete from draft where submission_id = ?`, Args: []any{record.ship.ID}},
		{SQL: `insert into submission_event (id, submission_id, kind, actor_id, actor_email, data, created_at) values (?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{newID("evt"), record.ship.ID, "decision_amended", record.person.ID, record.person.Email,
				marshalMap(map[string]any{"decision": record.request.Decision, "supersedes": finalID}), record.now}},
		{SQL: `insert into audit_log (id, program_id, actor_id, actor_email, action, target_type, target_id, before_json, after_json, created_at)
			values (?, ?, ?, ?, 'review.amend', 'submission', ?, ?, ?, ?)`,
			Args: []any{newID("aud"), record.ship.ProgramID, record.person.ID, record.person.Email, record.ship.ID,
				marshalMap(map[string]any{"status": record.ship.Status, "version": record.ship.Version}),
				marshalMap(map[string]any{"status": statusDecided, "decision": record.request.Decision, "hours": record.hours, "supersedes": finalID, "reason": record.overrideReason}), record.now}},
	}

	if record.overrideReason != "" {
		statements = append(statements, storage.TransactionStatement{
			SQL:  `insert into review_override (review_id, reason, created_at) values (?, ?, ?)`,
			Args: []any{record.reviewID, record.overrideReason, record.now},
		})
	}

	for _, findingID := range record.findingIDs {
		statements = append(statements, storage.TransactionStatement{
			SQL:  `insert or ignore into finding_return (review_id, finding_id, submission_id, version, created_at) values (?, ?, ?, ?, ?)`,
			Args: []any{record.reviewID, findingID, record.ship.ID, record.ship.Version, record.now},
		})
	}

	if !record.secondPass {
		for _, key := range record.request.Acknowledgements {
			statements = append(statements, storage.TransactionStatement{
				SQL: `insert or ignore into check_ack (id, submission_id, version, check_key, reviewer_id, reason, created_at)
					values (?, ?, ?, ?, ?, '', ?)`,
				Args: []any{newID("ack"), record.ship.ID, record.ship.Version, key, record.person.ID, record.now},
			})
		}
	}

	var outboxID string

	if a.outboundEnabled(ctx, record.ship.ProgramID) {
		message, statement, err := a.outboxStore().PrepareTransaction(outbox.Message{
			Event:     record.event,
			DedupeKey: "decision:" + record.reviewID,
			Payload:   record.payload,
			URL:       a.outboundURL(record.ship.ProgramID),
			Metadata:  map[string]string{"program_id": record.ship.ProgramID, "submission_id": record.ship.ID},
		})
		if err != nil {
			return nil, "", &decisionProblem{status: http.StatusInternalServerError, message: "could not queue the delivery"}
		}

		outboxID = message.ID

		statements = append(statements, statement)
	}

	return statements, outboxID, nil
}
