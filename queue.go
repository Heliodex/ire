package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

const makerAvatarBase = "https://cachet.dunkirk.sh/users"

// handleQueue returns the stage 1 work list, counts and claim state in one response.
//
//nolint:funlen // The query, row mapping and check join belong together.
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

	if stage == 2 && !canReviewSecondPass(person.Role) {
		app.WriteError(w, http.StatusForbidden, "only organisers can view the second pass")
		return
	}

	filter := strings.TrimSpace(r.URL.Query().Get("filter"))
	if filter == "" {
		filter = queueFilterAll
	}

	now := a.nowText()
	query := `select s.id, s.external_id, s.version, s.status, s.stage, s.priority, s.title,
			coalesce(s.theme, ''), coalesce(s.thumbnail_url, ''), coalesce(s.week, 0), coalesce(s.maker_name, ''), coalesce(s.maker_slack_id, ''), s.hours_claimed_seconds,
			coalesce(s.check_summary, ''), s.ingested_at, coalesce(u.email, ''),
			coalesce(r.status, ''), coalesce(r.attempts, 0), coalesce(r.started_at, ''), coalesce(r.finished_at, ''),
			coalesce(r.error, ''), coalesce(r.runner_version, ''), coalesce(prev_review.reviewer_email, '')
		from submission s
		left join claim c on c.submission_id = s.id and c.expires_at > ?
		left join user u on u.id = c.reviewer_id
		left join check_run r on r.submission_id = s.id and r.version = s.version
		left join review prev_review on prev_review.id = (
			select pr.id from review pr
			where pr.submission_id = s.previous_submission_id and pr.stage = 1 and pr.is_final = 1 and pr.invalidated_at is null
			order by pr.created_at desc, pr.id desc limit 1
		)
		where s.program_id = ? and s.stage = ?
		  and s.status in ('checking','ready','claimed','in_review','needs_attention','stage2_hold')`

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

	order := ` order by s.priority desc, s.ingested_at asc, s.id asc`
	if stage == stageOne {
		// The previous reviewer keeps a resubmission first, and other reviewers only see a fresh one behind the rest of the queue.
		order = ` order by s.priority desc,
			case
				when prev_review.reviewer_id = ? then 0
				when prev_review.reviewer_id is not null and s.ingested_at > ? then 2
				else 1
			end asc, s.ingested_at asc, s.id asc`

		args = append(args, person.ID, a.later(-reviewerHold))
	}

	query += order + ` limit ? offset ?`

	limit := parseLimit(r)
	args = append(args, limit+1, parseOffset(r))

	rows, err := a.store.Query(r.Context(), query, args...)
	if err != nil {
		writeStorage(w, err)

		return
	}
	defer rows.Close()

	response := contract.QueueResponse{
		Counts:  contract.QueueCounts{Checking: 0, Ready: 0, Claimed: 0, NeedsAttention: 0, Stage2Hold: 0},
		HasMore: false,
		Items:   []contract.QueueItem{},
	}

	for rows.Next() {
		var (
			item                                                     contract.QueueItem
			theme, thumbnail, makerName, makerSlack                  string
			summaryJSON, claimedBy                                   string
			runStatus, runStarted, runFinished, runError, runVersion string
			previousReviewer                                         string
			week, runAttempts                                        int
		)
		if err := rows.Scan(&item.ID, &item.ExternalID, &item.Version, &item.Status, &item.Stage, &item.Priority,
			&item.Title, &theme, &thumbnail, &week, &makerName, &makerSlack, &item.HoursClaimedSeconds, &summaryJSON, &item.IngestedAt, &claimedBy,
			&runStatus, &runAttempts, &runStarted, &runFinished, &runError, &runVersion, &previousReviewer); err != nil {
			writeStorage(w, err)

			return
		}

		if theme != "" {
			item.Theme = &theme
		}

		if thumbnail != "" {
			item.ThumbnailURL = &thumbnail
		}

		if week > 0 {
			item.Week = &week
		}

		if makerName == "" {
			makerName = "Unknown maker"
		}

		item.MakerName = makerName

		if makerSlack != "" {
			avatar := makerAvatarBase + "/" + url.PathEscape(makerSlack) + "/r"
			item.MakerAvatarURL = &avatar
		}

		item.CheckSummary = parseCheckSummary(summaryJSON)
		if claimedBy != "" {
			item.ClaimedBy = &claimedBy
		}

		if previousReviewer != "" {
			item.PreviousReviewer = &previousReviewer
		}

		if runStatus != "" {
			item.CheckRun = &contract.CheckRun{
				Status:        runStatus,
				Attempts:      runAttempts,
				StartedAt:     runStarted,
				FinishedAt:    optionalText(runFinished),
				Error:         optionalText(runError),
				RunnerVersion: runVersion,
			}
		}

		response.Items = append(response.Items, item)
	}

	if len(response.Items) > limit {
		response.Items = response.Items[:limit]
		response.HasMore = true
	}

	checks, err := a.queueChecks(r.Context(), response.Items)
	if err != nil {
		writeStorage(w, err)

		return
	}

	for i := range response.Items {
		response.Items[i].Checks = checks[response.Items[i].ID]
	}

	counts, err := a.queueCounts(r.Context(), person.ProgramID)
	if err == nil {
		if !canReviewSecondPass(person.Role) {
			counts.Stage2Hold = 0
		}

		response.Counts = counts
	}

	app.WriteJSON(w, http.StatusOK, response)
}

// queueChecks returns the latest result per check key for every queued ship, in group order, so the queue can draw one dot per check.
func (a *reviewApp) queueChecks(ctx context.Context, items []contract.QueueItem) (map[string][]contract.QueueCheck, error) {
	out := make(map[string][]contract.QueueCheck, len(items))
	if len(items) == 0 {
		return out, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(items)), ",")
	args := make([]any, len(items))

	for i := range items {
		args[i] = items[i].ID
		out[items[i].ID] = []contract.QueueCheck{}
	}

	rows, err := a.store.Query(ctx, `select c.submission_id, c.check_key, c.label, c.group_key, c.status, c.severity
		from check_result c
		join submission s on s.id = c.submission_id
		where c.submission_id in (`+placeholders+`) and c.version = s.version
		  and c.checked_at = (select max(c2.checked_at) from check_result c2
			where c2.submission_id = c.submission_id and c2.version = c.version and c2.check_key = c.check_key)
		order by c.submission_id, c.group_key, c.check_key`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			submissionID string
			check        contract.QueueCheck
		)

		if err := rows.Scan(&submissionID, &check.Key, &check.Label, &check.Group, &check.Status, &check.Severity); err != nil {
			return nil, err
		}

		out[submissionID] = append(out[submissionID], check)
	}

	return out, rows.Err()
}

// queueCounts computes the programme-wide badges, independent of the stage being viewed.
func (a *reviewApp) queueCounts(ctx context.Context, programID string) (contract.QueueCounts, error) {
	var counts contract.QueueCounts

	err := a.store.QueryRow(ctx, `select
			coalesce(sum(case when s.status = 'checking' then 1 else 0 end), 0),
			coalesce(sum(case when s.status = 'ready' then 1 else 0 end), 0),
			coalesce(sum(case when s.status in ('claimed','in_review') then 1 else 0 end), 0),
			coalesce(sum(case when s.status = 'needs_attention' then 1 else 0 end), 0),
			coalesce(sum(case when s.status = 'stage2_hold' then 1 else 0 end), 0)
		from submission s
		where s.program_id = ?
		  and s.status in ('checking','ready','claimed','in_review','needs_attention','stage2_hold')`, programID).
		Scan(&counts.Checking, &counts.Ready, &counts.Claimed, &counts.NeedsAttention, &counts.Stage2Hold)

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
	person, ok := a.requireReviewerWrite(w, r)
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

	if ship.Status == statusChecking {
		app.WriteError(w, http.StatusConflict, "checks are still running")
		return
	}

	secondPass := ship.Stage == 2 || ship.Status == statusStage2Hold
	if secondPass && !canReviewSecondPass(person.Role) {
		app.WriteError(w, http.StatusForbidden, "only organisers can claim a second-pass ship")
		return
	}

	if !secondPass && ship.Stage != stageOne {
		app.WriteError(w, http.StatusConflict, "this ship is not on the first pass")
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
		where claim.expires_at <= ? or claim.reviewer_id = ?`,
		ship.ID, person.ID, ship.Stage, expires, now, now, now, person.ID)
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
	person, ok := a.requireReviewerWrite(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	now := a.nowText()

	if _, ok := a.loadShipForReview(w, r); !ok {
		return
	}

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
	person, ok := a.requireReviewerWrite(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	now := a.nowText()

	if _, ok := a.loadShipForReview(w, r); !ok {
		return
	}

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
	person, ok := a.requireReviewerWrite(w, r)
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
