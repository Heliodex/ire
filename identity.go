package main

import (
	"context"
	"strings"
	"time"

	"review/contract"
)

// duplicateChecks compares a new ship with earlier ones in the same programme.
func (a *reviewApp) duplicateChecks(ctx context.Context, programID string, request contract.IngestRequest, now time.Time) []contract.CheckResult {
	checkedAt := now.Format(timestampFormat)
	repo := strings.ToLower(strings.TrimSpace(stringValue(request.RepoURL)))
	demo := strings.ToLower(strings.TrimSpace(stringValue(request.DemoURL)))
	maker := strings.ToLower(strings.TrimSpace(request.Maker.Email))
	hackatime := strings.TrimSpace(stringValue(request.Maker.HackatimeID))
	slack := strings.TrimSpace(stringValue(request.Maker.SlackID))

	rows, err := a.store.Query(ctx, `select external_id, title, coalesce(repo_url, ''), coalesce(demo_url, ''),
			lower(maker_email), coalesce(maker_hackatime_id, ''), coalesce(maker_slack_id, '')
		from submission where program_id = ? and status != 'withdrawn' and external_id != ?
		order by ingested_at desc limit 500`, programID, request.ExternalID)
	if err != nil {
		return []contract.CheckResult{
			check("duplicate.project", "Duplicate project", "duplicate", statusError, severityWarn, "Could not compare earlier ships", err.Error(), checkedAt),
		}
	}
	defer rows.Close()

	var (
		repoMatches    []string
		demoMatches    []string
		hackatimeReuse []string
		slackReuse     []string
	)

	for rows.Next() {
		var externalID, title, otherRepo, otherDemo, otherEmail, otherHackatime, otherSlack string

		if err := rows.Scan(&externalID, &title, &otherRepo, &otherDemo, &otherEmail, &otherHackatime, &otherSlack); err != nil {
			continue
		}

		label := title + " (" + externalID + ")"

		if repo != "" && strings.ToLower(otherRepo) == repo {
			repoMatches = append(repoMatches, label)
		}

		if demo != "" && strings.ToLower(otherDemo) == demo {
			demoMatches = append(demoMatches, label)
		}

		if hackatime != "" && otherHackatime == hackatime && otherEmail != "" && otherEmail != maker {
			hackatimeReuse = append(hackatimeReuse, label)
		}

		if slack != "" && otherSlack == slack && otherEmail != "" && otherEmail != maker {
			slackReuse = append(slackReuse, label)
		}
	}

	if err := rows.Err(); err != nil {
		return []contract.CheckResult{
			check("duplicate.project", "Duplicate project", "duplicate", statusError, severityWarn, "Could not compare earlier ships", err.Error(), checkedAt),
		}
	}

	checks := make([]contract.CheckResult, 0, 2)
	checks = append(checks, duplicateProjectCheck(repoMatches, demoMatches, checkedAt))
	checks = append(checks, identityMatchCheck(hackatimeReuse, slackReuse, checkedAt))

	return checks
}

// duplicateProjectCheck reports reused repository or demo links.
func duplicateProjectCheck(repos, demos []string, checkedAt string) contract.CheckResult {
	details := []string{}

	if len(repos) > 0 {
		details = append(details, "Repository also used by "+strings.Join(capStrings(repos, 3), ", "))
	}

	if len(demos) > 0 {
		details = append(details, "Demo also used by "+strings.Join(capStrings(demos, 3), ", "))
	}

	if len(details) == 0 {
		result := check("duplicate.project", "Duplicate project", "duplicate", statusPass, severityInfo, "No reused repository or demo", "The repository and demo do not match another ship.", checkedAt)
		result.Source = stringPtr("payload")

		return result
	}

	result := check("duplicate.project", "Duplicate project", "duplicate", statusWarn, severityWarn,
		"Repository or demo reused", strings.Join(details, "; ")+". Confirm this is a new phase or a deliberate rebuild.", checkedAt)
	result.Source = stringPtr("payload")

	return result
}

// identityMatchCheck reports a Hackatime or Slack identity used by another maker email.
func identityMatchCheck(hackatime, slack []string, checkedAt string) contract.CheckResult {
	details := []string{}

	if len(hackatime) > 0 {
		details = append(details, "Hackatime identity also used by "+strings.Join(capStrings(hackatime, 3), ", "))
	}

	if len(slack) > 0 {
		details = append(details, "Slack identity also used by "+strings.Join(capStrings(slack, 3), ", "))
	}

	if len(details) == 0 {
		result := check("identity.match", "Maker identity", "identity", statusPass, severityInfo, "No identity reuse", "The maker identity is not reused by another ship.", checkedAt)
		result.Source = stringPtr("payload")

		return result
	}

	result := check("identity.match", "Maker identity", "identity", statusWarn, severityWarn,
		"Identity reused across maker emails", strings.Join(details, "; ")+". Confirm the account is not shared.", checkedAt)
	result.Source = stringPtr("payload")

	return result
}
