package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// findingSeverities and findingStatuses keep the database and the wire in step without a second enum.
var findingSeverities = map[string]bool{"block": true, "fixable": true, "hold": true, "note": true}

var findingStatuses = map[string]bool{"accepted": true, "disputed": true, "fixed": true, "open": true, "withdrawn": true}

// handleFindings lists the findings that follow a project across versions.
func (a *ireApp) handleFindings(w http.ResponseWriter, r *http.Request) {
	_, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	findings, err := a.loadFindings(r.Context(), ship.ProgramID, ship.ExternalID)
	if err != nil {
		writeStorage(w, err)

		return
	}

	app.WriteJSON(w, http.StatusOK, contract.FindingsResponse{Findings: findings})
}

// handleFindingCreate records one problem or note for a project.
func (a *ireApp) handleFindingCreate(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewerWrite(w, r)
	if !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	if ship.Status == statusDecided || ship.Status == statusWithdrawn {
		app.WriteError(w, http.StatusConflict, "this ship has been closed")
		return
	}

	if !a.holdsClaim(r.Context(), ship.ID, person.ID) {
		app.WriteError(w, http.StatusConflict, "claim the ship before adding a finding")
		return
	}

	var request contract.FindingCreateRequest
	if !app.ReadJSON(w, r, maxAPIBody, "summary", &request) {
		return
	}

	summary := strings.TrimSpace(request.Summary)
	if summary == "" {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "summary", "summarise the finding in one line")
		return
	}

	if !findingSeverities[request.Severity] {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "severity", "choose block, fixable, note or hold")
		return
	}

	station := strings.TrimSpace(request.Station)
	if station == "" {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "station", "name the station that found this")
		return
	}

	evidence := strings.TrimSpace(stringValue(request.Evidence))

	now := a.nowText()
	id := newID("fnd")

	if _, err := a.store.Exec(r.Context(), `insert into finding (id, program_id, external_id, opened_submission_id, opened_version,
			station, question, severity, status, summary, detail, maker_action, evidence, opened_by, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?, ?, ?, ?, ?, ?)`,
		id, ship.ProgramID, ship.ExternalID, ship.ID, ship.Version, station, stringValue(request.Question),
		request.Severity, summary, stringValue(request.Detail), stringValue(request.MakerAction), evidence, person.ID, now, now); err != nil {
		writeStorage(w, err)

		return
	}

	finding, err := a.loadFinding(r.Context(), id)
	if err != nil {
		writeStorage(w, err)

		return
	}

	app.WriteJSON(w, http.StatusOK, contract.FindingResponse{Finding: finding})
}

// handleFindingUpdate moves a finding through its lifecycle, usually from open to fixed during fix verification.
func (a *ireApp) handleFindingUpdate(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewerWrite(w, r)
	if !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	if !a.holdsClaim(r.Context(), ship.ID, person.ID) {
		app.WriteError(w, http.StatusConflict, "claim the ship before changing a finding")
		return
	}

	var request contract.FindingUpdateRequest
	if !app.ReadJSON(w, r, maxAPIBody, "status", &request) {
		return
	}

	if !findingStatuses[request.Status] {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "status", "choose open, fixed, accepted, disputed or withdrawn")
		return
	}

	finding, err := a.loadFinding(r.Context(), r.PathValue("finding"))
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			app.WriteError(w, http.StatusNotFound, "unknown finding")
			return
		}

		writeStorage(w, err)

		return
	}

	if finding.ExternalID != ship.ExternalID {
		app.WriteError(w, http.StatusNotFound, "unknown finding")
		return
	}

	now := a.nowText()
	resolution := strings.TrimSpace(stringValue(request.Resolution))

	var resolvedBy any

	var resolvedVersion any

	switch request.Status {
	case "fixed", "accepted", "withdrawn":
		resolvedBy = person.ID
		resolvedVersion = ship.Version
	default:
		resolution = ""
	}

	if _, err := a.store.Exec(r.Context(), `update finding set status = ?, resolution = ?, resolved_by = ?,
			resolved_submission_id = ?, resolved_version = ?, updated_at = ? where id = ?`,
		request.Status, nullString(resolution), resolvedBy, resolvedSubmission(resolvedBy, ship.ID), resolvedVersion, now, finding.ID); err != nil {
		writeStorage(w, err)

		return
	}

	updated, err := a.loadFinding(r.Context(), finding.ID)
	if err != nil {
		writeStorage(w, err)

		return
	}

	app.WriteJSON(w, http.StatusOK, contract.FindingResponse{Finding: updated})
}

// resolvedSubmission keeps the submission link only when the finding has been resolved.
func resolvedSubmission(resolvedBy any, submissionID string) any {
	if resolvedBy == nil {
		return nil
	}

	return submissionID
}

// loadFindings reads every finding for a project, newest first.
func (a *ireApp) loadFindings(ctx context.Context, programID, externalID string) ([]contract.Finding, error) {
	rows, err := a.store.Query(ctx, `select id, external_id, opened_submission_id, opened_version, station, coalesce(question, ''),
			severity, status, summary, coalesce(detail, ''), coalesce(maker_action, ''), coalesce(evidence, '{}'),
			coalesce(opened_by, ''), coalesce(resolved_by, ''), coalesce(resolved_version, 0), coalesce(resolution, ''),
			created_at, updated_at
		from finding where program_id = ? and external_id = ? order by created_at desc, id desc`, programID, externalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []contract.Finding{}

	for rows.Next() {
		item, err := scanFinding(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, item)
	}

	return out, rows.Err()
}

// loadFinding reads one finding by id.
func (a *ireApp) loadFinding(ctx context.Context, id string) (contract.Finding, error) {
	row := a.store.QueryRow(ctx, `select id, external_id, opened_submission_id, opened_version, station, coalesce(question, ''),
			severity, status, summary, coalesce(detail, ''), coalesce(maker_action, ''), coalesce(evidence, '{}'),
			coalesce(opened_by, ''), coalesce(resolved_by, ''), coalesce(resolved_version, 0), coalesce(resolution, ''),
			created_at, updated_at
		from finding where id = ?`, id)

	return scanFinding(row)
}

// findingScanner is the row shape shared by loadFinding and loadFindings.
type findingScanner interface {
	Scan(dest ...any) error
}

// scanFinding turns one row into the wire shape.
func scanFinding(row findingScanner) (contract.Finding, error) {
	var (
		item                                    contract.Finding
		question, detail, makerAction, evidence string
		openedBy, resolvedBy, resolution        string
		resolvedVersion                         int
	)

	if err := row.Scan(&item.ID, &item.ExternalID, &item.OpenedSubmissionID, &item.OpenedVersion, &item.Station, &question,
		&item.Severity, &item.Status, &item.Summary, &detail, &makerAction, &evidence,
		&openedBy, &resolvedBy, &resolvedVersion, &resolution, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return contract.Finding{}, err
	}

	item.Question = optionalText(question)
	item.Detail = optionalText(detail)
	item.MakerAction = optionalText(makerAction)
	item.Evidence = optionalText(evidence)
	item.OpenedBy = optionalText(openedBy)
	item.ResolvedBy = optionalText(resolvedBy)
	item.Resolution = optionalText(resolution)

	if resolvedVersion > 0 {
		item.ResolvedVersion = &resolvedVersion
	}

	return item, nil
}

// openFindings returns the open findings that make an approval unsafe.
func (a *ireApp) openFindings(ctx context.Context, programID, externalID string) ([]contract.Finding, error) {
	findings, err := a.loadFindings(ctx, programID, externalID)
	if err != nil {
		return nil, err
	}

	out := make([]contract.Finding, 0, len(findings))

	for _, finding := range findings {
		if finding.Status == "open" && (finding.Severity == "block" || finding.Severity == "fixable") {
			out = append(out, finding)
		}
	}

	return out, nil
}

// findingProblem blocks an approval with unresolved returnable findings and validates the findings named in a return.
func (a *ireApp) findingProblem(ctx context.Context, request contract.DecisionRequest, ship submissionRow) *decisionProblem {
	if request.Decision == decisionApproved {
		open, err := a.openFindings(ctx, ship.ProgramID, ship.ExternalID)
		if err != nil {
			app.Errorf("open findings: %v", err)

			return &decisionProblem{status: http.StatusInternalServerError, message: "could not read the findings"}
		}

		if len(open) > 0 {
			return &decisionProblem{status: http.StatusUnprocessableEntity, code: "open_findings", field: "findings",
				message: "resolve the open findings before approving, or return the ship with the complete list"}
		}

		return nil
	}

	if request.Decision != decisionChanges || len(request.FindingIds) == 0 {
		return nil
	}

	findings, err := a.loadFindings(ctx, ship.ProgramID, ship.ExternalID)
	if err != nil {
		app.Errorf("findings: %v", err)

		return &decisionProblem{status: http.StatusInternalServerError, message: "could not read the findings"}
	}

	open := map[string]bool{}

	for _, finding := range findings {
		if finding.Status == "open" {
			open[finding.ID] = true
		}
	}

	for _, id := range request.FindingIds {
		if !open[id] {
			return &decisionProblem{status: http.StatusUnprocessableEntity, code: "invalid_finding", field: "findingIds",
				message: "a returned finding is missing or already resolved"}
		}
	}

	return nil
}
