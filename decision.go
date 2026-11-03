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

	"github.com/heliodex/better-deploy/wt/packages/app"
	"github.com/heliodex/better-deploy/wt/packages/storage"
)

// handleDecision records a stage 1 decision, applies the hold policy, writes the outbox row and returns the next ship to open.
//
//nolint:funlen,maintidx,gocognit,gocyclo // The decision transaction reads best as one unit; splitting it would hide the ordering.
func (a *reviewApp) handleDecision(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
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

	if ship.Status == statusChecking {
		app.WriteError(w, http.StatusConflict, "checks are still running")
		return
	}

	if existing, found, err := a.findReview(r.Context(), ship.ID, request.IdempotencyKey); err != nil {
		writeStorage(w, err)

		return
	} else if found {
		a.writeDecisionAgain(w, r, ship, existing)
		return
	}

	// A held ship is finished by an organiser at stage 2. A stage 1 ship is decided by a reviewer at stage 1.
	secondPass := ship.Stage == 2 || ship.Status == statusStage2Hold
	if secondPass && !canReviewSecondPass(person.Role) {
		app.WriteError(w, http.StatusForbidden, "only organisers can decide the second pass")
		return
	}

	if !secondPass && ship.Stage != stageOne {
		app.WriteError(w, http.StatusConflict, "this ship is not on the first pass")
		return
	}

	if !isOpen(ship.Status) {
		app.WriteError(w, http.StatusConflict, "this ship is no longer open")
		return
	}

	if request.Version != ship.Version {
		app.WriteErrorCode(w, http.StatusConflict, "stale_version", "this ship changed since you opened it; reload before deciding")
		return
	}

	if !a.holdsClaim(r.Context(), ship.ID, person.ID) {
		app.WriteError(w, http.StatusConflict, "claim the ship before deciding")
		return
	}

	checks, err := a.latestChecks(r.Context(), ship.ID, ship.Version)
	if err != nil {
		writeStorage(w, err)

		return
	}

	if request.Decision == decisionApproved {
		if !secondPass {
			if missing := missingAcknowledgements(checks, request.Acknowledgements); len(missing) > 0 {
				app.WriteErrorField(w, http.StatusUnprocessableEntity, "acknowledgement_required", "acknowledgements",
					"acknowledge the warnings before approving: "+strings.Join(missing, ", "))

				return
			}
		}

		if blocked := blockedChecks(checks); len(blocked) > 0 {
			app.WriteErrorField(w, http.StatusUnprocessableEntity, "check_blocked", "decision",
				"approval is blocked: "+blocked[0].Summary+". Ask the maker to resubmit or reject with a note.")

			return
		}
	}

	note := strings.TrimSpace(request.NoteToMaker)
	if len(note) > maxNote || len(request.InternalNote) > maxNote {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "noteToMaker", "notes are limited to 4000 characters")
		return
	}

	policy := a.policyFor(r.Context(), ship.ProgramID)
	if request.Decision != decisionApproved && policy.NoteRequiredForChanges && note == "" {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "note_required", "noteToMaker", "a note to the maker is required for changes or rejection")
		return
	}

	hours := ship.HoursClaimed
	supersedes := ""

	if secondPass {
		held, found, heldErr := a.heldReview(r.Context(), ship.ID)
		if heldErr != nil {
			writeStorage(w, heldErr)

			return
		}

		if !found {
			app.WriteError(w, http.StatusConflict, "no first-pass review to confirm")
			return
		}

		if held.ReviewerID == person.ID {
			app.WriteErrorCode(w, http.StatusUnprocessableEntity, "own_decision", "another organiser must confirm your first-pass decision")
			return
		}

		if held.Hours > 0 {
			hours = held.Hours
		}

		if request.Decision == decisionApproved && note == "" {
			note = held.NoteToMaker
		}

		supersedes = held.ID
	}

	if request.HoursApprovedSeconds != nil {
		hours = *request.HoursApprovedSeconds
	}

	if hours < 0 || hours > 30*24*3600 {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "hoursApprovedSeconds", "approved hours must be between 0 and 30 days")
		return
	}

	// Only an approval can be held. Changes and rejections are final outcomes that Half Life must receive; holding them would silently drop the decision.
	held, reason := false, ""
	if request.Decision == decisionApproved && !secondPass {
		held, reason = holdFor(ship, policy, checks, hours)
	}

	now := a.nowText()
	reviewID := newID("rev")
	status := statusDecided
	stage := ship.Stage
	submissionStage := stage

	if held {
		status = statusStage2Hold
		submissionStage = 2
	}

	stageHistory := a.stageHistory(r.Context(), ship.ID, person, request.Decision, now, stage)
	payload := a.outboundPayload(ship, person, checks, request.Decision, hours, note, strings.TrimSpace(request.InternalNote), now, stageHistory, payloadHackatimeProjects(r.Context(), a.store, ship.ID, ship.Version))

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		writeStorage(w, err)

		return
	}

	event := outboundEventName(request.Decision)
	statements := []storage.TransactionStatement{
		{SQL: `insert into review (id, submission_id, version, stage, reviewer_id, reviewer_email, reviewer_name, reviewer_role,
				decision, note_to_maker, internal_note, hours_approved_seconds, acknowledgements, policy_version, held, held_reason,
				supersedes_review_id, is_final, idempotency_key, created_at)
			values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{reviewID, ship.ID, ship.Version, stage, person.ID, person.Email, person.Name, person.Role,
				request.Decision, note, strings.TrimSpace(request.InternalNote), hours, marshalStrings(request.Acknowledgements),
				a.policyVersion(r.Context(), ship.ProgramID), boolInt(held), nullString(reason), nullString(supersedes), boolInt(!held), request.IdempotencyKey, now}},
		{SQL: `update submission set status = ?, stage = ?, hours_approved_seconds = ?, held_reason = ?,
				decided_at = ?, updated_at = ? where id = ?`,
			Args: []any{status, submissionStage, hours, nullString(reason), nullString(now), now, ship.ID}},
		{SQL: `delete from draft where submission_id = ?`, Args: []any{ship.ID}},
		{SQL: `delete from claim where submission_id = ?`, Args: []any{ship.ID}},
		{SQL: `insert into submission_event (id, submission_id, kind, actor_id, actor_email, data, created_at) values (?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{newID("evt"), ship.ID, eventKind(held), person.ID, person.Email, marshalMap(map[string]any{"decision": request.Decision, "held": held}), now}},
		{SQL: `insert into audit_log (id, program_id, actor_id, actor_email, action, target_type, target_id, before_json, after_json, created_at)
			values (?, ?, ?, ?, 'review.decision', 'submission', ?, ?, ?, ?)`,
			Args: []any{newID("aud"), ship.ProgramID, person.ID, person.Email, ship.ID,
				marshalMap(map[string]any{"status": ship.Status, "version": ship.Version}),
				marshalMap(map[string]any{"status": status, "decision": request.Decision, "held": held, "hours": hours}), now}},
	}

	if !secondPass {
		for _, key := range request.Acknowledgements {
			statements = append(statements, storage.TransactionStatement{
				SQL: `insert or ignore into check_ack (id, submission_id, version, check_key, reviewer_id, reason, created_at)
					values (?, ?, ?, ?, ?, '', ?)`,
				Args: []any{newID("ack"), ship.ID, ship.Version, key, person.ID, now},
			})
		}
	}

	outboxID := ""
	if !held {
		outboxID = newID("out")
		statements = append(statements, storage.TransactionStatement{
			SQL: `insert into outbox (id, program_id, submission_id, event, dedupe_key, payload, url, status, attempts, next_attempt_at, delivery_id, created_at)
				values (?, ?, ?, ?, ?, ?, ?, 'pending', 0, ?, ?, ?)`,
			Args: []any{outboxID, ship.ProgramID, ship.ID, event, "decision:" + reviewID, string(payloadJSON), a.outboundURL(ship.ProgramID), now, newID("del"), now},
		})
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
		deliveryStatus = a.deliverOutboxByID(r.Context(), outboxID)
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
	ID           string
	Held         int
	HeldReason   string
	Decision     string
	Hours        int
	NoteToMaker  string
	ReviewerName string
	CreatedAt    string
}

// heldReview is the stage 1 review waiting for a second pass.
type heldReview struct {
	ID          string
	ReviewerID  string
	Hours       int
	NoteToMaker string
}

// heldReview reads the latest held, non-invalidated first-pass review for a ship.
func (a *reviewApp) heldReview(ctx context.Context, submissionID string) (heldReview, bool, error) {
	var row heldReview

	err := a.store.QueryRow(ctx, `select id, coalesce(reviewer_id, ''), coalesce(hours_approved_seconds, 0), note_to_maker
		from review where submission_id = ? and held = 1 and invalidated_at is null
		order by created_at desc limit 1`, submissionID).Scan(&row.ID, &row.ReviewerID, &row.Hours, &row.NoteToMaker)
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
	if err := a.store.QueryRow(ctx, `select id, status from outbox where dedupe_key = ?`, "decision:"+reviewID).Scan(&id, &status); err != nil {
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

// outboundEventName maps a decision to the Ari event name.
func outboundEventName(decision string) string {
	switch decision {
	case decisionApproved:
		return "review.approved"
	case decisionChanges:
		return "review.changes"
	default:
		return "review.rejected"
	}
}

// eventKind is the timeline kind for the decision.
func eventKind(held bool) string {
	if held {
		return "held"
	}

	return "decided"
}

// outboundStage is one stage in the outbound history.
type outboundStage struct {
	Stage    int    `json:"stage"`
	Decision string `json:"decision"`
	Reviewer string `json:"reviewer"`
	At       string `json:"at"`
}

// outboundMaker is the maker identity Half Life expects.
type outboundMaker struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	SlackID string `json:"slack_id"`
}

// outboundShip is the immutable ship snapshot in the event.
type outboundShip struct {
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Track             string   `json:"track"`
	RepoURL           string   `json:"repo_url"`
	DemoURL           string   `json:"demo_url"`
	ThumbnailURL      string   `json:"thumbnail_url"`
	HackatimeProjects []string `json:"hackatime_projects"`
}

// outboundBreakdown is Ari's seconds breakdown.
type outboundBreakdown struct {
	Hackatime int `json:"hackatime"`
	Journals  int `json:"journals"`
	Lapse     int `json:"lapse"`
	Program   int `json:"program"`
}

// outboundReviewer is the actor on a decision.
type outboundReviewer struct {
	Email   string `json:"email"`
	SlackID string `json:"slack_id"`
}

// outboundJustification matches the fields Half Life's mirror writes back to Airtable.
type outboundJustification struct {
	HackatimeProjects string `json:"hackatime_projects,omitempty"`
	TechnicalFeatures string `json:"technical_features,omitempty"`
	DeflationReason   string `json:"deflation_reason,omitempty"`
}

// outboundReview is the review object Half Life reads.
type outboundReview struct {
	ApprovedSeconds  int                   `json:"approved_seconds"`
	ApprovedMinutes  int                   `json:"approved_minutes"`
	ApprovedHours    float64               `json:"approved_hours"`
	SecondsBreakdown outboundBreakdown     `json:"seconds_breakdown"`
	NoteToMaker      string                `json:"note_to_maker"`
	AuditNote        string                `json:"audit_note"`
	Reviewer         outboundReviewer      `json:"reviewer"`
	ReviewerEmail    string                `json:"reviewerEmail"`
	Justification    outboundJustification `json:"justification"`
}

// outboundCheck is one check summary in the event.
type outboundCheck struct {
	Key     string `json:"key"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

// outboundChecks is the check block in the event.
type outboundChecks struct {
	Overall string          `json:"overall"`
	Results []outboundCheck `json:"results"`
}

// outboundEvent is the exact webhook body. created_at is additive so the recovery feed carries a cursor-able time.
type outboundEvent struct {
	Event        string          `json:"event"`
	ID           string          `json:"id"`
	ExternalID   string          `json:"external_id"`
	Decision     string          `json:"decision"`
	Stage        int             `json:"stage"`
	StageHistory []outboundStage `json:"stage_history"`
	Maker        outboundMaker   `json:"maker"`
	Ship         outboundShip    `json:"ship"`
	Review       outboundReview  `json:"review"`
	Checks       outboundChecks  `json:"checks"`
	CreatedAt    string          `json:"createdAt"`
}

// outboundPayload builds the final event. A held decision never reaches here because no outbox row is written.
func (a *reviewApp) outboundPayload(ship submissionRow, person reviewer, checks []contract.CheckResult, decision string, hours int, note, internalNote, now string, history []outboundStage, hackatimeProjects []string) outboundEvent {
	results := make([]outboundCheck, 0, len(checks))
	for _, check := range checks {
		results = append(results, outboundCheck{Key: check.Key, Status: check.Status, Summary: check.Summary})
	}

	justification := outboundJustification{}
	if hours < ship.HoursClaimed {
		justification.DeflationReason = "Reviewed down to " + formatHours(hours) + " from " + formatHours(ship.HoursClaimed) + "."
	}

	return outboundEvent{
		Event:        outboundEventName(decision),
		ID:           ship.ID,
		ExternalID:   ship.ExternalID,
		Decision:     decision,
		Stage:        ship.Stage,
		StageHistory: history,
		Maker:        outboundMaker{Email: ship.MakerEmail, Name: ship.MakerName, SlackID: ship.MakerSlack},
		Ship: outboundShip{
			Title:             ship.Title,
			Description:       ship.Description,
			Track:             ship.Track,
			RepoURL:           ship.RepoURL,
			DemoURL:           ship.DemoURL,
			ThumbnailURL:      ship.ThumbnailURL,
			HackatimeProjects: hackatimeProjects,
		},
		Review: outboundReview{
			ApprovedSeconds: hours,
			ApprovedMinutes: hours / 60,
			ApprovedHours:   float64(hours) / secondsPerHour,
			SecondsBreakdown: outboundBreakdown{
				Hackatime: ship.HoursHackatim,
				Journals:  ship.HoursJournal,
			},
			NoteToMaker:   note,
			AuditNote:     internalNote,
			Reviewer:      outboundReviewer{Email: person.Email},
			ReviewerEmail: person.Email,
			Justification: justification,
		},
		Checks:    outboundChecks{Overall: summaryOverall(ship.CheckSummary), Results: results},
		CreatedAt: now,
	}
}

// stageHistory builds the stage list including the decision being made.
func (a *reviewApp) stageHistory(ctx context.Context, submissionID string, person reviewer, decision, now string, stage int) []outboundStage {
	out := []outboundStage{}

	reviews, err := a.loadReviews(ctx, submissionID)
	if err == nil {
		for _, review := range slices.Backward(reviews) {
			out = append(out, outboundStage{Stage: review.Stage, Decision: review.Decision, Reviewer: review.ReviewerEmail, At: review.CreatedAt})
		}
	}

	return append(out, outboundStage{Stage: stage, Decision: decision, Reviewer: person.Email, At: now})
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
