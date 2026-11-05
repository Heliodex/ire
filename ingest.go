package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// handleIngest accepts an Ari-compatible ship, verifies the signature over the raw body, and writes the snapshot, evidence and local checks in one transaction.
func (a *ireApp) handleIngest(w http.ResponseWriter, r *http.Request) {
	program, ok := a.programFor(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxIngestBody)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		app.WriteError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("payload is larger than %d MiB", maxIngestBody>>20))
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

// verifyIngestSignature checks the HMAC over timestamp.body, or the legacy body-only signature when the programme allows it. It writes the 401 and returns false on failure.
func (a *ireApp) verifyIngestSignature(w http.ResponseWriter, r *http.Request, program programRow, body []byte) bool {
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
func (a *ireApp) allowLegacySignature(ctx context.Context, programID string) bool {
	var value string
	if err := a.store.QueryRow(ctx, `select value from program_setting where program_id = ? and key = 'allow_legacy_signature'`, programID).Scan(&value); err != nil {
		return false
	}

	return strings.TrimSpace(value) == "true"
}

// ingestShip writes a new version, or reports the existing delivery or open ship. response is the API body; status is the HTTP status.
func (a *ireApp) ingestShip(ctx context.Context, program programRow, request contract.IngestRequest, deliveryID, rawBody string) (contract.IngestAccepted, int, error) {
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

	version, previousID, err := a.nextSubmissionVersion(ctx, program.ID, request.ExternalID)
	if err != nil {
		return contract.IngestAccepted{}, 0, err
	}

	nowTime := a.now()
	checks := localChecks(request, nowTime)
	checks = append(checks, a.duplicateChecks(ctx, program.ID, request, nowTime)...)

	var (
		evidence     *contract.Evidence
		queuedChecks bool
	)

	if request.RepoURL != nil && !a.config.DisableRepoChecks {
		if a.config.EnableJobs && a.config.Enqueue != nil {
			queuedChecks = true
		} else {
			remote, snapshot := a.repoChecks(ctx, *request.RepoURL, a.commitSince(ctx, program.ID, previousID), nowTime, submittedBom(request.Meta))
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

	fields := buildSubmissionFields(request)

	statements := []storage.TransactionStatement{
		submissionStatement(id, program, request, fields, version, previousID, status, string(summaryJSON), now),
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

	statements = append(statements, journalStatements(id, request.Journals)...)
	statements = append(statements, hackatimeStatements(id, request.HackatimeProjects)...)
	statements = append(statements, bomStatements(id, request.Meta)...)
	statements = append(statements, checkStatements(id, version, checks)...)

	if queuedChecks {
		statements = append(statements, storage.TransactionStatement{
			SQL: `insert or replace into check_run (submission_id, version, status, attempts, started_at, finished_at, error, runner_version)
				values (?, ?, 'queued', 0, ?, null, null, ?)`,
			Args: []any{id, version, now, checkRunnerVersion},
		})
	}

	snapshot, err := evidenceStatement(id, version, evidence)
	if err != nil {
		return contract.IngestAccepted{}, 0, err
	}

	if snapshot.SQL != "" {
		statements = append(statements, snapshot)
	}

	if _, err := a.store.Transaction(ctx, statements...); err != nil {
		return contract.IngestAccepted{}, 0, err
	}

	if queuedChecks {
		a.queueRepositoryChecks(ctx, id, version, fmt.Sprintf("checks:%s:%d", id, version))
	}

	return contract.IngestAccepted{ID: id, Status: "accepted", Version: version}, http.StatusCreated, nil
}

// nextSubmissionVersion returns the next version and the ship it replaces.
func (a *ireApp) nextSubmissionVersion(ctx context.Context, programID, externalID string) (int, string, error) {
	var (
		lastID, lastStatus string
		lastVersion        int
	)

	err := a.store.QueryRow(ctx, `select id, version, status from submission
		where program_id = ? and external_id = ? order by version desc limit 1`, programID, externalID).
		Scan(&lastID, &lastVersion, &lastStatus)
	if errors.Is(err, storage.ErrNoRows) {
		return 1, "", nil
	}

	if err != nil {
		return 0, "", err
	}

	return lastVersion + 1, lastID, nil
}

// openShip returns the open ship for an external id.
func (a *ireApp) openShip(ctx context.Context, programID, externalID string) (string, int, error) {
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
func (a *ireApp) handleWithdraw(w http.ResponseWriter, r *http.Request) {
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
