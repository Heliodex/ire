package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

const makerAvatarBase = "https://cachet.dunkirk.sh/users"

const (
	queueJournalCountSQL   = `(select count(*) from journal_entry je where je.submission_id = s.id)`
	queueJournalTextLenSQL = `(select coalesce(sum(length(coalesce(text, '')) + length(coalesce(markdown, ''))), 0) from journal_entry je where je.submission_id = s.id)`
	queueJournalImageSQL   = `(select coalesce(sum(json_array_length(coalesce(media, '[]'))), 0) from journal_entry je where je.submission_id = s.id)`
	queueCheckFailsSQL     = `(coalesce(json_extract(s.check_summary, '$.fail'), 0) + coalesce(json_extract(s.check_summary, '$.error'), 0))`
	queueEasySQL           = `s.status = 'ready' and coalesce(json_extract(s.check_summary, '$.overall'), '') = 'pass' and coalesce(s.thumbnail_url, '') <> '' and ` + queueJournalCountSQL + ` >= 2 and (s.hours_journal_seconds >= 900 or ` + queueJournalTextLenSQL + ` >= 400) and (` + queueJournalImageSQL + ` >= 1 or ` + queueJournalCountSQL + ` >= 3 or s.hours_journal_seconds >= 3600 or ` + queueJournalTextLenSQL + ` >= 1500)`
	queueHardSQL           = `(s.status = 'needs_attention' or ` + queueJournalCountSQL + ` = 0 or (` + queueJournalCountSQL + ` <= 1 and ` + queueJournalTextLenSQL + ` < 120) or ` + queueCheckFailsSQL + ` >= 4)`
	queueMediumSQL         = `not (` + queueHardSQL + `) and not (` + queueEasySQL + `)`
)

// queueSortColumns maps the queue's sortable headers to their ordering, so the sort parameter only selects a key. The checks score mirrors the client's old badness order: blocking results first, then warnings, then clean ships.
var queueSortColumns = map[string]string{
	"ship":    "s.title collate nocase",
	"maker":   "coalesce(s.maker_name, '') collate nocase",
	"status":  "s.status",
	"type":    "coalesce(s.theme, '') collate nocase",
	"version": "s.version",
	"claimed": "s.hours_claimed_seconds",
	"age":     "coalesce(s.submitted_at, s.created_at)",
	"checks":  "(coalesce(json_extract(nullif(coalesce(s.check_summary, ''), ''), '$.fail'), 0) + coalesce(json_extract(nullif(coalesce(s.check_summary, ''), ''), '$.error'), 0)) * 100 + coalesce(json_extract(nullif(coalesce(s.check_summary, ''), ''), '$.warn'), 0) * 10 - coalesce(json_extract(nullif(coalesce(s.check_summary, ''), ''), '$.pass'), 0)",
}

// queueParams is the validated queue listing request.
type queueParams struct {
	stage      int
	filter     string
	sortColumn string
	sortDir    string
	search     queueSearch
	station    string
}

// parseQueueParams validates the listing query, writing the error for a bad stage, sort or role.
func (a *ireApp) parseQueueParams(w http.ResponseWriter, r *http.Request, person reviewer) (queueParams, bool) {
	var params queueParams

	params.stage = stageOne

	if value := strings.TrimSpace(r.URL.Query().Get("stage")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > 2 {
			app.WriteErrorField(w, http.StatusBadRequest, "invalid", "stage", "stage must be 0, 1 or 2")

			return queueParams{}, false
		}

		params.stage = parsed
	}

	if params.stage == 2 && !person.Has(permSecondPass) {
		app.WriteError(w, http.StatusForbidden, "only organisers can view the 2nd pass")

		return queueParams{}, false
	}

	params.filter = strings.TrimSpace(r.URL.Query().Get("filter"))
	if params.filter == "" {
		params.filter = queueFilterAll
	}

	sortKey := strings.TrimSpace(r.URL.Query().Get("sort"))
	params.sortDir = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("dir")))

	if params.sortDir == "" {
		params.sortDir = "asc"
	}

	if params.sortDir != "asc" && params.sortDir != "desc" {
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "dir", "dir must be asc or desc")

		return queueParams{}, false
	}

	if sortKey != "" {
		column, ok := queueSortColumns[sortKey]
		if !ok {
			app.WriteErrorField(w, http.StatusBadRequest, "invalid", "sort", "sort must be ship, maker, status, type, version, claimed, age or checks")

			return queueParams{}, false
		}

		params.sortColumn = column
	}

	params.search = parseQueueSearch(r.URL.Query().Get("q"))

	return params, true
}

// queueListQuery builds the listing query for one page, keeping the resubmission ordering for stage 1.
// queueListWhere builds the shared from/where fragment behind queue listings and
// their filtered totals, so both count the same ships.
func (a *ireApp) queueListWhere(params queueParams, person reviewer) (string, []any) {
	now := a.nowText()
	query := `from submission s
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

	args := []any{now, person.ProgramID, params.stage}

	if params.station != "" {
		query += ` and ` + currentStationSQL() + ` = ? and s.status not in ('checking','stage2_hold')`

		args = append(args, params.station)
	}

	switch params.filter {
	case queueFilterEasy:
		query += ` and ` + queueEasySQL
	case queueFilterFast:
		query += ` and s.status = 'ready' and coalesce(json_extract(s.check_summary, '$.overall'), '') = 'pass'`
	case queueFilterHard:
		query += ` and ` + queueHardSQL
	case queueFilterMedium:
		query += ` and ` + queueMediumSQL
	case queueFilterAttention:
		query += ` and s.status = 'needs_attention'`
	case queueFilterMine:
		query += ` and c.reviewer_id = ?`

		args = append(args, person.ID)
	case queueFilterAll:
	}

	return params.search.apply(query, args)
}

func (a *ireApp) queueListQuery(params queueParams, person reviewer, limit, offset int) (string, []any) {
	query, args := a.queueListWhere(params, person)
	query = `select s.id, s.external_id, s.version, s.status, s.stage, s.priority, s.title,
			coalesce(s.theme, ''), coalesce(s.thumbnail_url, ''), coalesce(s.week, 0), coalesce(s.maker_name, ''), coalesce(s.maker_email, ''), coalesce(s.maker_slack_id, ''), coalesce(s.requested_tier, ''), s.hours_claimed_seconds,
			s.hours_journal_seconds, s.hours_hackatime_seconds, ` + queueJournalCountSQL + `, ` + queueJournalImageSQL + `, ` + queueJournalTextLenSQL + `, s.bom_total_cents,
			coalesce(s.check_summary, ''), s.ingested_at, coalesce(s.submitted_at, s.created_at), coalesce(u.email, ''),
			coalesce(r.status, ''), coalesce(r.attempts, 0), coalesce(r.started_at, ''), coalesce(r.finished_at, ''),
			coalesce(r.error, ''), coalesce(r.runner_version, ''), coalesce(prev_review.reviewer_email, '')
		` + query

	order := ` order by s.priority desc, coalesce(s.submitted_at, s.created_at) asc, s.id asc`
	if params.stage == stageOne && params.sortColumn == "" {
		// The previous reviewer keeps a resubmission first, and other reviewers only see a fresh one behind the rest of the queue.
		order = ` order by s.priority desc,
			case
				when prev_review.reviewer_id = ? then 0
				when prev_review.reviewer_id is not null and s.ingested_at > ? then 2
				else 1
			end asc, s.ingested_at asc, s.id asc`

		args = append(args, person.ID, a.later(-reviewerHold))
	}

	if params.sortColumn != "" {
		// An explicit sort spans pages, so appended rows keep arriving at the end.
		order = ` order by ` + params.sortColumn + ` ` + params.sortDir + `, s.id asc`
	}

	query += order + ` limit ? offset ?`

	args = append(args, limit+1, offset)

	return query, args
}

// queueFilteredCount counts the ships one listing query would return, so a
// filtered queue can show its share of the whole queue.
func (a *ireApp) queueFilteredCount(ctx context.Context, person reviewer, params queueParams) (int, error) {
	where, args := a.queueListWhere(params, person)

	var total int
	if err := a.store.QueryRow(ctx, `select count(*) `+where, args...).Scan(&total); err != nil {
		return 0, err
	}

	return total, nil
}

// scanQueueItem reads one listing row into a queue item.
func scanQueueItem(rows *storage.Rows) (contract.QueueItem, error) {
	var (
		item                                                     contract.QueueItem
		theme, thumbnail, makerName, makerSlack, tier            string
		summaryJSON, claimedBy                                   string
		runStatus, runStarted, runFinished, runError, runVersion string
		previousReviewer                                         string
		week, runAttempts                                        int
	)
	if err := rows.Scan(&item.ID, &item.ExternalID, &item.Version, &item.Status, &item.Stage, &item.Priority,
		&item.Title, &theme, &thumbnail, &week, &makerName, &item.MakerEmail, &makerSlack, &tier, &item.HoursClaimedSeconds,
		&item.HoursJournalSeconds, &item.HoursHackatimeSeconds, &item.JournalCount, &item.JournalImageCount, &item.JournalTextLength, &item.BomTotalCents,
		&summaryJSON, &item.IngestedAt, &item.SubmittedAt, &claimedBy,
		&runStatus, &runAttempts, &runStarted, &runFinished, &runError, &runVersion, &previousReviewer); err != nil {
		return contract.QueueItem{}, err
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
		item.MakerSlackID = &makerSlack

		avatar := makerAvatarBase + "/" + url.PathEscape(makerSlack) + "/r"
		item.MakerAvatarURL = &avatar
	}

	if tier != "" {
		item.RequestedTier = &tier
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

	return item, nil
}

// queueCheckSummary counts the check rows the queue draws, so the displayed counts always agree with the dots and the review page even when a stored summary lagged behind a late check run.
func queueCheckSummary(checks []contract.QueueCheck) contract.CheckSummary {
	results := make([]contract.CheckResult, len(checks))
	for i, check := range checks {
		results[i] = contract.CheckResult{Status: check.Status, Severity: check.Severity}
	}

	counts := checkCountsFor(results)

	return contract.CheckSummary{
		Overall: counts.Overall,
		Pass:    counts.Pass,
		Warn:    counts.Warn,
		Fail:    counts.Fail,
		Error:   counts.Error,
		Running: counts.Running,
	}
}

// handleQueue returns the stage 1 work list, counts and claim state in one response.
func (a *ireApp) handleQueue(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	params, ok := a.parseQueueParams(w, r, person)
	if !ok {
		return
	}

	if r.URL.Query().Get("stream") == "1" {
		a.streamQueue(w, r, person, params)

		return
	}

	limit := parseLimit(r)

	query, args := a.queueListQuery(params, person, limit, parseOffset(r))

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
		item, err := scanQueueItem(rows)
		if err != nil {
			writeStorage(w, err)

			return
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
		response.Items[i].CheckSummary = queueCheckSummary(response.Items[i].Checks)
	}

	counts, err := a.queueCounts(r.Context(), person.ProgramID, person.ID, a.nowText())
	if err == nil {
		if !person.Has(permSecondPass) {
			counts.Stage2Hold = 0
		}

		response.Counts = counts
	}

	app.WriteJSON(w, http.StatusOK, response)
}

// queueChecks returns the latest result per check key for every queued ship, in group order, so the queue can draw one dot per check.
func (a *ireApp) queueChecks(ctx context.Context, items []contract.QueueItem) (map[string][]contract.QueueCheck, error) {
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
func (a *ireApp) queueCounts(ctx context.Context, programID, reviewerID, now string) (contract.QueueCounts, error) {
	var counts contract.QueueCounts

	err := a.store.QueryRow(ctx, `select
			coalesce(sum(case when s.status = 'checking' then 1 else 0 end), 0),
			coalesce(sum(case when s.status = 'ready' then 1 else 0 end), 0),
			coalesce(sum(case when s.status in ('claimed','in_review') then 1 else 0 end), 0),
			coalesce(sum(case when s.status = 'needs_attention' then 1 else 0 end), 0),
			coalesce(sum(case when s.status = 'stage2_hold' then 1 else 0 end), 0),
			coalesce(sum(case when `+queueEasySQL+` then 1 else 0 end), 0),
			coalesce(sum(case when s.status = 'ready' and coalesce(json_extract(s.check_summary, '$.overall'), '') = 'pass' then 1 else 0 end), 0),
			coalesce(sum(case when `+queueHardSQL+` then 1 else 0 end), 0),
			coalesce(sum(case when `+queueMediumSQL+` then 1 else 0 end), 0),
			coalesce(sum(case when c.reviewer_id = ? then 1 else 0 end), 0)
		from submission s
		left join claim c on c.submission_id = s.id and c.expires_at > ?
		where s.program_id = ?
		  and s.status in ('checking','ready','claimed','in_review','needs_attention','stage2_hold')`, reviewerID, now, programID).
		Scan(&counts.Checking, &counts.Ready, &counts.Claimed, &counts.NeedsAttention, &counts.Stage2Hold, &counts.Easy, &counts.Fast, &counts.Hard, &counts.Medium, &counts.Mine)

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
func (a *ireApp) handleClaim(w http.ResponseWriter, r *http.Request) {
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
	if secondPass && !person.Has(permSecondPass) {
		app.WriteError(w, http.StatusForbidden, "only organisers can claim a second-pass ship")
		return
	}

	if !secondPass && ship.Stage != stageOne {
		app.WriteError(w, http.StatusConflict, "this ship is not on the 1st pass")
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
func (a *ireApp) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
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
func (a *ireApp) handleRelease(w http.ResponseWriter, r *http.Request) {
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
func (a *ireApp) handleDraft(w http.ResponseWriter, r *http.Request) {
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
func (a *ireApp) handleBundle(w http.ResponseWriter, r *http.Request) {
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
func (a *ireApp) loadShipForReview(w http.ResponseWriter, r *http.Request) (submissionRow, bool) {
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
func (a *ireApp) holdsClaim(ctx context.Context, submissionID, reviewerID string) bool {
	var found string

	err := a.store.QueryRow(ctx, `select reviewer_id from claim where submission_id = ? and reviewer_id = ? and expires_at > ?`,
		submissionID, reviewerID, a.nowText()).Scan(&found)

	return err == nil && found == reviewerID
}
