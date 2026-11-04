package main

import (
	"encoding/json/v2"
	"math"
	"strings"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// submissionFields is the ingest payload flattened for the submission row.
type submissionFields struct {
	track                                                        string
	phase, theme, makerSlack, makerHackatime, demoURL, thumbnail *string
	week, requestedTier, priority, grantCap                      *int
	bomTotal, hoursClaimed, hoursJournal, hoursHackatime         int
}

// buildSubmissionFields maps the optional ingest blocks onto the submission row.
func buildSubmissionFields(request contract.IngestRequest) submissionFields {
	fields := submissionFields{track: "hardware"}

	if request.Track != nil && strings.TrimSpace(*request.Track) != "" {
		fields.track = strings.TrimSpace(*request.Track)
	}

	if request.Meta != nil {
		meta := request.Meta
		fields.phase = trimSpace(meta.Phase)
		fields.theme = trimSpace(meta.Theme)
		fields.week = meta.Week
		fields.requestedTier = meta.RequestedTier
		fields.priority = meta.Priority
		fields.grantCap = meta.GrantCapCents

		if meta.Hours != nil {
			fields.hoursClaimed = meta.Hours.ClaimedSeconds
			fields.hoursJournal = meta.Hours.JournalSeconds
			fields.hoursHackatime = meta.Hours.HackatimeSeconds
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

			fields.bomTotal += quantity * unit
		}
	}

	fields.makerSlack = trimSpace(request.Maker.SlackID)
	fields.makerHackatime = trimSpace(request.Maker.HackatimeID)
	fields.demoURL = trimSpace(request.DemoURL)
	fields.thumbnail = trimSpace(request.ThumbnailURL)

	if fields.hoursJournal == 0 {
		for _, entry := range request.Journals {
			fields.hoursJournal += journalSeconds(entry)
		}
	}

	if fields.hoursClaimed == 0 {
		fields.hoursClaimed = fields.hoursJournal
	}

	return fields
}

// submissionStatement inserts the ship row itself.
func submissionStatement(id string, program programRow, request contract.IngestRequest, fields submissionFields, version int, previousID, status, summaryJSON, now string) storage.TransactionStatement {
	return storage.TransactionStatement{
		SQL: `insert into submission (id, program_id, external_id, version, previous_submission_id, status, stage, priority, title, description, track, theme, phase, week, maker_email, maker_name, maker_slack_id, maker_hackatime_id, repo_url, demo_url, thumbnail_url, requested_tier, grant_cap_cents, bom_total_cents, hours_claimed_seconds, hours_journal_seconds, hours_hackatime_seconds, check_summary, ingested_at, created_at, updated_at)
			values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		Args: []any{id, program.ID, request.ExternalID, version, nullString(previousID), status, stageOne, intValue(fields.priority), request.Title, request.Description, fields.track, nullString(stringValue(fields.theme)), nullString(stringValue(fields.phase)), nullInt(fields.week), strings.ToLower(strings.TrimSpace(request.Maker.Email)), request.Maker.Name, nullString(stringValue(fields.makerSlack)), nullString(stringValue(fields.makerHackatime)), nullString(stringValue(request.RepoURL)), nullString(stringValue(fields.demoURL)), nullString(stringValue(fields.thumbnail)), nullInt(fields.requestedTier), nullInt(fields.grantCap), fields.bomTotal, fields.hoursClaimed, fields.hoursJournal, fields.hoursHackatime, summaryJSON, now, now, now},
	}
}

// journalStatements inserts the journal entries and their media metadata.
func journalStatements(id string, entries []contract.IngestJournal) []storage.TransactionStatement {
	statements := make([]storage.TransactionStatement, 0, len(entries))

	for i, entry := range entries {
		mediaJSON, _ := json.Marshal(mediaURLs(entry))
		timelapseJSON, _ := json.Marshal(timelapseRecords(entry))

		title := "Work session"
		if entry.Markdown != nil && strings.TrimSpace(*entry.Markdown) != "" {
			title = firstLine(*entry.Markdown)
		}

		statements = append(statements, storage.TransactionStatement{
			SQL:  `insert into journal_entry (id, submission_id, at, seconds, title, text, markdown, media, timelapses, sort) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{newID("jrn"), id, entry.At, journalSeconds(entry), title, entry.Text, stringValue(entry.Markdown), string(mediaJSON), string(timelapseJSON), i},
		})
	}

	return statements
}

// hackatimeStatements links the named projects that the check job will refresh.
func hackatimeStatements(id string, projects []string) []storage.TransactionStatement {
	statements := make([]storage.TransactionStatement, 0, len(projects))
	seen := map[string]struct{}{}

	for i, project := range projects {
		project = strings.TrimSpace(project)
		if project == "" || i >= 20 {
			continue
		}

		if _, found := seen[project]; found {
			continue
		}

		seen[project] = struct{}{}

		statements = append(statements, storage.TransactionStatement{
			SQL:  `insert into hackatime_link (submission_id, project) values (?, ?)`,
			Args: []any{id, project},
		})
	}

	return statements
}

// bomStatements inserts the platform's priced parts.
func bomStatements(id string, meta *contract.IngestMeta) []storage.TransactionStatement {
	if meta == nil {
		return nil
	}

	statements := make([]storage.TransactionStatement, 0, len(meta.Bom))

	for i, line := range meta.Bom {
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

	return statements
}

// checkStatements stores the initial check rows.
func checkStatements(id string, version int, checks []contract.CheckResult) []storage.TransactionStatement {
	statements := make([]storage.TransactionStatement, 0, len(checks))

	for _, check := range checks {
		statements = append(statements, storage.TransactionStatement{
			SQL: `insert into check_result (id, submission_id, version, check_key, label, group_key, status, severity, summary, detail, checked_at)
				values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{newID("chk"), id, version, check.Key, check.Label, check.Group, check.Status, check.Severity, check.Summary, stringValue(check.Detail), check.CheckedAt},
		})
	}

	return statements
}

// evidenceStatement stores a synchronously fetched evidence snapshot.
func evidenceStatement(id string, version int, evidence *contract.Evidence) (storage.TransactionStatement, error) {
	if evidence == nil {
		return storage.TransactionStatement{}, nil
	}

	files, err := json.Marshal(evidence.Files)
	if err != nil {
		return storage.TransactionStatement{}, err
	}

	commits, err := json.Marshal(evidence.Commits)
	if err != nil {
		return storage.TransactionStatement{}, err
	}

	return storage.TransactionStatement{
		SQL: `insert into repo_snapshot (submission_id, version, forge, full_name, default_branch, head_sha,
				file_count, truncated, readme_text, files, commits, fetched_at, error)
			values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '')`,
		Args: []any{id, version, evidence.Forge, evidence.FullName, stringValue(evidence.DefaultBranch),
			stringValue(evidence.HeadSha), len(evidence.Files), boolInt(evidence.Truncated), stringValue(evidence.Readme),
			string(files), string(commits), evidence.FetchedAt},
	}, nil
}
