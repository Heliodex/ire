package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/app"
	"github.com/heliodex/better-deploy/wt/packages/storage"
)

// handleQueue returns the stage 1 work list, counts and claim state in one response.
func (a *reviewApp) handleQueue(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	stage := stageOne

	if value := strings.TrimSpace(r.URL.Query().Get("stage")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > 2 {
			app.WriteErrorField(w, http.StatusBadRequest, "invalid", "stage", "stage must be 0, 1 or 2")
			return
		}

		stage = parsed
	}

	filter := strings.TrimSpace(r.URL.Query().Get("filter"))
	if filter == "" {
		filter = queueFilterAll
	}

	now := a.nowText()
	query := `select s.id, s.external_id, s.version, s.status, s.stage, s.priority, s.title,
			coalesce(s.theme, ''), coalesce(s.week, 0), coalesce(s.maker_name, ''), s.hours_claimed_seconds,
			coalesce(s.check_summary, ''), s.ingested_at, coalesce(u.email, '')
		from submission s
		left join claim c on c.submission_id = s.id and c.expires_at > ?
		left join user u on u.id = c.reviewer_id
		where s.program_id = ? and s.stage = ?
		  and s.status in ('ready','claimed','in_review','needs_attention','stage2_hold')`

	args := []any{now, person.ProgramID, stage}

	switch filter {
	case queueFilterFast:
		query += ` and s.status = 'ready' and s.check_summary like '%"overall":"pass"%'`
	case queueFilterAttention:
		query += ` and s.status = 'needs_attention'`
	case queueFilterMine:
		query += ` and c.reviewer_id = ?`

		args = append(args, person.ID)
	case queueFilterAll:
	}

	query += ` order by s.priority desc, s.ingested_at asc, s.id asc limit ?`

	args = append(args, defaultLimit)

	rows, err := a.store.Query(r.Context(), query, args...)
	if err != nil {
		writeStorage(w, err)

		return
	}
	defer rows.Close()

	response := contract.QueueResponse{
		Counts: contract.QueueCounts{Ready: 0, Claimed: 0, NeedsAttention: 0, Stage2Hold: 0},
		Items:  []contract.QueueItem{},
	}

	for rows.Next() {
		var (
			item                                     contract.QueueItem
			theme, makerName, summaryJSON, claimedBy string
			week                                     int
		)
		if err := rows.Scan(&item.ID, &item.ExternalID, &item.Version, &item.Status, &item.Stage, &item.Priority,
			&item.Title, &theme, &week, &makerName, &item.HoursClaimedSeconds, &summaryJSON, &item.IngestedAt, &claimedBy); err != nil {
			writeStorage(w, err)

			return
		}

		if theme != "" {
			item.Theme = &theme
		}

		if week > 0 {
			item.Week = &week
		}

		if makerName == "" {
			makerName = "Unknown maker"
		}

		item.MakerName = makerName

		item.CheckSummary = parseCheckSummary(summaryJSON)
		if claimedBy != "" {
			item.ClaimedBy = &claimedBy
		}

		response.Items = append(response.Items, item)
	}

	counts, err := a.queueCounts(r.Context(), person.ProgramID, stage, now)
	if err == nil {
		response.Counts = counts
	}

	app.WriteJSON(w, http.StatusOK, response)
}

// queueCounts computes the four badges in one aggregate query.
func (a *reviewApp) queueCounts(ctx context.Context, programID string, stage int, now string) (contract.QueueCounts, error) {
	var counts contract.QueueCounts

	err := a.store.QueryRow(ctx, `select
			coalesce(sum(case when s.status = 'ready' then 1 else 0 end), 0),
			coalesce(sum(case when s.status in ('claimed','in_review') then 1 else 0 end), 0),
			coalesce(sum(case when s.status = 'needs_attention' then 1 else 0 end), 0),
			coalesce(sum(case when s.status = 'stage2_hold' then 1 else 0 end), 0)
		from submission s
		left join claim c on c.submission_id = s.id and c.expires_at > ?
		where s.program_id = ? and s.stage = ?
		  and s.status in ('ready','claimed','in_review','needs_attention','stage2_hold')`, now, programID, stage).
		Scan(&counts.Ready, &counts.Claimed, &counts.NeedsAttention, &counts.Stage2Hold)

	return counts, err
}

// parseCheckSummary decodes the stored summary, defaulting to a pass with zero counts when it is missing.
func parseCheckSummary(summary string) contract.CheckSummary {
	var counts checkCounts

	if err := json.Unmarshal([]byte(summary), &counts); err != nil || counts.Overall == "" {
		return contract.CheckSummary{Overall: checkOverallPass}
	}

	return contract.CheckSummary{
		Overall: counts.Overall,
		Pass:    counts.Pass,
		Warn:    counts.Warn,
		Fail:    counts.Fail,
		Error:   counts.Error,
		Running: counts.Running,
	}
}

// handleClaim takes the stage 1 claim for a ship. It is one conditional write, so two reviewers cannot both win.
func (a *reviewApp) handleClaim(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	if !isOpen(ship.Status) {
		app.WriteError(w, http.StatusConflict, "this ship is no longer open")
		return
	}

	now := a.nowText()
	expires := a.later(claimTTL)

	result, err := a.store.Exec(r.Context(), `insert into claim (submission_id, reviewer_id, stage, expires_at, heartbeat_at, created_at)
		values (?, ?, ?, ?, ?, ?)
		on conflict(submission_id) do update set
			reviewer_id = excluded.reviewer_id,
			stage = excluded.stage,
			expires_at = excluded.expires_at,
			heartbeat_at = excluded.heartbeat_at
		where claim.expires_at <= ?`,
		ship.ID, person.ID, ship.Stage, expires, now, now, now)
	if err != nil {
		writeStorage(w, err)

		return
	}

	if result.RowsAffected == 0 {
		holder := a.claimFor(r.Context(), ship.ID, person)
		if holder == nil {
			app.WriteError(w, http.StatusConflict, "could not claim this ship")
			return
		}

		app.WriteErrorField(w, http.StatusConflict, "claim_taken", "claim", "another reviewer is working on this ship until "+holder.ExpiresAt)

		return
	}

	if ship.Status == statusReady {
		if _, err := a.store.Exec(r.Context(), `update submission set status = ?, first_opened_at = coalesce(first_opened_at, ?), updated_at = ? where id = ?`,
			statusClaimed, now, now, ship.ID); err != nil {
			writeStorage(w, err)

			return
		}
	}

	if _, err := a.store.Exec(r.Context(), `insert into submission_event (id, submission_id, kind, actor_id, actor_email, created_at) values (?, ?, 'claimed', ?, ?, ?)`,
		newID("evt"), ship.ID, person.ID, person.Email, now); err != nil {
		writeStorage(w, err)

		return
	}

	claim := a.claimFor(r.Context(), ship.ID, person)
	if claim == nil {
		app.WriteError(w, http.StatusInternalServerError, "claim was written but could not be read back")
		return
	}

	app.WriteJSON(w, http.StatusOK, contract.ClaimResponse{Claim: *claim})
}

// handleHeartbeat extends the caller's claim.
func (a *reviewApp) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	now := a.nowText()

	result, err := a.store.Exec(r.Context(), `update claim set expires_at = ?, heartbeat_at = ? where submission_id = ? and reviewer_id = ?`,
		a.later(claimTTL), now, id, person.ID)
	if err != nil {
		writeStorage(w, err)

		return
	}

	if result.RowsAffected == 0 {
		app.WriteError(w, http.StatusConflict, "you do not hold this claim")
		return
	}

	claim := a.claimFor(r.Context(), id, person)
	if claim == nil {
		app.WriteError(w, http.StatusInternalServerError, "claim disappeared")
		return
	}

	app.WriteJSON(w, http.StatusOK, contract.ClaimResponse{Claim: *claim})
}

// handleRelease gives a claimed ship back to the queue.
func (a *reviewApp) handleRelease(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	now := a.nowText()

	result, err := a.store.Exec(r.Context(), `delete from claim where submission_id = ? and reviewer_id = ?`, id, person.ID)
	if err != nil {
		writeStorage(w, err)

		return
	}

	if result.RowsAffected == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if _, err := a.store.Exec(r.Context(), `update submission set status = case when status in ('claimed','in_review') then 'ready' else status end, updated_at = ? where id = ?`, now, id); err != nil {
		writeStorage(w, err)

		return
	}

	if _, err := a.store.Exec(r.Context(), `insert into submission_event (id, submission_id, kind, actor_id, actor_email, created_at) values (?, ?, 'released', ?, ?, ?)`,
		newID("evt"), id, person.ID, person.Email, now); err != nil {
		writeStorage(w, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleDraft saves the reviewer's note and form state so a refresh does not lose typing.
func (a *reviewApp) handleDraft(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	if !a.holdsClaim(r.Context(), ship.ID, person.ID) {
		app.WriteError(w, http.StatusConflict, "claim the ship before drafting")
		return
	}

	var request contract.DraftRequest
	if !app.ReadJSON(w, r, maxAPIBody, "note", &request) {
		return
	}

	now := a.nowText()
	if _, err := a.store.Exec(r.Context(), `insert into draft (submission_id, reviewer_id, stage, note, internal_note, "values", checklist, adjustments, updated_at)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(submission_id, reviewer_id) do update set
			note = excluded.note, internal_note = excluded.internal_note, "values" = excluded."values",
			checklist = excluded.checklist, adjustments = excluded.adjustments, updated_at = excluded.updated_at`,
		ship.ID, person.ID, ship.Stage, request.Note, request.InternalNote, request.Values, request.Checklist, request.Adjustments, now); err != nil {
		writeStorage(w, err)

		return
	}

	app.WriteJSON(w, http.StatusOK, contract.DraftSaved{Saved: 1, UpdatedAt: now})
}

// handleBundle returns everything the review screen needs in one request.
func (a *reviewApp) handleBundle(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	bundle, err := a.reviewBundle(r.Context(), ship, person)
	if err != nil {
		writeStorage(w, err)

		return
	}

	if ship.FirstOpenedAt == "" {
		_, _ = a.store.Exec(r.Context(), `update submission set first_opened_at = ? where id = ?`, a.nowText(), ship.ID)
	}

	app.WriteJSON(w, http.StatusOK, bundle)
}

// reviewBundle assembles the seven indexed reads behind the review screen.
func (a *reviewApp) reviewBundle(ctx context.Context, ship submissionRow, person reviewer) (contract.ReviewBundle, error) {
	checks, err := a.latestChecks(ctx, ship.ID, ship.Version)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	journals, err := a.loadJournals(ctx, ship.ID)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	bom, err := a.loadBom(ctx, ship.ID)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	reviews, err := a.loadReviews(ctx, ship.ID)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	events, err := a.loadEvents(ctx, ship.ID)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	policy := a.policyFor(ctx, ship.ProgramID)
	claim := a.claimFor(ctx, ship.ID, person)

	makerEmail := ship.MakerEmail
	if person.Role == roleReviewer {
		makerEmail = redactEmail(makerEmail)
	}

	submission := contract.Submission{
		ID:                    ship.ID,
		ExternalID:            ship.ExternalID,
		Version:               ship.Version,
		Status:                ship.Status,
		Stage:                 ship.Stage,
		Title:                 ship.Title,
		Track:                 ship.Track,
		HoursClaimedSeconds:   ship.HoursClaimed,
		HoursJournalSeconds:   ship.HoursJournal,
		HoursHackatimeSeconds: ship.HoursHackatim,
		MakerEmail:            makerEmail,
		IngestedAt:            ship.IngestedAt,
		UpdatedAt:             ship.UpdatedAt,
	}

	if ship.Description != "" {
		submission.Description = &ship.Description
	}

	if ship.Theme != "" {
		submission.Theme = &ship.Theme
	}

	if ship.Phase != "" {
		submission.Phase = &ship.Phase
	}

	if ship.Week > 0 {
		submission.Week = &ship.Week
	}

	if ship.MakerName != "" {
		submission.MakerName = &ship.MakerName
	}

	if ship.RepoURL != "" {
		submission.RepoURL = &ship.RepoURL
	}

	if ship.DemoURL != "" {
		submission.DemoURL = &ship.DemoURL
	}

	if ship.ThumbnailURL != "" {
		submission.ThumbnailURL = &ship.ThumbnailURL
	}

	if ship.RequestedTier > 0 {
		submission.RequestedTier = &ship.RequestedTier
	}

	if ship.GrantCapCents > 0 {
		submission.GrantCapCents = &ship.GrantCapCents
	}

	if ship.BomTotalCents > 0 {
		submission.BomTotalCents = &ship.BomTotalCents
	}

	if ship.HoursApproved > 0 {
		submission.HoursApprovedSeconds = &ship.HoursApproved
	}

	if ship.HeldReason != "" {
		submission.HeldReason = &ship.HeldReason
	}

	if ship.DecidedAt != "" {
		submission.DecidedAt = &ship.DecidedAt
	}

	return contract.ReviewBundle{
		Submission:       submission,
		Checks:           checks,
		Journals:         journals,
		Bom:              bom,
		Reviews:          reviews,
		Events:           events,
		PriorReviews:     payloadPriorReviews(ctx, a.store, ship.ID, ship.Version),
		Claim:            claim,
		Acknowledgements: a.acknowledgementsFor(ctx, ship.ID, ship.Version, person.ID),
		Policy:           contractPolicy(policy),
	}, nil
}

// loadShipForReview loads the path's ship and checks that it belongs to the path's programme.
func (a *reviewApp) loadShipForReview(w http.ResponseWriter, r *http.Request) (submissionRow, bool) {
	program, ok := a.programFor(w, r)
	if !ok {
		return submissionRow{}, false
	}

	ship, err := a.loadSubmission(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			app.WriteError(w, http.StatusNotFound, "unknown submission")
			return submissionRow{}, false
		}

		writeStorage(w, err)

		return submissionRow{}, false
	}

	if ship.ProgramID != program.ID {
		app.WriteError(w, http.StatusNotFound, "unknown submission")
		return submissionRow{}, false
	}

	return ship, true
}

// holdsClaim reports whether this reviewer still holds the ship's claim.
func (a *reviewApp) holdsClaim(ctx context.Context, submissionID, reviewerID string) bool {
	var found string

	err := a.store.QueryRow(ctx, `select reviewer_id from claim where submission_id = ? and reviewer_id = ? and expires_at > ?`,
		submissionID, reviewerID, a.nowText()).Scan(&found)

	return err == nil && found == reviewerID
}
