package main

import (
	"context"
	"crypto/subtle"
	"encoding/json/v2"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// requireMachine authenticates a machine endpoint with the programme signing secret as a bearer token.
func (a *reviewApp) requireMachine(w http.ResponseWriter, r *http.Request) bool {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))

	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(a.config.IngestSecret)) != 1 {
		app.WriteError(w, http.StatusUnauthorized, "invalid bearer token")
		return false
	}

	return true
}

// handleStatus returns the Ari-compatible ship status, looked up by external id or ship id.
func (a *reviewApp) handleStatus(w http.ResponseWriter, r *http.Request) {
	program, ok := a.programFor(w, r)
	if !ok {
		return
	}

	if !a.requireMachine(w, r) {
		return
	}

	externalID := strings.TrimSpace(r.URL.Query().Get("external_id"))
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	version := 0

	if value := strings.TrimSpace(r.URL.Query().Get("version")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			app.WriteErrorField(w, http.StatusBadRequest, "invalid", "version", "version must be a positive integer")
			return
		}

		version = parsed
	}

	var (
		ship submissionRow
		err  error
	)

	switch {
	case id != "":
		ship, err = a.loadSubmission(r.Context(), id)
		if err == nil && ship.ProgramID != program.ID {
			err = errNotFound
		}
	case externalID != "":
		ship, err = a.loadSubmissionByExternalID(r.Context(), program.ID, externalID, version)
	default:
		app.WriteError(w, http.StatusBadRequest, "external_id or id is required")
		return
	}

	if err != nil {
		if errors.Is(err, storage.ErrNoRows) || errors.Is(err, errNotFound) {
			app.WriteError(w, http.StatusNotFound, "unknown ship")
			return
		}

		writeStorage(w, err)

		return
	}

	response, err := a.statusResponse(r.Context(), ship)
	if err != nil {
		writeStorage(w, err)

		return
	}

	// Ari's SDK requires the decision key to exist and be null for a ship that is not final, so send a map rather than a struct with omitempty.
	encoded, err := json.Marshal(response)
	if err != nil {
		writeStorage(w, err)

		return
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		writeStorage(w, err)

		return
	}

	if response.Decision == nil {
		payload["decision"] = nil
	}

	app.WriteJSON(w, http.StatusOK, payload)
}

// statusResponse assembles the Ari-compatible status body.
func (a *reviewApp) statusResponse(ctx context.Context, ship submissionRow) (contract.StatusResponse, error) {
	checks, err := a.latestChecks(ctx, ship.ID, ship.Version)
	if err != nil {
		return contract.StatusResponse{}, err
	}

	reviews, err := a.loadReviews(ctx, ship.ID)
	if err != nil {
		return contract.StatusResponse{}, err
	}

	events, err := a.loadEvents(ctx, ship.ID)
	if err != nil {
		return contract.StatusResponse{}, err
	}

	run, hasRun, err := a.loadCheckRun(ctx, ship.ID, ship.Version)
	if err != nil {
		return contract.StatusResponse{}, err
	}

	var checkRun *contract.CheckRun
	if hasRun {
		checkRun = &run
	}

	response := contract.StatusResponse{
		CheckRun:     checkRun,
		ID:           ship.ID,
		ExternalID:   ship.ExternalID,
		Version:      ship.Version,
		Phase:        ariPhase(ship.Status),
		Stage:        ship.Stage,
		StageHistory: []contract.StatusResponseStageHistoryElem{},
		Checks: contract.StatusResponseChecks{
			Overall: summaryOverall(ship.CheckSummary),
			Results: []contract.StatusResponseChecksResultsElem{},
		},
		Timeline:  []contract.StatusResponseTimelineElem{},
		UpdatedAt: ship.UpdatedAt,
	}

	for _, check := range checks {
		response.Checks.Results = append(response.Checks.Results, contract.StatusResponseChecksResultsElem{
			Key:       check.Key,
			Status:    check.Status,
			Summary:   check.Summary,
			CheckedAt: check.CheckedAt,
		})
	}

	for _, review := range slices.Backward(reviews) {
		if review.InvalidatedAt != nil {
			continue
		}

		response.StageHistory = append(response.StageHistory, contract.StatusResponseStageHistoryElem{
			Stage:    review.Stage,
			Decision: review.Decision,
			Reviewer: stringPtr(review.ReviewerEmail),
			At:       review.CreatedAt,
		})
	}

	for _, review := range reviews {
		if review.Held != 0 || review.InvalidatedAt != nil {
			continue
		}

		decision := review.Decision
		response.Decision = &decision
		response.Review = a.statusReview(ship, review)

		break
	}

	for _, event := range events {
		response.Timeline = append(response.Timeline, contract.StatusResponseTimelineElem{Kind: event.Kind, At: event.CreatedAt})
	}

	return response, nil
}

// statusReview converts a stored review into the review object Half Life reads.
func (a *reviewApp) statusReview(ship submissionRow, review contract.Review) *contract.StatusResponseReview {
	approved := ship.HoursApproved
	if review.HoursApprovedSeconds != nil {
		approved = *review.HoursApprovedSeconds
	}

	minutes := approved / 60
	hours := float64(approved) / secondsPerHour

	justification := &contract.StatusResponseReviewJustification{}

	if approved < ship.HoursClaimed {
		reason := "Reviewed down to " + formatHours(approved) + " from " + formatHours(ship.HoursClaimed) + "."
		justification.DeflationReason = &reason
	}

	return &contract.StatusResponseReview{
		ApprovedSeconds: approved,
		ApprovedMinutes: minutes,
		ApprovedHours:   hours,
		SecondsBreakdown: contract.StatusResponseReviewSecondsBreakdown{
			Hackatime: ship.HoursHackatim,
			Journals:  ship.HoursJournal,
			Lapse:     0,
			Program:   0,
		},
		NoteToMaker:   review.NoteToMaker,
		AuditNote:     review.InternalNote,
		Reviewer:      &contract.StatusResponseReviewReviewer{Email: stringPtr(review.ReviewerEmail)},
		Justification: justification,
	}
}

// ariPhase maps the internal status to the phase names Ari clients understand.
func ariPhase(status string) string {
	switch status {
	case statusChecking, statusNeedsAttention:
		return "processing"
	case statusReady, statusClaimed, statusInReview:
		return "review"
	case statusStage2Hold:
		return "second_pass"
	case statusDecided:
		return "reviewed"
	case statusWithdrawn:
		return "withdrawn"
	}

	return "processing"
}

// summaryOverall reads the overall field from a stored check summary.
func summaryOverall(summary string) string {
	var counts checkCounts
	if err := json.Unmarshal([]byte(summary), &counts); err != nil || counts.Overall == "" {
		return checkOverallPass
	}

	return counts.Overall
}

// formatHours renders seconds as a compact hour count.
func formatHours(seconds int) string {
	if seconds%3600 == 0 {
		return strconv.Itoa(seconds/secondsPerHour) + "h"
	}

	return strconv.FormatFloat(float64(seconds)/secondsPerHour, 'f', 2, 64) + "h"
}

// handleReviews returns final decisions since a cursor, as the exact outbound payloads. This is the recovery path when a webhook is lost.
func (a *reviewApp) handleReviews(w http.ResponseWriter, r *http.Request) {
	program, ok := a.programFor(w, r)
	if !ok {
		return
	}

	if !a.requireMachine(w, r) {
		return
	}

	since, ok := parseSince(r)
	if !ok {
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "since", "since must be an RFC 3339 timestamp")
		return
	}

	rows, err := a.store.Query(r.Context(), `select payload, created_at from webhook_outbox
		where json_extract(metadata, '$.program_id') = ? and event like 'review.%' and created_at > ?
		order by created_at asc limit ?`, program.ID, since, parseLimit(r))
	if err != nil {
		writeStorage(w, err)

		return
	}
	defer rows.Close()

	response := contract.ReviewsResponse{Reviews: []contract.ReviewEvent{}}
	cursor := since

	for rows.Next() {
		var payload, createdAt string
		if err := rows.Scan(&payload, &createdAt); err != nil {
			writeStorage(w, err)

			return
		}

		var event contract.ReviewEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}

		response.Reviews = append(response.Reviews, event)
		cursor = createdAt
	}

	if len(response.Reviews) > 0 {
		response.Cursor = &cursor
	}

	app.WriteJSON(w, http.StatusOK, response)
}

// handleEvents returns every transition since a cursor. It is the queue audit trail for integrations.
func (a *reviewApp) handleEvents(w http.ResponseWriter, r *http.Request) {
	program, ok := a.programFor(w, r)
	if !ok {
		return
	}

	if !a.requireMachine(w, r) {
		return
	}

	since, ok := parseSince(r)
	if !ok {
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "since", "since must be an RFC 3339 timestamp")
		return
	}

	rows, err := a.store.Query(r.Context(), `select e.id, e.submission_id, e.kind, coalesce(e.actor_email, ''), coalesce(e.data, ''), e.created_at
		from submission_event e join submission s on s.id = e.submission_id
		where s.program_id = ? and e.created_at > ?
		order by e.created_at asc, e.id asc limit ?`, program.ID, since, parseLimit(r))
	if err != nil {
		writeStorage(w, err)

		return
	}
	defer rows.Close()

	response := contract.EventsResponse{Events: []contract.EventsResponseEventsElem{}}

	for rows.Next() {
		var (
			item        contract.EventsResponseEventsElem
			actor, data string
		)
		if err := rows.Scan(&item.ID, &item.SubmissionID, &item.Kind, &actor, &data, &item.CreatedAt); err != nil {
			writeStorage(w, err)

			return
		}

		if actor != "" {
			item.ActorEmail = &actor
		}

		if data != "" {
			item.Data = &data
		}

		response.Events = append(response.Events, item)
	}

	app.WriteJSON(w, http.StatusOK, response)
}

// handleOutbox lists recent deliveries so an operator can see what was sent and why a retry is pending.
func (a *reviewApp) handleOutbox(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	records, err := a.outboxStore().List(r.Context(), a.store, parseLimit(r))
	if err != nil {
		writeStorage(w, err)

		return
	}

	response := contract.OutboxResponse{Deliveries: []contract.OutboxRow{}}

	for _, record := range records {
		if record.Metadata["program_id"] != person.ProgramID {
			continue
		}

		item := contract.OutboxRow{
			ID:        record.ID,
			Event:     record.Event,
			Status:    normalizeOutboxStatus(record.Status),
			Attempts:  record.Attempts,
			CreatedAt: record.CreatedAt.Format(timestampFormat),
		}

		if submissionID := record.Metadata["submission_id"]; submissionID != "" {
			item.SubmissionID = &submissionID
		}

		if record.LastHTTPStatus > 0 {
			lastHTTP := record.LastHTTPStatus
			item.LastHttpStatus = &lastHTTP
		}

		if record.LastError != "" {
			lastError := record.LastError
			item.LastError = &lastError
		}

		if record.DeliveredAt != nil {
			deliveredAt := record.DeliveredAt.Format(timestampFormat)
			item.DeliveredAt = &deliveredAt
		}

		if record.DeliveryID != "" {
			deliveryID := record.DeliveryID
			item.DeliveryID = &deliveryID
		}

		if len(record.Payload) > 0 {
			payload := string(record.Payload)
			item.Payload = &payload

			if person.Role == roleReviewer {
				item.Payload = redactOutboxPayload(payload)
			}
		}

		response.Deliveries = append(response.Deliveries, item)
	}

	app.WriteJSON(w, http.StatusOK, response)
}

// redactOutboxPayload hides the maker's address from reviewers reading delivery debug output, matching the bundle's redaction. Organisers and admins still see the exact bytes.
func redactOutboxPayload(payload string) *string {
	var event map[string]any
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return &payload
	}

	maker, ok := event["maker"].(map[string]any)
	if !ok {
		return &payload
	}

	email, ok := maker["email"].(string)
	if !ok || email == "" {
		return &payload
	}

	maker["email"] = redactEmail(email)

	redacted, err := json.Marshal(event)
	if err != nil {
		return &payload
	}

	value := string(redacted)

	return &value
}

// handleFakeDeliveries lists what the prototype's in-process receiver accepted, with the signature it verified.
func (a *reviewApp) handleFakeDeliveries(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentReviewer(w, r); !ok {
		return
	}

	rows, err := a.store.Query(r.Context(), `select id, delivery_id, event, timestamp, body, signature, verified, received_at
		from fake_receiver_delivery order by received_at desc limit ?`, parseLimit(r))
	if err != nil {
		writeStorage(w, err)

		return
	}
	defer rows.Close()

	response := contract.FakeDeliveriesResponse{Deliveries: []contract.FakeDelivery{}}

	for rows.Next() {
		var item contract.FakeDelivery
		if err := rows.Scan(&item.ID, &item.DeliveryID, &item.Event, &item.Timestamp, &item.Body, &item.Signature, &item.Verified, &item.ReceivedAt); err != nil {
			writeStorage(w, err)

			return
		}

		response.Deliveries = append(response.Deliveries, item)
	}

	app.WriteJSON(w, http.StatusOK, response)
}
