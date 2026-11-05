package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/outbox"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// handleDecision records a stage 1 decision, applies the hold policy, writes the outbox row and returns the next ship to open.
func (a *reviewApp) handleDecision(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewerWrite(w, r)
	if !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	var request contract.DecisionRequest
	if !app.ReadJSON(w, r, maxAPIBody, "decision", &request) {
		return
	}

	if existing, found, err := a.findReview(r.Context(), ship.ID, request.IdempotencyKey); err != nil {
		writeStorage(w, err)

		return
	} else if found {
		a.writeDecisionAgain(w, r, ship, existing)
		return
	}

	secondPass, checks, ok := a.decisionGuard(w, r, ship, person, request)
	if !ok {
		return
	}

	if problem := approvalProblem(request, checks, secondPass); problem != nil {
		problem.write(w)
		return
	}

	note := strings.TrimSpace(request.NoteToMaker)
	policy := a.policyFor(r.Context(), ship.ProgramID)

	if problem := noteProblem(request, note, policy); problem != nil {
		problem.write(w)
		return
	}

	hours := ship.HoursClaimed
	supersedes, heldDecision, heldHours := "", "", 0

	if secondPass {
		base, problem := a.secondPassBase(r.Context(), ship, person, request, note)
		if problem != nil {
			problem.write(w)

			return
		}

		if base.hours > 0 {
			hours = base.hours
		}

		note, supersedes, heldDecision, heldHours = base.note, base.supersedes, base.heldDecision, base.heldHours
	}

	hours, overrideReason, problem := resolveHours(request, hours, secondPass, heldDecision, heldHours)
	if problem != nil {
		problem.write(w)
		return
	}

	held, reason := decisionHold(request, secondPass, ship, policy, checks, hours)

	now := a.nowText()
	reviewID := newID("rev")
	status, stage, submissionStage := decisionState(ship, held)

	payloadJSON, err := a.decisionPayloadJSON(r.Context(), ship, person, checks, request, hours, note, overrideReason, stage, now)
	if err != nil {
		writeStorage(w, err)

		return
	}

	event := outboundEventName(request.Decision)
	record := decisionRecord{
		ship: ship, person: person, request: request,
		reviewID: reviewID, status: status, stage: stage, submissionStage: submissionStage,
		hours: hours, note: note, reason: reason, overrideReason: overrideReason, supersedes: supersedes,
		held: held, secondPass: secondPass, event: event, payload: payloadJSON, now: now,
	}

	statements, outboxID, problem := a.decisionStatements(r.Context(), record)
	if problem != nil {
		problem.write(w)
		return
	}

	if _, err := a.store.Transaction(r.Context(), statements...); err != nil {
		// A concurrent retry can win the idempotency index; return the row it wrote.
		if existing, found, findErr := a.findReview(r.Context(), ship.ID, request.IdempotencyKey); findErr == nil && found {
			a.writeDecisionAgain(w, r, ship, existing)
			return
		}

		writeStorage(w, err)

		return
	}

	deliveryStatus := ""
	if outboxID != "" {
		deliveryStatus = a.deliverOutboxNow(r.Context(), outboxID)
	}

	nextStage := ship.Stage
	if held {
		nextStage = stageOne
	}

	nextID := a.nextQueueID(r.Context(), ship.ProgramID, ship.ID, nextStage)

	app.WriteJSON(w, http.StatusOK, contract.DecisionOutcome{
		ID:             reviewID,
		Status:         status,
		Held:           boolInt(held),
		HeldReason:     stringPtr(reason),
		OutboxID:       optionalID(outboxID),
		DeliveryStatus: stringPtr(deliveryStatus),
		NextID:         optionalID(nextID),
	})
}

// decisionRecord is one decision's inputs and stored values.
type decisionRecord struct {
	ship                                     submissionRow
	person                                   reviewer
	request                                  contract.DecisionRequest
	reviewID, status                         string
	stage, submissionStage, hours            int
	note, reason, overrideReason, supersedes string
	held, secondPass                         bool
	event                                    string
	payload                                  []byte
	now                                      string
}

// decisionStatements builds the review, submission update, audit, acknowledgement and outbox writes.
func (a *reviewApp) decisionStatements(ctx context.Context, record decisionRecord) ([]storage.TransactionStatement, string, *decisionProblem) {
	statements := []storage.TransactionStatement{
		{SQL: `insert into review (id, submission_id, version, stage, reviewer_id, reviewer_email, reviewer_name, reviewer_role,
				decision, note_to_maker, internal_note, hours_approved_seconds, acknowledgements, policy_version, held, held_reason,
				supersedes_review_id, is_final, idempotency_key, created_at)
			values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{record.reviewID, record.ship.ID, record.ship.Version, record.stage, record.person.ID, record.person.Email, record.person.Name, record.person.Role,
				record.request.Decision, record.note, strings.TrimSpace(record.request.InternalNote), record.hours, marshalStrings(record.request.Acknowledgements),
				a.policyVersion(ctx, record.ship.ProgramID), boolInt(record.held), nullString(record.reason),
				nullString(record.supersedes), boolInt(!record.held), record.request.IdempotencyKey, record.now}},
		{SQL: `update submission set status = ?, stage = ?, hours_approved_seconds = ?, held_reason = ?,
				decided_at = ?, updated_at = ? where id = ?`,
			Args: []any{record.status, record.submissionStage, record.hours, nullString(record.reason), nullString(record.now), record.now, record.ship.ID}},
		{SQL: `delete from draft where submission_id = ?`, Args: []any{record.ship.ID}},
		{SQL: `delete from claim where submission_id = ?`, Args: []any{record.ship.ID}},
		{SQL: `insert into submission_event (id, submission_id, kind, actor_id, actor_email, data, created_at) values (?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{newID("evt"), record.ship.ID, eventKind(record.held), record.person.ID, record.person.Email, marshalMap(map[string]any{"decision": record.request.Decision, "held": record.held}), record.now}},
		{SQL: `insert into audit_log (id, program_id, actor_id, actor_email, action, target_type, target_id, before_json, after_json, created_at)
			values (?, ?, ?, ?, 'review.decision', 'submission', ?, ?, ?, ?)`,
			Args: []any{newID("aud"), record.ship.ProgramID, record.person.ID, record.person.Email, record.ship.ID,
				marshalMap(map[string]any{"status": record.ship.Status, "version": record.ship.Version}),
				marshalMap(map[string]any{"status": record.status, "decision": record.request.Decision, "held": record.held, "hours": record.hours}), record.now}},
	}

	if record.overrideReason != "" {
		statements = append(statements, storage.TransactionStatement{
			SQL:  `insert into review_override (review_id, reason, created_at) values (?, ?, ?)`,
			Args: []any{record.reviewID, record.overrideReason, record.now},
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

	outboxID := ""

	if !record.held && a.outboundEnabled(ctx, record.ship.ProgramID) {
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

// decisionHold applies the policy only to a stage 1 approval; changes and rejections are final.
func decisionHold(request contract.DecisionRequest, secondPass bool, ship submissionRow, policy reviewPolicy, checks []contract.CheckResult, hours int) (bool, string) {
	if request.Decision != decisionApproved || secondPass {
		return false, ""
	}

	return holdFor(ship, policy, checks, hours)
}

// decisionState returns the review status, review stage and submission stage for a decision.
func decisionState(ship submissionRow, held bool) (string, int, int) {
	if held {
		return statusStage2Hold, ship.Stage, 2
	}

	return statusDecided, ship.Stage, ship.Stage
}

// decisionPayloadJSON builds the exact event bytes for the outbox.
func (a *reviewApp) decisionPayloadJSON(ctx context.Context, ship submissionRow, person reviewer, checks []contract.CheckResult, request contract.DecisionRequest, hours int, note, overrideReason string, stage int, now string) ([]byte, error) {
	history := a.stageHistory(ctx, ship.ID, person, request.Decision, now, stage)
	payload := a.outboundPayload(ship, person, checks, request.Decision, hours, note, strings.TrimSpace(request.InternalNote), overrideReason, now, history, payloadHackatimeProjects(ctx, a.store, ship.ID, ship.Version))

	return json.Marshal(payload)
}

// decisionProblem is a validation failure with the response shape the handler would write.
type decisionProblem struct {
	status      int
	code, field string
	message     string
}

// write sends the problem with the most specific error shape.
func (p decisionProblem) write(w http.ResponseWriter) {
	switch {
	case p.field != "":
		app.WriteErrorField(w, p.status, p.code, p.field, p.message)
	case p.code != "":
		app.WriteErrorCode(w, p.status, p.code, p.message)
	default:
		app.WriteError(w, p.status, p.message)
	}
}

// approvalProblem rejects an approval that misses acknowledgements or has a blocking check.
func approvalProblem(request contract.DecisionRequest, checks []contract.CheckResult, secondPass bool) *decisionProblem {
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

// noteProblem enforces note length and the programme's note requirement.
func noteProblem(request contract.DecisionRequest, note string, policy reviewPolicy) *decisionProblem {
	if len(note) > maxNote || len(request.InternalNote) > maxNote {
		return &decisionProblem{status: http.StatusUnprocessableEntity, code: "invalid", field: "noteToMaker", message: "notes are limited to 4000 characters"}
	}

	if request.Decision != decisionApproved && policy.NoteRequiredForChanges && note == "" {
		return &decisionProblem{status: http.StatusUnprocessableEntity, code: "note_required", field: "noteToMaker", message: "a note to the maker is required for changes or rejection"}
	}

	return nil
}

// decisionGuard validates role, stage, version and claim, and loads the current checks.
func (a *reviewApp) decisionGuard(w http.ResponseWriter, r *http.Request, ship submissionRow, person reviewer, request contract.DecisionRequest) (bool, []contract.CheckResult, bool) {
	if ship.Status == statusChecking {
		app.WriteError(w, http.StatusConflict, "checks are still running")
		return false, nil, false
	}

	// A held ship is finished by an organiser at stage 2. A stage 1 ship is decided by a reviewer at stage 1.
	secondPass := ship.Stage == 2 || ship.Status == statusStage2Hold
	if secondPass && !canReviewSecondPass(person.Role) {
		app.WriteError(w, http.StatusForbidden, "only organisers can decide the second pass")
		return false, nil, false
	}

	if !secondPass && ship.Stage != stageOne {
		app.WriteError(w, http.StatusConflict, "this ship is not on the first pass")
		return false, nil, false
	}

	if !isOpen(ship.Status) {
		app.WriteError(w, http.StatusConflict, "this ship is no longer open")
		return false, nil, false
	}

	if request.Version != ship.Version {
		app.WriteErrorCode(w, http.StatusConflict, "stale_version", "this ship changed since you opened it; reload before deciding")
		return false, nil, false
	}

	if !a.holdsClaim(r.Context(), ship.ID, person.ID) {
		app.WriteError(w, http.StatusConflict, "claim the ship before deciding")
		return false, nil, false
	}

	checks, err := a.latestChecks(r.Context(), ship.ID, ship.Version)
	if err != nil {
		writeStorage(w, err)

		return false, nil, false
	}

	return secondPass, checks, true
}

// resolveHours applies the requested hours and requires an override reason when they change the first pass.
func resolveHours(request contract.DecisionRequest, hours int, secondPass bool, heldDecision string, heldHours int) (int, string, *decisionProblem) {
	if request.HoursApprovedSeconds != nil {
		hours = *request.HoursApprovedSeconds
	}

	if hours < 0 || hours > maxHours {
		return hours, "", &decisionProblem{status: http.StatusUnprocessableEntity, code: "invalid", field: "hoursApprovedSeconds", message: "approved hours must be between 0 and 30 days"}
	}

	overrideReason := strings.TrimSpace(stringValue(request.OverrideReason))
	if len(overrideReason) > maxNote {
		return hours, "", &decisionProblem{status: http.StatusUnprocessableEntity, code: "invalid", field: "overrideReason", message: "the override reason is limited to 4000 characters"}
	}

	if secondPass && heldDecision != "" && (hours != heldHours || request.Decision != heldDecision) && overrideReason == "" {
		return hours, "", &decisionProblem{status: http.StatusUnprocessableEntity, code: "override_reason_required", message: "explain why the second pass changes the first-pass decision"}
	}

	return hours, overrideReason, nil
}

// secondPassBase is the first-pass decision a second pass starts from.
type secondPassBase struct {
	hours, heldHours               int
	note, supersedes, heldDecision string
}

// secondPassBase loads the held review and rejects a decision on the organiser's own first pass.
func (a *reviewApp) secondPassBase(ctx context.Context, ship submissionRow, person reviewer, request contract.DecisionRequest, note string) (secondPassBase, *decisionProblem) {
	held, found, err := a.heldReview(ctx, ship.ID)
	if err != nil {
		app.Errorf("held review: %v", err)

		return secondPassBase{}, &decisionProblem{status: http.StatusInternalServerError, message: "could not read the first-pass review"}
	}

	if !found {
		return secondPassBase{}, &decisionProblem{status: http.StatusConflict, message: "no first-pass review to confirm"}
	}

	if held.ReviewerID == person.ID {
		return secondPassBase{}, &decisionProblem{status: http.StatusUnprocessableEntity, code: "own_decision", message: "another organiser must confirm your first-pass decision"}
	}

	base := secondPassBase{hours: held.Hours, note: note, supersedes: held.ID, heldDecision: held.Decision, heldHours: held.Hours}

	if request.Decision == decisionApproved && note == "" {
		base.note = held.NoteToMaker
	}

	return base, nil
}

// writeDecisionAgain returns the row an idempotent retry already wrote.
func (a *reviewApp) writeDecisionAgain(w http.ResponseWriter, r *http.Request, ship submissionRow, review reviewRow) {
	outboxID, status := a.outboxForReview(r.Context(), review.ID)

	nextStage := ship.Stage
	if review.Held == 1 {
		nextStage = stageOne
	}

	nextID := a.nextQueueID(r.Context(), ship.ProgramID, ship.ID, nextStage)

	app.WriteJSON(w, http.StatusOK, contract.DecisionOutcome{
		ID:             review.ID,
		Status:         ship.Status,
		Held:           review.Held,
		HeldReason:     optionalString(review.HeldReason),
		OutboxID:       optionalID(outboxID),
		DeliveryStatus: stringPtr(status),
		NextID:         optionalID(nextID),
	})
}

// reviewRow is the subset of a stored decision needed to replay a response.
type reviewRow struct {
	ID                                   string
	Held                                 int
	HeldReason, Decision                 string
	Hours                                int
	NoteToMaker, ReviewerName, CreatedAt string
}

// heldReview is the stage 1 review waiting for a second pass.
type heldReview struct {
	ID, ReviewerID, Decision string
	Hours                    int
	NoteToMaker              string
}

// heldReview reads the latest held, non-invalidated first-pass review for a ship.
func (a *reviewApp) heldReview(ctx context.Context, submissionID string) (heldReview, bool, error) {
	var row heldReview

	err := a.store.QueryRow(ctx, `select id, coalesce(reviewer_id, ''), decision, coalesce(hours_approved_seconds, 0), note_to_maker
		from review where submission_id = ? and held = 1 and invalidated_at is null
		order by created_at desc limit 1`, submissionID).Scan(&row.ID, &row.ReviewerID, &row.Decision, &row.Hours, &row.NoteToMaker)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			return heldReview{}, false, nil
		}

		return heldReview{}, false, err
	}

	return row, true, nil
}

// findReview returns the review a submission already has for this idempotency key.
func (a *reviewApp) findReview(ctx context.Context, submissionID, key string) (reviewRow, bool, error) {
	if strings.TrimSpace(key) == "" {
		return reviewRow{}, false, nil
	}

	var row reviewRow

	err := a.store.QueryRow(ctx, `select id, held, coalesce(held_reason, ''), decision, coalesce(hours_approved_seconds, 0), note_to_maker, coalesce(reviewer_name, ''), created_at
		from review where submission_id = ? and idempotency_key = ?`, submissionID, key).
		Scan(&row.ID, &row.Held, &row.HeldReason, &row.Decision, &row.Hours, &row.NoteToMaker, &row.ReviewerName, &row.CreatedAt)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			return reviewRow{}, false, nil
		}

		return reviewRow{}, false, err
	}

	return row, true, nil
}

// outboxForReview returns the delivery id and status for a review, or empty strings.
func (a *reviewApp) outboxForReview(ctx context.Context, reviewID string) (string, string) {
	var id, status string
	if err := a.store.QueryRow(ctx, `select id, status from webhook_outbox where dedupe_key = ?`, "decision:"+reviewID).Scan(&id, &status); err != nil {
		return "", ""
	}

	return id, status
}

// holdFor applies the stage 2 hold policy. Fraud is always stage 2, never stage 1.
func holdFor(ship submissionRow, policy reviewPolicy, checks []contract.CheckResult, hours int) (bool, string) {
	if policy.HoldOnWarn && slices.ContainsFunc(checks, isWarning) {
		return true, "Approved with warnings; held for the second pass."
	}

	if policy.HoldMinApprovedSeconds > 0 && hours >= policy.HoldMinApprovedSeconds {
		return true, "Approved hours are over the second-pass threshold."
	}

	if ship.RequestedTier > 0 && slices.Contains(policy.HoldTiers, ship.RequestedTier) {
		return true, "Tier " + strconv.Itoa(ship.RequestedTier) + " approvals are held for the second pass."
	}

	if ship.HoursClaimed > 0 && hours > ship.HoursClaimed {
		return true, "The approved hours are above the claim; held for the second pass."
	}

	return false, ""
}

// nextQueueID finds the next open ship for auto-advance in the current stage.
func (a *reviewApp) nextQueueID(ctx context.Context, programID, currentID string, stage int) string {
	var (
		id  string
		err error
	)

	if stage == 2 {
		err = a.store.QueryRow(ctx, `select id from submission
			where program_id = ? and id != ? and stage = 2 and status = 'stage2_hold'
			order by priority desc, ingested_at asc limit 1`, programID, currentID).Scan(&id)
	} else {
		err = a.store.QueryRow(ctx, `select id from submission
			where program_id = ? and id != ? and stage = 1 and status in ('ready','needs_attention')
			order by priority desc, ingested_at asc limit 1`, programID, currentID).Scan(&id)
	}

	if err != nil {
		return ""
	}

	return id
}

// eventKind is the timeline kind for the decision.
func eventKind(held bool) string {
	if held {
		return "held"
	}

	return "decided"
}

// policyVersion reads the programme's current policy version for the review snapshot.
func (a *reviewApp) policyVersion(ctx context.Context, programID string) int {
	var version int
	if err := a.store.QueryRow(ctx, `select coalesce(policy_version, 1) from program where id = ?`, programID).Scan(&version); err != nil {
		return 1
	}

	return version
}

// outboundURL reads the programme's delivery URL, falling back to the in-process receiver.
func (a *reviewApp) outboundURL(programID string) string {
	program, err := a.programByID(context.Background(), programID)
	if err == nil && program.OutboundURL != "" {
		return program.OutboundURL
	}

	if a.config.OutboundURL != "" {
		return a.config.OutboundURL
	}

	return "fake://receiver"
}

// optionalString returns nil for an empty string.
func optionalString(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}

// optionalID returns nil for an empty id.
func optionalID(value string) *string {
	return optionalString(value)
}

// unmarshalStrings decodes a stored string slice, returning an empty list for a missing value.
func unmarshalStrings(raw string) []string {
	out := []string{}
	if raw == "" {
		return out
	}

	_ = json.Unmarshal([]byte(raw), &out)

	return out
}

// marshalStrings encodes a string slice for storage.
func marshalStrings(values []string) string {
	data, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}

	return string(data)
}

// marshalMap encodes a small audit map for storage, returning an empty object on failure.
func marshalMap(value map[string]any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}

	return string(data)
}
