package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"review/contract"
)

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

	shortEntries, wordless, clips, clipLinks, coveredSeconds, journalTime := 0, 0, 0, 0, 0, 0
	invalidClips := []string{}

	for _, entry := range request.Journals {
		images += len(entry.Media)
		journalTime += journalSeconds(entry)

		words := len(strings.Fields(entry.Text))
		if words < 15 && (len(entry.Timelapses) > 0 || len(entry.Media) > 0) {
			wordless++
		}

		for _, clip := range entry.Timelapses {
			clips++

			if clip.CoveredSeconds != nil {
				coveredSeconds += max(*clip.CoveredSeconds, 0)
			}

			if clip.URL != nil && strings.TrimSpace(*clip.URL) != "" {
				clipLinks++

				if !validHTTPSURL(*clip.URL) {
					invalidClips = append(invalidClips, *clip.URL)
				}
			}
		}

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
		checks = append(checks, check("evidence.journals", "Journal evidence", "evidence", statusWarn, severityWarn, pluralCount(shortEntries, "short entry", "short entries"), "Some entries lack enough detail to justify their time.", checkedAt))
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

	checks = append(checks, journalChecks(request.Journals, journalTime, claimed, wordless, checkedAt)...)
	checks = append(checks, clipChecks(clips, clipLinks, coveredSeconds, claimed, invalidClips, checkedAt)...)

	for i := range checks {
		checks[i].Source = stringPtr("payload")
	}

	return checks
}

// journalChecks reports the written evidence and how its time compares with the claim.
func journalChecks(entries []contract.IngestJournal, journalTime, claimed, wordless int, checkedAt string) []contract.CheckResult {
	checks := []contract.CheckResult{}

	if len(entries) == 0 {
		checks = append(checks, check("journal.present", "Journal entries", "evidence", statusFail, severityWarn, "No journal entries", "There is no written evidence to read.", checkedAt))
	} else {
		checks = append(checks, check("journal.present", "Journal entries", "evidence", statusPass, severityInfo,
			pluralCount(len(entries), "entry", "entries"), "The maker sent written journal entries.", checkedAt))
	}

	if len(entries) > 0 {
		status, severity := statusPass, severityInfo
		summary := "Every entry has words"

		if wordless > 0 {
			status, severity = statusWarn, severityWarn
			summary = pluralCount(wordless, "clip-only entry", "clip-only entries")
		}

		checks = append(checks, check("journal.wordless", "Journal words", "evidence", status, severity, summary,
			"Clip-only entries are evidence, but they do not explain the work in words.", checkedAt))

		hoursStatus, hoursSeverity := statusPass, severityInfo
		hoursSummary := formatHours(journalTime) + " logged"

		switch {
		case claimed == 0:
			hoursStatus, hoursSeverity = statusSkip, severityInfo
			hoursSummary = "No claimed time"
		case journalTime == 0:
			hoursStatus, hoursSeverity = statusWarn, severityWarn
			hoursSummary = "No journal time"
		case journalTime*2 < claimed:
			hoursStatus, hoursSeverity = statusWarn, severityWarn
			hoursSummary = formatHours(journalTime) + " of " + formatHours(claimed)
		}

		checks = append(checks, check("journal.hours", "Journal time", "evidence", hoursStatus, hoursSeverity, hoursSummary,
			"Journal time is compared with the claim; it is not added to Hackatime time again.", checkedAt))
	}

	return checks
}

// clipChecks reports clip presence, coverage and link validity.
func clipChecks(clips, links, covered, claimed int, invalid []string, checkedAt string) []contract.CheckResult {
	if clips == 0 {
		return []contract.CheckResult{
			check("lapse.clips", "Timelapses", "evidence", statusSkip, severityInfo, "No timelapses", "The ship carries no timelapse clips.", checkedAt),
			check("lapse.coverage", "Timelapse coverage", "evidence", statusSkip, severityInfo, "No coverage to measure", "There are no clips to compare with the claim.", checkedAt),
			check("lapse.links", "Timelapse links", "evidence", statusSkip, severityInfo, "No clip links", "There are no clip URLs to validate.", checkedAt),
		}
	}

	checks := []contract.CheckResult{
		check("lapse.clips", "Timelapses", "evidence", statusPass, severityInfo, fmt.Sprintf("%d clip%s", clips, plural(clips)), "The ship carries timelapse evidence.", checkedAt),
	}

	if claimed > 0 {
		ratio := float64(covered) / float64(claimed)
		if ratio > 1 {
			ratio = 1
		}

		status, severity := statusPass, severityInfo
		if ratio < minClipCoverage {
			status, severity = statusWarn, severityWarn
		}

		checks = append(checks, check("lapse.coverage", "Timelapse coverage", "evidence", status, severity,
			fmt.Sprintf("%.0f%% covered", ratio*100),
			fmt.Sprintf("%s covered of %s claimed.", formatHours(covered), formatHours(claimed)), checkedAt))
	} else {
		checks = append(checks, check("lapse.coverage", "Timelapse coverage", "evidence", statusSkip, severityInfo, "No claimed time", "There is no claim to measure coverage against.", checkedAt))
	}

	switch {
	case len(invalid) > 0:
		checks = append(checks, check("lapse.links", "Timelapse links", "evidence", statusError, severityWarn,
			fmt.Sprintf("%d clip link%s could not be used", len(invalid), plural(len(invalid))),
			"Use an HTTPS clip URL: "+strings.Join(capStrings(invalid, 3), ", "), checkedAt))
	case links == 0:
		checks = append(checks, check("lapse.links", "Timelapse links", "evidence", statusSkip, severityInfo, "No clip URLs", "The clips carry no direct link to validate.", checkedAt))
	default:
		checks = append(checks, check("lapse.links", "Timelapse links", "evidence", statusPass, severityInfo, "Clip links are HTTPS", "Every clip carries a usable HTTPS URL.", checkedAt))
	}

	return checks
}

// pluralCount returns a count with its singular or plural noun.
func pluralCount(count int, singular, plural string) string {
	if count == 1 {
		return strconv.Itoa(count) + " " + singular
	}

	return strconv.Itoa(count) + " " + plural
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

// validHTTPSURL reports whether value parses as an absolute https URL.
func validHTTPSURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}

	return parsed.Scheme == "https" && parsed.Host != ""
}

// validHTTPURL reports whether value parses as an absolute http or https URL.
func validHTTPURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}

	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
