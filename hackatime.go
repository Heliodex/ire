package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ire/contract"
)

const (
	hackatimeDefaultAPI = "https://hackatime.hackclub.com"
	hackatimeTimeout    = 10 * time.Second
	hackatimeCacheTTL   = 5 * time.Minute
	maxHackatimeBody    = 1 << 20
)

// hackatimeStats is the project list return shape of the public stats endpoint.
type hackatimeStats struct {
	Data struct {
		Projects []struct {
			Name         string  `json:"name"`
			TotalSeconds float64 `json:"total_seconds"`
		} `json:"projects"`
	} `json:"data"`
}

// hackatimeLink is one project linked to a ship and its cached seconds.
type hackatimeLink struct {
	Project   string
	Seconds   int
	FetchedAt string
	Error     string
}

// hackatimeChecks reads the linked projects from Hackatime and reports what it could verify.
func (a *ireApp) hackatimeChecks(ctx context.Context, ship submissionRow) []contract.CheckResult {
	checkedAt := a.nowText()

	links, err := a.loadHackatimeLinks(ctx, ship.ID)
	if err != nil {
		return []contract.CheckResult{check("hackatime.linked", "Hackatime linked", "evidence", statusError, severityWarn, "Hackatime links could not be read", err.Error(), checkedAt)}
	}

	if ship.MakerHackatim == "" {
		if len(links) == 0 {
			return []contract.CheckResult{check("hackatime.linked", "Hackatime linked", "evidence", statusSkip, severityInfo, "No Hackatime id", "Half Life sent no Hackatime identity, so tracked time cannot be checked.", checkedAt)}
		}

		return []contract.CheckResult{check("hackatime.linked", "Hackatime linked", "evidence", statusWarn, severityWarn, "Projects linked without a Hackatime id", "Add the maker's Hackatime identity to verify tracked time.", checkedAt)}
	}

	if len(links) == 0 {
		return []contract.CheckResult{check("hackatime.linked", "Hackatime linked", "evidence", statusSkip, severityInfo, "No Hackatime projects linked", "The maker linked no Hackatime projects, so there is no tracked time to compare.", checkedAt)}
	}

	total, stale, failures := a.refreshHackatimeLinks(ctx, ship, links)

	checks := []contract.CheckResult{
		check("hackatime.linked", "Hackatime linked", "evidence", statusPass, severityInfo,
			fmt.Sprintf("%d project%s linked", len(links), plural(len(links))), strings.Join(hackatimeProjectDetails(links), ", "), checkedAt),
	}

	timeStatus, timeSeverity := statusPass, severityInfo

	timeSummary := formatHours(total) + " tracked all-time"
	if total == 0 {
		timeStatus, timeSeverity = statusWarn, severityWarn
		timeSummary = "No tracked time"
	}

	timeDetail := fmt.Sprintf("All-time project totals; %s claimed. Scope the project to the phase week before comparing directly. %s",
		formatHours(ship.HoursClaimed), strings.Join(hackatimeProjectDetails(links), ", "))
	checks = append(checks, check("hackatime.time", "Hackatime time", "evidence", timeStatus, timeSeverity, timeSummary, timeDetail, checkedAt))

	if len(stale) == 0 {
		checks = append(checks, check("hackatime.stale", "Hackatime freshness", "evidence", statusPass, severityInfo, "Tracked time is current", "Every linked project was refreshed.", checkedAt))
	} else {
		checks = append(checks, check("hackatime.stale", "Hackatime freshness", "evidence", statusError, severityWarn,
			fmt.Sprintf("%d project%s could not be refreshed", len(stale), plural(len(stale))),
			strings.Join(failures, "; ")+"; cached values are shown where available", checkedAt))
	}

	for i := range checks {
		checks[i].Source = stringPtr("forge")
	}

	return checks
}

// loadHackatimeLinks reads the linked projects for a ship.
func (a *ireApp) loadHackatimeLinks(ctx context.Context, submissionID string) ([]hackatimeLink, error) {
	rows, err := a.store.Query(ctx, `select project, seconds, coalesce(fetched_at, ''), coalesce(error, '')
		from hackatime_link where submission_id = ? order by project`, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []hackatimeLink

	for rows.Next() {
		var link hackatimeLink
		if err := rows.Scan(&link.Project, &link.Seconds, &link.FetchedAt, &link.Error); err != nil {
			return nil, err
		}

		links = append(links, link)
	}

	return links, rows.Err()
}

// refreshHackatimeLinks refreshes each project, returning the total, stale projects and their errors.
func (a *ireApp) refreshHackatimeLinks(ctx context.Context, ship submissionRow, links []hackatimeLink) (int, []string, []string) {
	var (
		total    int
		stale    []string
		failures []string
	)

	for i := range links {
		link := &links[i]

		if a.hackatimeFresh(link) {
			total += link.Seconds
			continue
		}

		seconds, err := a.fetchHackatimeProject(ctx, ship.MakerHackatim, link.Project)
		if err != nil {
			stale = append(stale, link.Project)
			failures = append(failures, link.Project+": "+err.Error())
			total += link.Seconds

			_, _ = a.store.Exec(ctx, `update hackatime_link set error = ? where submission_id = ? and project = ?`, err.Error(), ship.ID, link.Project)

			continue
		}

		link.Seconds = seconds
		link.Error = ""
		link.FetchedAt = a.nowText()
		total += seconds

		_, _ = a.store.Exec(ctx, `update hackatime_link set seconds = ?, fetched_at = ?, error = null where submission_id = ? and project = ?`,
			seconds, link.FetchedAt, ship.ID, link.Project)
	}

	return total, stale, failures
}

// hackatimeFresh reports whether a cached value is still inside the five-minute window.
func (a *ireApp) hackatimeFresh(link *hackatimeLink) bool {
	if link.Error != "" || link.FetchedAt == "" {
		return false
	}

	fetched, err := time.Parse(timestampFormat, link.FetchedAt)
	if err != nil {
		return false
	}

	return a.now().Sub(fetched) < hackatimeCacheTTL
}

// fetchHackatimeProject reads one project's all-time seconds from the public stats endpoint.
func (a *ireApp) fetchHackatimeProject(ctx context.Context, userID, project string) (int, error) {
	base := strings.TrimSpace(a.config.HackatimeAPIBase)
	if base == "" {
		base = hackatimeDefaultAPI
	}

	requestURL := strings.TrimRight(base, "/") + "/api/v1/users/" + url.PathEscape(userID) + "/stats?features=projects"

	requestCtx, cancel := context.WithTimeout(ctx, hackatimeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "half-life-ire")

	resp, err := a.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("hackatime returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHackatimeBody))
	if err != nil {
		return 0, err
	}

	var stats hackatimeStats
	if err := json.Unmarshal(body, &stats); err != nil {
		return 0, fmt.Errorf("hackatime response could not be read: %w", err)
	}

	if stats.Data.Projects == nil {
		return 0, errors.New("hackatime returned no project list")
	}

	for _, entry := range stats.Data.Projects {
		if entry.Name == project {
			return int(max(entry.TotalSeconds, 0)), nil
		}
	}

	return 0, fmt.Errorf("hackatime does not list a project named %q", project)
}

// hackatimeProjectDetails renders the per-project totals for a check detail.
func hackatimeProjectDetails(links []hackatimeLink) []string {
	out := make([]string, 0, len(links))

	for _, link := range links {
		out = append(out, link.Project+" "+formatHours(link.Seconds))
	}

	return out
}
