package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// handleIngest accepts an Ari-compatible ship, verifies the signature over the raw body, and writes the snapshot, evidence and local checks in one transaction.
func (a *reviewApp) handleIngest(w http.ResponseWriter, r *http.Request) {
	program, ok := a.programFor(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxIngestBody)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		app.WriteError(w, http.StatusRequestEntityTooLarge, "payload is larger than 1 MiB")
		return
	}

	if !a.verifyIngestSignature(w, r, program, body) {
		return
	}

	var request contract.IngestRequest
	if err := json.Unmarshal(body, &request); err != nil {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "body", err.Error())
		return
	}

	if !validateIngest(w, request) {
		return
	}

	deliveryID := strings.TrimSpace(r.Header.Get("X-Ari-Delivery-Id"))
	if deliveryID == "" {
		deliveryID = "body:" + sha256Hex(body)
	}

	response, status, err := a.ingestShip(r.Context(), program, request, deliveryID, string(body))
	if err != nil {
		// A concurrent ingest can win the open-ship unique index between our read and the transaction. Report the ship it created instead of a storage 500.
		if id, version, openErr := a.openShip(r.Context(), program.ID, request.ExternalID); openErr == nil {
			app.WriteJSON(w, http.StatusConflict, map[string]any{"error": "already_queued", "id": id, "version": version})
			return
		}

		writeStorage(w, err)

		return
	}

	if status == http.StatusConflict {
		app.WriteJSON(w, status, map[string]any{"error": "already_queued", "id": response.ID, "version": response.Version})
		return
	}

	app.WriteJSON(w, status, response)
}

// validateIngest performs the field checks the generated contract cannot express, such as URL schemes and hour caps.
func validateIngest(w http.ResponseWriter, request contract.IngestRequest) bool {
	externalID := strings.TrimSpace(request.ExternalID)
	if externalID == "" {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "external_id", "external_id is required")
		return false
	}

	if strings.TrimSpace(request.Title) == "" {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "title", "title is required")
		return false
	}

	if strings.TrimSpace(request.Maker.Email) == "" {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "maker.email", "maker.email is required")
		return false
	}

	if request.RepoURL != nil && strings.TrimSpace(*request.RepoURL) != "" && !validHTTPURL(*request.RepoURL) {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "repo_url", "repo_url must be an http or https URL")
		return false
	}

	if request.DemoURL != nil && strings.TrimSpace(*request.DemoURL) != "" && !validHTTPURL(*request.DemoURL) {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "demo_url", "demo_url must be an http or https URL")
		return false
	}

	if request.ThumbnailURL != nil && strings.TrimSpace(*request.ThumbnailURL) != "" && !validHTTPURL(*request.ThumbnailURL) {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "thumbnail_url", "thumbnail_url must be an http or https URL")
		return false
	}

	if request.Meta != nil && request.Meta.Hours != nil {
		hours := request.Meta.Hours
		if hours.ClaimedSeconds < 0 || hours.ClaimedSeconds > maxHours ||
			hours.JournalSeconds < 0 || hours.JournalSeconds > maxHours ||
			hours.HackatimeSeconds < 0 || hours.HackatimeSeconds > maxHours {
			app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "meta.hours", "hours must be between 0 and 30 days")
			return false
		}
	}

	return true
}

// validHTTPURL reports whether value parses as an absolute http or https URL.
func validHTTPURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}

	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// verifyIngestSignature checks the HMAC over timestamp.body, or the legacy body-only signature when the programme allows it. It writes the 401 and returns false on failure.
func (a *reviewApp) verifyIngestSignature(w http.ResponseWriter, r *http.Request, program programRow, body []byte) bool {
	signature := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Ari-Signature")))
	if signature == "" {
		app.WriteErrorCode(w, http.StatusUnauthorized, "bad_signature", "X-Ari-Signature is required")
		return false
	}

	signer := a.signer(a.config.IngestSecret)

	if timestamp := strings.TrimSpace(r.Header.Get("X-Ari-Timestamp")); timestamp != "" {
		seconds, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil || absInt64(a.now().Unix()-seconds) > signatureWindow {
			app.WriteErrorCode(w, http.StatusUnauthorized, "bad_signature", "timestamp is invalid or stale")
			return false
		}

		if !signer.VerifyParts(signature, []byte(timestamp), []byte("."), body) {
			app.WriteErrorCode(w, http.StatusUnauthorized, "bad_signature", "signature does not match")
			return false
		}
	} else {
		if !a.allowLegacySignature(r.Context(), program.ID) {
			app.WriteErrorCode(w, http.StatusUnauthorized, "bad_signature", "X-Ari-Timestamp is required")
			return false
		}

		if !signer.VerifyParts(signature, body) {
			app.WriteErrorCode(w, http.StatusUnauthorized, "bad_signature", "signature does not match")
			return false
		}
	}

	return true
}

// allowLegacySignature reads the programme setting that accepts the current SDK's body-only signature. A missing or unreadable setting rejects the request rather than accepting an unverified body.
func (a *reviewApp) allowLegacySignature(ctx context.Context, programID string) bool {
	var value string
	if err := a.store.QueryRow(ctx, `select value from program_setting where program_id = ? and key = 'allow_legacy_signature'`, programID).Scan(&value); err != nil {
		return false
	}

	return strings.TrimSpace(value) == "true"
}

// ingestShip writes a new version, or reports the existing delivery or open ship. response is the API body; status is the HTTP status.
//
//nolint:funlen,maintidx,gocognit,gocyclo // The transaction statements and evidence mapping belong together.
func (a *reviewApp) ingestShip(ctx context.Context, program programRow, request contract.IngestRequest, deliveryID, rawBody string) (contract.IngestAccepted, int, error) {
	now := a.nowText()

	var (
		existingID      string
		existingVersion int
		existingHTTP    int
	)

	err := a.store.QueryRow(ctx, `select coalesce(submission_id, ''), coalesce(version, 0), http_status from ingest_delivery where delivery_id = ?`, deliveryID).
		Scan(&existingID, &existingVersion, &existingHTTP)
	if err == nil {
		status := http.StatusOK
		if existingHTTP == http.StatusConflict {
			status = http.StatusConflict
		}

		return contract.IngestAccepted{ID: existingID, Status: "duplicate", Version: existingVersion}, status, nil
	}

	if !errors.Is(err, storage.ErrNoRows) {
		return contract.IngestAccepted{}, 0, err
	}

	openID, openVersion, openErr := a.openShip(ctx, program.ID, request.ExternalID)
	if openErr != nil && !errors.Is(openErr, storage.ErrNoRows) {
		return contract.IngestAccepted{}, 0, openErr
	}

	if openErr == nil {
		if _, err := a.store.Exec(ctx, `insert into ingest_delivery (delivery_id, program_id, external_id, version, submission_id, status, http_status, payload_sha256, received_at, processed_at)
			values (?, ?, ?, ?, ?, 'conflict', ?, ?, ?, ?)`,
			deliveryID, program.ID, request.ExternalID, openVersion, openID, http.StatusConflict, sha256Hex([]byte(rawBody)), now, now); err != nil {
			return contract.IngestAccepted{}, 0, err
		}

		return contract.IngestAccepted{ID: openID, Status: "duplicate", Version: openVersion}, http.StatusConflict, nil
	}

	version := 1
	previousID := ""

	var (
		lastID, lastStatus string
		lastVersion        int
	)

	err = a.store.QueryRow(ctx, `select id, version, status from submission
		where program_id = ? and external_id = ? order by version desc limit 1`, program.ID, request.ExternalID).
		Scan(&lastID, &lastVersion, &lastStatus)
	if err == nil {
		version = lastVersion + 1
		previousID = lastID
	} else if !errors.Is(err, storage.ErrNoRows) {
		return contract.IngestAccepted{}, 0, err
	}

	nowTime := a.now()
	checks := localChecks(request, nowTime)

	var (
		evidence     *contract.Evidence
		queuedChecks bool
	)

	if request.RepoURL != nil && !a.config.DisableRepoChecks {
		if a.config.EnableJobs && a.config.Enqueue != nil {
			queuedChecks = true
		} else {
			remote, snapshot := a.repoChecks(ctx, *request.RepoURL, nowTime)
			checks = mergeChecks(checks, remote)
			evidence = snapshot
		}
	}

	summary := checkCountsFor(checks)

	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return contract.IngestAccepted{}, 0, err
	}

	status := statusReady

	switch {
	case queuedChecks:
		status = statusChecking
	case summary.Overall == checkOverallFail:
		status = statusNeedsAttention
	}

	id := newID("sub")

	track := "hardware"
	if request.Track != nil && strings.TrimSpace(*request.Track) != "" {
		track = strings.TrimSpace(*request.Track)
	}

	var (
		phase, theme, makerSlack, makerHackatime, demoURL, thumbnailURL *string
		week, requestedTier, priority, grantCap                         *int
		bomTotal                                                        int
		hoursClaimed, hoursJournal, hoursHackatime                      int
	)

	if request.Meta != nil {
		meta := request.Meta
		phase = trimSpace(meta.Phase)
		theme = trimSpace(meta.Theme)
		week = meta.Week
		requestedTier = meta.RequestedTier
		priority = meta.Priority
		grantCap = meta.GrantCapCents

		if meta.Hours != nil {
			hoursClaimed = meta.Hours.ClaimedSeconds
			hoursJournal = meta.Hours.JournalSeconds
			hoursHackatime = meta.Hours.HackatimeSeconds
		}

		for _, line := range meta.Bom {
			quantity := 1
			if line.Qty != nil {
				quantity = max(*line.Qty, 0)
			}

			unit := 0
			if line.UnitUsd != nil {
				unit = int(math.Round(*line.UnitUsd * 100))
			}

			bomTotal += quantity * unit
		}
	}

	makerSlack = trimSpace(request.Maker.SlackID)
	makerHackatime = trimSpace(request.Maker.HackatimeID)
	demoURL = trimSpace(request.DemoURL)
	thumbnailURL = trimSpace(request.ThumbnailURL)

	if hoursJournal == 0 {
		for _, entry := range request.Journals {
			hoursJournal += journalSeconds(entry)
		}
	}

	if hoursClaimed == 0 {
		hoursClaimed = hoursJournal
	}

	statements := []storage.TransactionStatement{
		{SQL: `insert into submission (id, program_id, external_id, version, previous_submission_id, status, stage, priority, title, description, track, theme, phase, week, maker_email, maker_name, maker_slack_id, maker_hackatime_id, repo_url, demo_url, thumbnail_url, requested_tier, grant_cap_cents, bom_total_cents, hours_claimed_seconds, hours_journal_seconds, hours_hackatime_seconds, check_summary, ingested_at, created_at, updated_at)
			values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{id, program.ID, request.ExternalID, version, nullString(previousID), status, stageOne, intValue(priority), request.Title, request.Description, track, nullString(stringValue(theme)), nullString(stringValue(phase)), nullInt(week), strings.ToLower(strings.TrimSpace(request.Maker.Email)), request.Maker.Name, nullString(stringValue(makerSlack)), nullString(stringValue(makerHackatime)), nullString(stringValue(request.RepoURL)), nullString(stringValue(demoURL)), nullString(stringValue(thumbnailURL)), nullInt(requestedTier), nullInt(grantCap), bomTotal, hoursClaimed, hoursJournal, hoursHackatime, string(summaryJSON), now, now, now}},
		{SQL: `insert into submission_version (submission_id, version, payload, payload_sha256, received_at, delivery_id)
			values (?, ?, ?, ?, ?, ?)`,
			Args: []any{id, version, rawBody, sha256Hex([]byte(rawBody)), now, deliveryID}},
		{SQL: `insert into submission_event (id, submission_id, kind, data, created_at) values (?, ?, 'ingested', ?, ?)`,
			Args: []any{newID("evt"), id, `{"source":"ingest"}`, now}},
		{SQL: `insert into submission_event (id, submission_id, kind, data, created_at) values (?, ?, 'checks_ready', ?, ?)`,
			Args: []any{newID("evt"), id, string(summaryJSON), now}},
		{SQL: `insert into ingest_delivery (delivery_id, program_id, external_id, version, submission_id, status, http_status, payload_sha256, received_at, processed_at)
			values (?, ?, ?, ?, ?, 'accepted', ?, ?, ?, ?)`,
			Args: []any{deliveryID, program.ID, request.ExternalID, version, id, http.StatusCreated, sha256Hex([]byte(rawBody)), now, now}},
	}

	for i, entry := range request.Journals {
		seconds := journalSeconds(entry)
		mediaJSON, _ := json.Marshal(mediaURLs(entry))
		timelapseJSON, _ := json.Marshal(timelapseRecords(entry))

		title := "Work session"
		if entry.Markdown != nil && strings.TrimSpace(*entry.Markdown) != "" {
			title = firstLine(*entry.Markdown)
		}

		statements = append(statements, storage.TransactionStatement{
			SQL:  `insert into journal_entry (id, submission_id, at, seconds, title, text, markdown, media, timelapses, sort) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{newID("jrn"), id, entry.At, seconds, title, entry.Text, stringValue(entry.Markdown), string(mediaJSON), string(timelapseJSON), i},
		})
	}

	if request.Meta != nil {
		for i, line := range request.Meta.Bom {
			quantity := 1
			if line.Qty != nil {
				quantity = max(*line.Qty, 0)
			}

			unit := 0
			if line.UnitUsd != nil {
				unit = int(math.Round(*line.UnitUsd * 100))
			}

			statements = append(statements, storage.TransactionStatement{
				SQL:  `insert into bom_line (id, submission_id, name, quantity, unit_cost_cents, vendor, link, sort) values (?, ?, ?, ?, ?, ?, ?, ?)`,
				Args: []any{newID("bom"), id, line.Name, quantity, unit, stringValue(line.Vendor), stringValue(line.Link), i},
			})
		}
	}

	for _, check := range checks {
		statements = append(statements, storage.TransactionStatement{
			SQL: `insert into check_result (id, submission_id, version, check_key, label, group_key, status, severity, summary, detail, checked_at)
				values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{newID("chk"), id, version, check.Key, check.Label, check.Group, check.Status, check.Severity, check.Summary, stringValue(check.Detail), check.CheckedAt},
		})
	}

	if queuedChecks {
		statements = append(statements, storage.TransactionStatement{
			SQL: `insert or replace into check_run (submission_id, version, status, attempts, started_at, finished_at, error, runner_version)
				values (?, ?, 'queued', 0, ?, null, null, ?)`,
			Args: []any{id, version, now, checkRunnerVersion},
		})
	}

	if evidence != nil {
		files, marshalErr := json.Marshal(evidence.Files)
		if marshalErr != nil {
			return contract.IngestAccepted{}, 0, marshalErr
		}

		commits, marshalErr := json.Marshal(evidence.Commits)
		if marshalErr != nil {
			return contract.IngestAccepted{}, 0, marshalErr
		}

		statements = append(statements, storage.TransactionStatement{
			SQL: `insert into repo_snapshot (submission_id, version, forge, full_name, default_branch, head_sha,
					file_count, truncated, readme_text, files, commits, fetched_at, error)
				values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '')`,
			Args: []any{id, version, evidence.Forge, evidence.FullName, stringValue(evidence.DefaultBranch),
				stringValue(evidence.HeadSha), len(evidence.Files), boolInt(evidence.Truncated), stringValue(evidence.Readme),
				string(files), string(commits), evidence.FetchedAt},
		})
	}

	if _, err := a.store.Transaction(ctx, statements...); err != nil {
		return contract.IngestAccepted{}, 0, err
	}

	if queuedChecks {
		a.queueRepositoryChecks(ctx, id, version)
	}

	return contract.IngestAccepted{ID: id, Status: "accepted", Version: version}, http.StatusCreated, nil
}

// openShip returns the open ship for an external id.
func (a *reviewApp) openShip(ctx context.Context, programID, externalID string) (string, int, error) {
	var (
		id      string
		version int
	)

	err := a.store.QueryRow(ctx, `select id, version from submission
		where program_id = ? and external_id = ? and status in ('checking','needs_attention','ready','claimed','in_review','stage2_hold')
		limit 1`, programID, externalID).Scan(&id, &version)

	return id, version, err
}

// handleWithdraw closes an open ship. It is idempotent: a ship that is already decided still reports withdrawn with its id.
func (a *reviewApp) handleWithdraw(w http.ResponseWriter, r *http.Request) {
	program, ok := a.programFor(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAPIBody)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		app.WriteError(w, http.StatusRequestEntityTooLarge, "payload is too large")
		return
	}

	if !a.verifyIngestSignature(w, r, program, body) {
		return
	}

	var request contract.WithdrawRequest
	if err := json.Unmarshal(body, &request); err != nil {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "body", err.Error())
		return
	}

	var id, status string

	err = a.store.QueryRow(r.Context(), `select id, status from submission
		where program_id = ? and external_id = ? order by version desc limit 1`, program.ID, request.ExternalID).
		Scan(&id, &status)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			app.WriteError(w, http.StatusNotFound, "unknown ship")
			return
		}

		writeStorage(w, err)

		return
	}

	if status == statusDecided || status == statusWithdrawn {
		app.WriteJSON(w, http.StatusOK, contract.WithdrawOutcome{ID: id, Status: "withdrawn"})
		return
	}

	now := a.nowText()
	if _, err := a.store.Transaction(r.Context(),
		storage.TransactionStatement{SQL: `update submission set status = ?, check_summary = ?, updated_at = ? where id = ?`,
			Args: []any{statusWithdrawn, `{"overall":"pass","pass":0,"warn":0,"fail":0,"error":0,"running":0}`, now, id}},
		storage.TransactionStatement{SQL: `delete from claim where submission_id = ?`, Args: []any{id}},
		storage.TransactionStatement{SQL: `insert into submission_event (id, submission_id, kind, created_at) values (?, ?, 'withdrawn', ?)`,
			Args: []any{newID("evt"), id, now}},
	); err != nil {
		writeStorage(w, err)

		return
	}

	app.WriteJSON(w, http.StatusOK, contract.WithdrawOutcome{ID: id, Status: "withdrawn"})
}

// localChecks is the prototype's offline check suite. It reads only the payload: no network, no forge token. The fixtures show what the ported checks will report.
func localChecks(request contract.IngestRequest, now time.Time) []contract.CheckResult {
	checkedAt := now.Format(timestampFormat)
	checks := make([]contract.CheckResult, 0, 7)

	repo := ""
	if request.RepoURL != nil {
		repo = strings.TrimSpace(*request.RepoURL)
	}

	if repo == "" {
		checks = append(checks, check("repo.accessible", "Repository reachable", "repo", statusFail, severityBlock, "No repository URL", "Half Life sent no repo_url, so the ship cannot be reviewed.", checkedAt))
	} else {
		checks = append(checks, check("repo.accessible", "Repository reachable", "repo", statusPass, severityInfo, "Repository URL present", "Network fetching is disabled in this prototype; the ported check fetches the tree and README.", checkedAt))
	}

	description := strings.TrimSpace(request.Description)
	if len(description) >= minDescriptionLength {
		checks = append(checks, check("repo.description", "Project description", "repo", statusPass, severityInfo, "Detailed description", descriptionLength(len(description)), checkedAt))
	} else {
		checks = append(checks, check("repo.description", "Project description", "repo", statusWarn, severityWarn, "Short description", descriptionLength(len(description)), checkedAt))
	}

	images := 0
	if request.ThumbnailURL != nil && strings.TrimSpace(*request.ThumbnailURL) != "" {
		images++
	}

	shortEntries := 0

	for _, entry := range request.Journals {
		images += len(entry.Media)

		if len(strings.TrimSpace(entry.Text)) < minJournalText {
			shortEntries++
		}
	}

	switch {
	case images >= 2:
		checks = append(checks, check("repo.readme.images", "README images", "repo", statusPass, severityInfo, strconv.Itoa(images)+" images", "The ship carries enough images for a first pass.", checkedAt))
	case images == 1:
		checks = append(checks, check("repo.readme.images", "README images", "repo", statusWarn, severityWarn, "1 image (2 recommended)", "Add a schematic or board render.", checkedAt))
	default:
		checks = append(checks, check("repo.readme.images", "README images", "repo", statusWarn, severityWarn, "No images", "The payload carries no cover or journal images.", checkedAt))
	}

	bomLines := 0
	if request.Meta != nil {
		bomLines = len(request.Meta.Bom)
	}

	if bomLines > 0 {
		checks = append(checks, check("repo.bom", "Bill of materials", "repo", statusPass, severityInfo, strconv.Itoa(bomLines)+" BOM lines", "The platform list was sent with the ship.", checkedAt))
	} else {
		checks = append(checks, check("repo.bom", "Bill of materials", "repo", statusFail, severityWarn, "No BOM sent", "Half Life sent no BOM lines, so the grant cannot be checked.", checkedAt))
	}

	switch {
	case len(request.Journals) == 0:
		checks = append(checks, check("evidence.journals", "Journal evidence", "evidence", statusFail, severityWarn, "No journal entries", "There is no written evidence to read.", checkedAt))
	case shortEntries > 0:
		checks = append(checks, check("evidence.journals", "Journal evidence", "evidence", statusWarn, severityWarn, strconv.Itoa(shortEntries)+" short entries", "Some entries lack enough detail to justify their time.", checkedAt))
	default:
		checks = append(checks, check("evidence.journals", "Journal evidence", "evidence", statusPass, severityInfo, strconv.Itoa(len(request.Journals))+" entries", "Every entry has enough detail for a first pass.", checkedAt))
	}

	claimed := 0
	if request.Meta != nil && request.Meta.Hours != nil {
		claimed = request.Meta.Hours.ClaimedSeconds
	}

	if claimed > 0 {
		checks = append(checks, check("evidence.hours", "Claimed hours", "evidence", statusPass, severityInfo, strconv.Itoa(claimed/secondsPerHour)+"h claimed", "The hours breakdown was sent with the ship.", checkedAt))
	} else {
		checks = append(checks, check("evidence.hours", "Claimed hours", "evidence", statusWarn, severityWarn, "No hours claimed", "The maker claimed no time.", checkedAt))
	}

	return checks
}

// check builds one check result.
func check(key, label, group, status, severity, summary, detail, checkedAt string) contract.CheckResult {
	return contract.CheckResult{
		Key:       key,
		Label:     label,
		Group:     group,
		Status:    status,
		Severity:  severity,
		Summary:   summary,
		Detail:    stringPtr(detail),
		CheckedAt: checkedAt,
	}
}

// descriptionLength explains why the description check warns.
func descriptionLength(length int) string {
	return strconv.Itoa(length) + " characters; 160 is the prototype's minimum."
}

// journalSeconds reads an entry's duration, preferring seconds over the Ari-compatible minutes.
func journalSeconds(entry contract.IngestJournal) int {
	if entry.Seconds != nil {
		return max(*entry.Seconds, 0)
	}

	if entry.Minutes != nil {
		return max(*entry.Minutes, 0) * 60
	}

	return 0
}

// mediaURLs returns the media records worth copying into the evidence snapshot.
func mediaURLs(entry contract.IngestJournal) []map[string]string {
	out := make([]map[string]string, 0, len(entry.Media))
	for _, media := range entry.Media {
		item := map[string]string{"url": media.URL}
		if media.Type != nil {
			item["type"] = *media.Type
		} else {
			item["type"] = "image"
		}

		if media.Alt != nil {
			item["alt"] = *media.Alt
		}

		out = append(out, item)
	}

	return out
}

// timelapseRecords copies the clip metadata so coverage is visible without the provider.
func timelapseRecords(entry contract.IngestJournal) []map[string]any {
	out := make([]map[string]any, 0, len(entry.Timelapses))
	for _, clip := range entry.Timelapses {
		item := map[string]any{}
		if clip.Provider != nil {
			item["provider"] = *clip.Provider
		}

		if clip.ExternalID != nil {
			item["externalId"] = *clip.ExternalID
		}

		if clip.URL != nil {
			item["url"] = *clip.URL
		}

		if clip.CoveredSeconds != nil {
			item["coveredSeconds"] = *clip.CoveredSeconds
		}

		out = append(out, item)
	}

	return out
}

// firstLine returns the first non-empty line, trimmed to a stable title.
func firstLine(value string) string {
	for line := range strings.SplitSeq(value, "\n") {
		trimmed := strings.TrimSpace(strings.TrimLeft(line, "# "))
		if trimmed != "" {
			return trimmed
		}
	}

	return "Work session"
}

// nullString encodes an empty string as SQL NULL.
func nullString(value string) any {
	if value == "" {
		return nil
	}

	return value
}

// nullInt encodes a nil pointer as SQL NULL, or its value.
func nullInt(value *int) any {
	if value == nil {
		return nil
	}

	return *value
}

// intValue returns a pointer's value or zero.
func intValue(value *int) int {
	if value == nil {
		return 0
	}

	return *value
}

// stringValue returns a pointer's value or an empty string.
func stringValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

// absInt64 returns the absolute value of v.
func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}

	return v
}

// hmacHex signs message with secret and returns lowercase hex.
func hmacHex(secret string, message []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(message)

	return hex.EncodeToString(mac.Sum(nil))
}
