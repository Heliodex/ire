package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"strconv"
	"strings"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// submissionRow is the flattened ship row every handler reads. Nullable columns are coalesced to zero values.
type submissionRow struct {
	ID, ProgramID, ExternalID                                                                             string
	Version                                                                                               int
	PreviousID, Status                                                                                    string
	Stage, Priority                                                                                       int
	HeldReason, Title, Description, Track, Theme, Phase                                                   string
	Week                                                                                                  int
	MakerEmail, MakerName, MakerSlack, MakerHackatim, RepoURL, DemoURL, ThumbnailURL                      string
	RequestedTier, GrantCapCents, BomTotalCents, HoursClaimed, HoursJournal, HoursHackatim, HoursApproved int
	CheckSummary, IngestedAt, FirstOpenedAt, DecidedAt, CreatedAt, UpdatedAt                              string
}

// loadSubmission reads one ship by id.
func (a *ireApp) loadSubmission(ctx context.Context, id string) (submissionRow, error) {
	var row submissionRow

	err := a.store.QueryRow(ctx, `select id, program_id, external_id, version, coalesce(previous_submission_id, ''),
			status, stage, priority, coalesce(held_reason, ''), title, coalesce(description, ''), track,
			coalesce(theme, ''), coalesce(phase, ''), coalesce(week, 0),
			maker_email, coalesce(maker_name, ''), coalesce(maker_slack_id, ''), coalesce(maker_hackatime_id, ''),
			coalesce(repo_url, ''), coalesce(demo_url, ''), coalesce(thumbnail_url, ''),
			coalesce(requested_tier, 0), coalesce(grant_cap_cents, 0), coalesce(bom_total_cents, 0),
			hours_claimed_seconds, hours_journal_seconds, hours_hackatime_seconds, coalesce(hours_approved_seconds, 0),
			coalesce(check_summary, ''), ingested_at, coalesce(first_opened_at, ''), coalesce(decided_at, ''), created_at, updated_at
		from submission where id = ?`, id).
		Scan(&row.ID, &row.ProgramID, &row.ExternalID, &row.Version, &row.PreviousID,
			&row.Status, &row.Stage, &row.Priority, &row.HeldReason, &row.Title, &row.Description, &row.Track,
			&row.Theme, &row.Phase, &row.Week,
			&row.MakerEmail, &row.MakerName, &row.MakerSlack, &row.MakerHackatim,
			&row.RepoURL, &row.DemoURL, &row.ThumbnailURL,
			&row.RequestedTier, &row.GrantCapCents, &row.BomTotalCents,
			&row.HoursClaimed, &row.HoursJournal, &row.HoursHackatim, &row.HoursApproved,
			&row.CheckSummary, &row.IngestedAt, &row.FirstOpenedAt, &row.DecidedAt, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		return submissionRow{}, err
	}

	return row, nil
}

// loadSubmissionByExternalID reads the latest version for a project/phase.
func (a *ireApp) loadSubmissionByExternalID(ctx context.Context, programID, externalID string, version int) (submissionRow, error) {
	var id string

	if version > 0 {
		err := a.store.QueryRow(ctx, `select id from submission where program_id = ? and external_id = ? and version = ?`, programID, externalID, version).Scan(&id)
		if err != nil {
			return submissionRow{}, err
		}
	} else {
		err := a.store.QueryRow(ctx, `select id from submission where program_id = ? and external_id = ? order by version desc limit 1`, programID, externalID).Scan(&id)
		if err != nil {
			return submissionRow{}, err
		}
	}

	return a.loadSubmission(ctx, id)
}

// latestChecks returns the newest result per check key for one version.
func (a *ireApp) latestChecks(ctx context.Context, submissionID string, version int) ([]contract.CheckResult, error) {
	rows, err := a.store.Query(ctx, `select check_key, label, group_key, status, severity, summary, coalesce(detail, ''), coalesce(data, ''), checked_at
		from check_result c
		where c.submission_id = ? and c.version = ?
		  and c.checked_at = (select max(c2.checked_at) from check_result c2
			where c2.submission_id = c.submission_id and c2.version = c.version and c2.check_key = c.check_key)
		order by group_key, check_key`, submissionID, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []contract.CheckResult{}

	for rows.Next() {
		var (
			item         contract.CheckResult
			detail, data string
		)
		if err := rows.Scan(&item.Key, &item.Label, &item.Group, &item.Status, &item.Severity, &item.Summary, &detail, &data, &item.CheckedAt); err != nil {
			return nil, err
		}

		item.Detail = stringPtr(detail)
		item.Source = sourceFromData(data)
		out = append(out, item)
	}

	return out, rows.Err()
}

// maxJournalMedia caps the images exposed per journal entry for review display.
const maxJournalMedia = 20

// maxJournalTimelapses caps the clips exposed per journal entry for review display.
const maxJournalTimelapses = 50

// clipSource identifies the ledger source for one timelapse clip.
func clipSource(entryID string, index int, externalID string) string {
	if strings.TrimSpace(externalID) != "" {
		return "clip:" + entryID + ":" + strings.TrimSpace(externalID)
	}

	return "clip:" + entryID + ":#" + strconv.Itoa(index)
}

// journalMediaURLs exposes an entry's image attachments for review display,
// keeping only http(s) image URLs so maker-supplied metadata cannot inject
// other schemes into the page.
func journalMediaURLs(raw string) []contract.JournalMedia {
	var records []struct {
		Type *string `json:"type"`
		URL  *string `json:"url"`
		Alt  *string `json:"alt"`
	}

	if json.Unmarshal([]byte(raw), &records) != nil {
		return []contract.JournalMedia{}
	}

	out := []contract.JournalMedia{}

	for _, record := range records {
		if record.URL == nil {
			continue
		}

		url := strings.TrimSpace(*record.URL)
		if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
			continue
		}

		if record.Type != nil && *record.Type != "" && *record.Type != "image" {
			continue
		}

		alt := ""
		if record.Alt != nil {
			alt = *record.Alt
		}

		out = append(out, contract.JournalMedia{URL: url, Alt: optionalText(alt)})

		if len(out) >= maxJournalMedia {
			break
		}
	}

	return out
}

// journalTimelapses exposes an entry's clips for review display, keeping only
// http(s) URLs and resolving each clip's approved time from the ledger cuts.
func journalTimelapses(entryID, raw string, adjustments []contract.TimeAdjustment) []contract.JournalTimelapse {
	var records []struct {
		CoveredSeconds *int    `json:"coveredSeconds"`
		ExternalID     *string `json:"externalId"`
		Provider       *string `json:"provider"`
		ThumbnailURL   *string `json:"thumbnailUrl"`
		URL            *string `json:"url"`
	}

	if json.Unmarshal([]byte(raw), &records) != nil {
		return []contract.JournalTimelapse{}
	}

	cuts := map[string]int{}
	infos := map[string]contract.TimeAdjustment{}

	for _, adjustment := range adjustments {
		if adjustment.Kind != "cut" {
			continue
		}

		cuts[adjustment.Source] += adjustment.Seconds

		if _, seen := infos[adjustment.Source]; !seen {
			infos[adjustment.Source] = adjustment
		}
	}

	out := []contract.JournalTimelapse{}

	for index, record := range records {
		if len(out) >= maxJournalTimelapses {
			break
		}

		covered := 0
		if record.CoveredSeconds != nil && *record.CoveredSeconds > 0 {
			covered = *record.CoveredSeconds
		}

		externalID := ""
		if record.ExternalID != nil {
			externalID = strings.TrimSpace(*record.ExternalID)
		}

		source := clipSource(entryID, index, externalID)

		approved := covered
		if cut, ok := cuts[source]; ok {
			approved = max(0, covered-cut)
		}

		item := contract.JournalTimelapse{CoveredSeconds: covered}
		if externalID != "" {
			item.ExternalID = &externalID
		}

		if record.Provider != nil && strings.TrimSpace(*record.Provider) != "" {
			provider := strings.TrimSpace(*record.Provider)
			item.Provider = &provider
		}

		if record.URL != nil {
			if url := strings.TrimSpace(*record.URL); strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
				item.URL = &url
			}
		}

		if record.ThumbnailURL != nil {
			if thumbnail := strings.TrimSpace(*record.ThumbnailURL); strings.HasPrefix(thumbnail, "https://") || strings.HasPrefix(thumbnail, "http://") {
				item.ThumbnailURL = &thumbnail
			}
		}

		if info, ok := infos[source]; ok && approved < covered {
			item.ApprovedSeconds = &approved
			item.Reason = &info.Reason
			item.Note = info.Note
		}

		out = append(out, item)
	}

	return out
}

// loadAdjustments returns the ledger cuts and bonuses behind the approved total.
func (a *ireApp) loadAdjustments(ctx context.Context, submissionID string) ([]contract.TimeAdjustment, error) {
	rows, err := a.store.Query(ctx, `select id, kind, seconds, reason, coalesce(note, ''), coalesce(reviewer_id, ''), created_at, coalesce(source, '')
		from time_adjustment where submission_id = ? order by created_at`, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []contract.TimeAdjustment{}

	for rows.Next() {
		var (
			item             contract.TimeAdjustment
			note, reviewerID string
		)

		if err := rows.Scan(&item.ID, &item.Kind, &item.Seconds, &item.Reason, &note, &reviewerID, &item.CreatedAt, &item.Source); err != nil {
			return nil, err
		}

		item.Note = optionalText(note)
		item.ReviewerID = optionalText(reviewerID)
		out = append(out, item)
	}

	return out, rows.Err()
}

// loadJournals returns journal entries with their media and clip counts.
func (a *ireApp) loadJournals(ctx context.Context, submissionID string) ([]contract.JournalEntry, error) {
	rows, err := a.store.Query(ctx, `select id, at, seconds, coalesce(title, ''), coalesce(text, ''), coalesce(markdown, ''),
			coalesce(media, '[]'), coalesce(timelapses, '[]'), coalesce(approved_seconds, -1), coalesce(reviewer_note, ''),
			coalesce(deflation_reason, '')
		from journal_entry where submission_id = ? order by sort, at`, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []contract.JournalEntry{}

	adjustments, err := a.loadAdjustments(ctx, submissionID)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var (
			item                                        contract.JournalEntry
			title, text, markdown, mediaJSON, clipsJSON string
			approvedSeconds                             int
			reviewerNote, deflationReason               string
		)
		if err := rows.Scan(&item.ID, &item.At, &item.Seconds, &title, &text, &markdown, &mediaJSON, &clipsJSON, &approvedSeconds, &reviewerNote, &deflationReason); err != nil {
			return nil, err
		}

		item.Title = stringPtr(title)
		item.Text = stringPtr(text)
		item.Markdown = stringPtr(markdown)
		item.Media = journalMediaURLs(mediaJSON)
		item.MediaCount = jsonArrayLength(mediaJSON)
		item.ClipCount = jsonArrayLength(clipsJSON)
		item.CoveredSeconds = jsonCoveredSeconds(clipsJSON)
		item.Timelapses = journalTimelapses(item.ID, clipsJSON, adjustments)

		if approvedSeconds >= 0 {
			item.ApprovedSeconds = &approvedSeconds
		}

		item.ReviewerNote = optionalText(reviewerNote)
		item.DeflationReason = optionalText(deflationReason)
		out = append(out, item)
	}

	return out, rows.Err()
}

// loadBom returns the priced parts list.
func (a *ireApp) loadBom(ctx context.Context, submissionID string) ([]contract.BomLine, error) {
	rows, err := a.store.Query(ctx, `select name, coalesce(purpose, ''), quantity, unit_cost_cents, coalesce(vendor, ''), coalesce(link, '')
		from bom_line where submission_id = ? order by sort`, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []contract.BomLine{}

	for rows.Next() {
		var (
			item                  contract.BomLine
			purpose, vendor, link string
		)
		if err := rows.Scan(&item.Name, &purpose, &item.Quantity, &item.UnitCostCents, &vendor, &link); err != nil {
			return nil, err
		}

		item.Purpose = stringPtr(purpose)
		item.Vendor = stringPtr(vendor)
		item.Link = stringPtr(link)
		out = append(out, item)
	}

	return out, rows.Err()
}

// loadReviews returns every review for a ship, newest first.
func (a *ireApp) loadReviews(ctx context.Context, submissionID string) ([]contract.Review, error) {
	rows, err := a.store.Query(ctx, `select review.id, review.stage, review.decision, review.note_to_maker, coalesce(review.internal_note, ''),
			coalesce(review.technical_features, ''), coalesce(review.deflation_reason, ''),
			coalesce(review.hours_approved_seconds, 0), review.reviewer_email, coalesce(review.reviewer_name, ''), review.held,
			coalesce(review.held_reason, ''), coalesce(review_override.reason, ''), coalesce(review.acknowledgements, ''),
			coalesce(review.invalidated_at, ''), review.created_at
		from review
		left join review_override on review_override.review_id = review.id
		where review.submission_id = ? order by review.created_at desc, review.stage desc, review.id desc`, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []contract.Review{}

	for rows.Next() {
		var (
			item                                               contract.Review
			internalNote, technicalFeatures, deflationReason   string
			reviewerName, heldReason, overrideReason, acksJSON string
			invalidatedAt                                      string
			hours                                              int
		)
		if err := rows.Scan(&item.ID, &item.Stage, &item.Decision, &item.NoteToMaker, &internalNote,
			&technicalFeatures, &deflationReason, &hours,
			&item.ReviewerEmail, &reviewerName, &item.Held, &heldReason, &overrideReason, &acksJSON, &invalidatedAt, &item.CreatedAt); err != nil {
			return nil, err
		}

		item.InternalNote = stringPtr(internalNote)
		item.TechnicalFeatures = optionalText(technicalFeatures)
		item.DeflationReason = optionalText(deflationReason)

		item.ReviewerName = stringPtr(reviewerName)
		item.HeldReason = optionalText(heldReason)
		item.OverrideReason = optionalText(overrideReason)
		item.InvalidatedAt = optionalText(invalidatedAt)
		item.Acknowledgements = unmarshalStrings(acksJSON)

		if hours > 0 {
			item.HoursApprovedSeconds = &hours
		}

		out = append(out, item)
	}

	return out, rows.Err()
}

// loadEvents returns the timeline for a ship, oldest first.
func (a *ireApp) loadEvents(ctx context.Context, submissionID string) ([]contract.SubmissionEvent, error) {
	rows, err := a.store.Query(ctx, `select id, kind, coalesce(actor_email, ''), coalesce(data, ''), created_at
		from submission_event where submission_id = ? order by created_at, id`, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []contract.SubmissionEvent{}

	for rows.Next() {
		var (
			item        contract.SubmissionEvent
			actor, data string
		)
		if err := rows.Scan(&item.ID, &item.Kind, &actor, &data, &item.CreatedAt); err != nil {
			return nil, err
		}

		item.ActorEmail = stringPtr(actor)
		item.Data = stringPtr(data)
		out = append(out, item)
	}

	return out, rows.Err()
}

// claimFor returns the live claim on a ship, if any.
func (a *ireApp) claimFor(ctx context.Context, submissionID string, me reviewer) *contract.ClaimState {
	var email, name, expiresAt, reviewerID string

	err := a.store.QueryRow(ctx, `select c.reviewer_id, u.email, coalesce(u.name, ''), c.expires_at
		from claim c join user u on u.id = c.reviewer_id
		where c.submission_id = ? and c.expires_at > ?`, submissionID, a.nowText()).
		Scan(&reviewerID, &email, &name, &expiresAt)
	if err != nil {
		return nil
	}

	var mine int
	if reviewerID == me.ID {
		mine = 1
	}

	return &contract.ClaimState{ReviewerEmail: email, ReviewerName: stringPtr(name), ExpiresAt: expiresAt, IsMine: mine}
}

// acknowledgementsFor returns the check keys this reviewer acknowledged for a version.
func (a *ireApp) acknowledgementsFor(ctx context.Context, submissionID string, version int, userID string) []string {
	rows, err := a.store.Query(ctx, `select check_key from check_ack
		where submission_id = ? and version = ? and reviewer_id = ? order by check_key`, submissionID, version, userID)
	if err != nil {
		return []string{}
	}
	defer rows.Close()

	out := []string{}

	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err == nil {
			out = append(out, key)
		}
	}

	return out
}

// policyFor reads the review policy setting, falling back to the documented defaults.
func (a *ireApp) policyFor(ctx context.Context, programID string) reviewPolicy {
	policy := defaultPolicy()

	var value string
	if err := a.store.QueryRow(ctx, `select value from program_setting where program_id = ? and key = 'review_policy'`, programID).Scan(&value); err != nil {
		return policy
	}

	if err := json.Unmarshal([]byte(value), &policy); err != nil {
		return defaultPolicy()
	}

	return policy
}

// reviewPolicy is the stage 1 policy the prototype enforces. Stage 2 fraud is always out of scope for stage 1.
type reviewPolicy struct {
	HoldOnWarn               bool  `json:"holdOnWarn"`
	HoldMinApprovedSeconds   int   `json:"holdMinApprovedSeconds"`
	HoldTiers                []int `json:"holdTiers"`
	NoteRequiredForChanges   bool  `json:"noteRequiredForChanges"`
	FraudMinStage            int   `json:"fraudMinStage"`
	RequireTechnicalFeatures bool  `json:"requireTechnicalFeatures"`
	RequireDeflationReason   bool  `json:"requireDeflationReason"`
}

// defaultPolicy is the seeded policy when the setting is missing.
func defaultPolicy() reviewPolicy {
	return reviewPolicy{
		HoldOnWarn:               true,
		HoldMinApprovedSeconds:   0,
		HoldTiers:                []int{2, 3},
		NoteRequiredForChanges:   true,
		FraudMinStage:            2,
		RequireTechnicalFeatures: false,
		RequireDeflationReason:   true,
	}
}

// contractPolicy converts the internal policy to the API shape.
func contractPolicy(policy reviewPolicy) contract.ReviewPolicy {
	tiers := make([]int, len(policy.HoldTiers))
	copy(tiers, policy.HoldTiers)

	return contract.ReviewPolicy{
		HoldOnWarn:               boolInt(policy.HoldOnWarn),
		HoldMinApprovedSeconds:   policy.HoldMinApprovedSeconds,
		HoldTiers:                tiers,
		NoteRequiredForChanges:   boolInt(policy.NoteRequiredForChanges),
		FraudMinStage:            policy.FraudMinStage,
		RequireDeflationReason:   boolInt(policy.RequireDeflationReason),
		RequireTechnicalFeatures: boolInt(policy.RequireTechnicalFeatures),
	}
}

// boolInt returns 1 for true and 0 for false, the storage convention.
func boolInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

// jsonArrayLength counts the entries in a JSON array, returning zero for empty or invalid text.
func jsonArrayLength(value string) int {
	var items []any
	if err := json.Unmarshal([]byte(value), &items); err != nil {
		return 0
	}

	return len(items)
}

// jsonCoveredSeconds sums coveredSeconds from a timelapse array.
func jsonCoveredSeconds(value string) int {
	var items []struct {
		CoveredSeconds int `json:"coveredSeconds"`
	}
	if err := json.Unmarshal([]byte(value), &items); err != nil {
		return 0
	}

	var total int
	for _, item := range items {
		total += item.CoveredSeconds
	}

	return total
}

// draftFor returns the caller's saved draft for a ship, so a refresh restores typed notes.
func (a *ireApp) draftFor(ctx context.Context, submissionID, reviewerID string) *contract.DraftState {
	var note, internalNote, values, updatedAt string

	err := a.store.QueryRow(ctx, `select note, internal_note, coalesce("values", '{}'), updated_at from draft where submission_id = ? and reviewer_id = ?`,
		submissionID, reviewerID).Scan(&note, &internalNote, &values, &updatedAt)
	if err != nil {
		return nil
	}

	return &contract.DraftState{Note: note, InternalNote: internalNote, Values: values, UpdatedAt: updatedAt}
}

// payloadHackatimeProjects reads the Hackatime project names Half Life sent with the ship so the outbound event mirrors them.
func payloadHackatimeProjects(ctx context.Context, client *storage.Client, submissionID string, version int) []string {
	var payload string
	if err := client.QueryRow(ctx, `select payload from submission_version where submission_id = ? and version = ?`, submissionID, version).Scan(&payload); err != nil {
		return []string{}
	}

	var request contract.IngestRequest
	if err := json.Unmarshal([]byte(payload), &request); err != nil || len(request.HackatimeProjects) == 0 {
		return []string{}
	}

	return request.HackatimeProjects
}

// payloadPriorReviews reads the prior review context sent with a ship.
func payloadPriorReviews(ctx context.Context, client *storage.Client, submissionID string, version int) []contract.PriorReview {
	var payload string
	if err := client.QueryRow(ctx, `select payload from submission_version where submission_id = ? and version = ?`, submissionID, version).Scan(&payload); err != nil {
		return nil
	}

	var request contract.IngestRequest
	if err := json.Unmarshal([]byte(payload), &request); err != nil || request.Meta == nil {
		return nil
	}

	out := make([]contract.PriorReview, 0, len(request.Meta.PriorReviews))
	for _, prior := range request.Meta.PriorReviews {
		item := contract.PriorReview{Result: prior.Result}
		if prior.Feedback != nil {
			item.Feedback = prior.Feedback
		}

		if prior.Reviewer != nil {
			item.Reviewer = prior.Reviewer
		}

		if prior.At != nil {
			item.At = prior.At
		}

		if prior.Invalidated != nil {
			item.Invalidated = prior.Invalidated
		}

		out = append(out, item)
	}

	return out
}

// redactEmail hides a maker's address from reviewers while keeping the domain legible.
func redactEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "***"
	}

	return email[:1] + "***" + email[at:]
}

// isBlocked reports whether a check refuses an approval.
func isBlocked(check contract.CheckResult) bool {
	return check.Severity == severityBlock && (check.Status == statusFail || check.Status == statusError)
}

// isWarning reports whether a check needs an acknowledgement before approval.
func isWarning(check contract.CheckResult) bool {
	return check.Status == statusWarn || (check.Status == statusFail && check.Severity == severityWarn)
}

// isOpen reports whether a ship can still be claimed or decided.
func isOpen(status string) bool {
	switch status {
	case statusChecking, statusNeedsAttention, statusReady, statusClaimed, statusInReview, statusStage2Hold:
		return true
	}

	return false
}

// missingAcknowledgements returns the warning keys that were not acknowledged.
func missingAcknowledgements(checks []contract.CheckResult, acknowledged []string) []string {
	acked := make(map[string]struct{}, len(acknowledged))
	for _, key := range acknowledged {
		acked[key] = struct{}{}
	}

	var missing []string

	for _, check := range checks {
		if !isWarning(check) {
			continue
		}

		if _, ok := acked[check.Key]; !ok {
			missing = append(missing, check.Key)
		}
	}

	return missing
}

// blockedChecks returns the checks that forbid an approval.
func blockedChecks(checks []contract.CheckResult) []contract.CheckResult {
	var out []contract.CheckResult

	for _, check := range checks {
		if isBlocked(check) {
			out = append(out, check)
		}
	}

	return out
}

// errNotFound is the sentinel a loader returns when the row does not exist.
var errNotFound = errors.New("not found")
