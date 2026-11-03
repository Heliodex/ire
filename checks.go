package main

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"review/contract"
)

const (
	githubDefaultAPI = "https://api.github.com"

	repositoryTimeout = 12 * time.Second
	maxCheckBody      = 8 << 20
	minReadmeLength   = 200
	maxCommitPages    = 100
)

var readmeImagePattern = regexp.MustCompile(`!\[[^\]]*\]\((https?://[^)\s]+)\)|<img[^>]+src=["'](https?://[^"']+)["']`)

// githubRepo is the subset of the repository metadata the checks need.
type githubRepo struct {
	DefaultBranch string `json:"default_branch"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
	Message       string `json:"message"`
}

// githubTree is the recursive tree response.
type githubTree struct {
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"tree"`
	Truncated bool   `json:"truncated"`
	Message   string `json:"message"`
}

// githubCommit is one commit in the list response.
type githubCommit struct {
	Commit struct {
		Author struct {
			Name string `json:"name"`
			Date string `json:"date"`
		} `json:"author"`
		Message string `json:"message"`
	} `json:"commit"`
}

// githubClient calls the GitHub REST API with the configured token.
type githubClient struct {
	http  *http.Client
	base  string
	token string
}

// repoChecks runs the real repository checks. Only GitHub is supported in this slice; other forges return a warning so the reviewer knows the repo was not fetched.
func (a *reviewApp) repoChecks(ctx context.Context, repoURL string, now time.Time) []contract.CheckResult {
	checkedAt := now.Format(timestampFormat)

	if strings.TrimSpace(repoURL) == "" {
		return []contract.CheckResult{check("repo.accessible", "Repository reachable", "repo", statusFail, severityBlock, "No repository URL", "Half Life sent no repo_url, so the repository cannot be checked.", checkedAt)}
	}

	owner, name, ok := parseGitHubRepo(repoURL)
	if !ok {
		return []contract.CheckResult{check("repo.accessible", "Repository reachable", "repo", statusWarn, severityWarn, "Only GitHub repositories are checked", "This slice fetches GitHub repositories. Open the link and check the repository by hand.", checkedAt)}
	}

	client := githubClient{http: a.client, base: a.config.GitHubAPIBase, token: a.config.GitHubToken}
	if client.base == "" {
		client.base = githubDefaultAPI
	}

	meta, status, err := client.repo(ctx, owner, name)
	if err != nil || status != http.StatusOK {
		status, summary, detail := repoAccessFailure(status, err)
		return []contract.CheckResult{check("repo.accessible", "Repository reachable", "repo", status, severityBlock, summary, detail, checkedAt)}
	}

	if meta.Private {
		return []contract.CheckResult{check("repo.accessible", "Repository reachable", "repo", statusFail, severityBlock, "Repository is private", "Make the repository public so the board and design files can be reviewed.", checkedAt)}
	}

	checks := []contract.CheckResult{
		check("repo.accessible", "Repository reachable", "repo", statusPass, severityInfo, "Public repository found", meta.FullName, checkedAt),
	}

	readme, readmeStatus, readmeErr := client.readme(ctx, owner, name)
	checks = append(checks, readmeCheck(readme, readmeStatus, readmeErr, checkedAt))
	checks = append(checks, imageCheck(readme, readmeStatus, readmeErr, checkedAt))

	tree, treeStatus, treeErr := client.tree(ctx, owner, name, meta.DefaultBranch)
	if treeErr != nil || treeStatus != http.StatusOK {
		checks = append(checks, check("repo.tree", "Repository files", "repo", statusError, severityBlock, "Repository files could not be read", "The file tree request failed; retry the checks before deciding.", checkedAt))
	} else {
		checks = append(checks, fileChecks(tree, checkedAt)...)
	}

	commits, commitStatus, commitErr := client.commits(ctx, owner, name)
	if commitErr != nil || commitStatus != http.StatusOK {
		checks = append(checks, check("repo.commits", "Commit history", "repo", statusError, severityWarn, "Commit history could not be read", "The commit request failed; the reviewer should open the repository.", checkedAt))
	} else {
		checks = append(checks, commitCheck(commits, checkedAt))
	}

	return checks
}

// parseGitHubRepo extracts owner and repository from a GitHub URL.
func parseGitHubRepo(raw string) (string, string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", false
	}

	host := strings.ToLower(parsed.Hostname())
	if host != "github.com" && host != "www.github.com" {
		return "", "", false
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", false
	}

	owner := parts[0]

	name := strings.TrimSuffix(parts[1], ".git")
	if owner == "" || name == "" {
		return "", "", false
	}

	return owner, name, true
}

// repoAccessFailure turns a metadata failure into an accessible check.
func repoAccessFailure(status int, err error) (string, string, string) {
	if status == http.StatusNotFound {
		return statusFail, "Repository is private or missing", "GitHub returned 404 without credentials. Make the repository public and resubmit."
	}

	if status == http.StatusForbidden || status == http.StatusTooManyRequests {
		return statusError, "GitHub rate limit reached", "The repository could not be checked without a token. Retry with a configured GitHub token."
	}

	if err != nil {
		return statusError, "Repository check failed", err.Error()
	}

	return statusError, "Repository check failed", fmt.Sprintf("GitHub returned HTTP %d.", status)
}

// readmeCheck reports README presence and size.
func readmeCheck(body []byte, status int, err error, checkedAt string) contract.CheckResult {
	if err != nil || status != http.StatusOK {
		if status == http.StatusNotFound {
			return check("repo.readme", "README present", "repo", statusFail, severityWarn, "README missing", "Add a README with what the project is, how it works and how to build it.", checkedAt)
		}

		return check("repo.readme", "README present", "repo", statusError, severityWarn, "README could not be read", "The README request failed; the reviewer should open the repository.", checkedAt)
	}

	size := len(strings.TrimSpace(string(body)))
	if size < minReadmeLength {
		return check("repo.readme", "README present", "repo", statusWarn, severityWarn, "README is short", fmt.Sprintf("%d characters; add the build and usage details.", size), checkedAt)
	}

	return check("repo.readme", "README present", "repo", statusPass, severityInfo, fmt.Sprintf("README found (%.1f KB)", float64(size)/1024), "The README explains the project and its build.", checkedAt)
}

// imageCheck reports the README image count.
func imageCheck(readme []byte, status int, err error, checkedAt string) contract.CheckResult {
	count := 0
	if err == nil && status == http.StatusOK {
		count = imageCount(readme)
	}

	switch {
	case count >= 2:
		return check("repo.readme.images", "README images", "repo", statusPass, severityInfo, fmt.Sprintf("%d images", count), "The README has enough images for a first pass.", checkedAt)
	case count == 1:
		return check("repo.readme.images", "README images", "repo", statusWarn, severityWarn, "1 image (2 recommended)", "Add a schematic, board render or build photo.", checkedAt)
	default:
		return check("repo.readme.images", "README images", "repo", statusWarn, severityWarn, "No images", "The README has no images.", checkedAt)
	}
}

// imageCount counts unique image URLs in the README.
func imageCount(readme []byte) int {
	matches := readmeImagePattern.FindAllSubmatch(readme, -1)

	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		url := match[1]
		if len(url) == 0 {
			url = match[2]
		}

		if len(url) > 0 {
			seen[string(url)] = struct{}{}
		}
	}

	return len(seen)
}

// fileChecks checks the file tree for the BOM and design files, and adds the README image count.
func fileChecks(tree githubTree, checkedAt string) []contract.CheckResult {
	var (
		bomFiles    []string
		designFiles []string
	)

	for _, file := range tree.Tree {
		if file.Type != "blob" {
			continue
		}

		base := strings.ToLower(file.Path[strings.LastIndex(file.Path, "/")+1:])
		if isBOMFile(base) {
			bomFiles = append(bomFiles, file.Path)
		}

		if isDesignFile(strings.ToLower(file.Path)) {
			designFiles = append(designFiles, file.Path)
		}
	}

	sort.Strings(bomFiles)
	sort.Strings(designFiles)

	checks := []contract.CheckResult{}

	if len(bomFiles) > 0 {
		checks = append(checks, check("repo.bom", "Bill of materials", "repo", statusPass, severityInfo, bomFiles[0]+" found", strings.Join(bomFiles, ", "), checkedAt))
	} else {
		checks = append(checks, check("repo.bom", "Bill of materials", "repo", statusFail, severityWarn, "No BOM file", "Add a BOM with part, quantity, unit price and vendor.", checkedAt))
	}

	if len(designFiles) > 0 {
		checks = append(checks, check("repo.design_files", "Design files", "repo", statusPass, severityInfo, "Design files found", strings.Join(capStrings(designFiles, 6), ", "), checkedAt))
	} else {
		checks = append(checks, check("repo.design_files", "Design files", "repo", statusFail, severityWarn, "No design files", "Add the KiCad, Eagle or gerber source files.", checkedAt))
	}

	return checks
}

// isBOMFile matches common bill-of-materials file names.
func isBOMFile(base string) bool {
	base = strings.ToLower(base)
	return strings.HasPrefix(base, "bom") || strings.Contains(base, "bill-of-materials") || strings.Contains(base, "bill_of_materials")
}

// isDesignFile matches PCB source, schematic and fabrication files.
func isDesignFile(path string) bool {
	for _, suffix := range []string{".kicad_pcb", ".kicad_sch", ".sch", ".brd", ".gbr", ".drl", ".zip", ".step", ".stl"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}

	return strings.Contains(path, "gerber") || strings.Contains(path, "drill")
}

// commitCheck reports commit count, days and authors.
func commitCheck(commits []githubCommit, checkedAt string) contract.CheckResult {
	if len(commits) == 0 {
		return check("repo.commits", "Commit history", "repo", statusFail, severityWarn, "No commits found", "The repository has no commits the forge can see.", checkedAt)
	}

	days := make(map[string]struct{}, len(commits))
	authors := make(map[string]struct{}, len(commits))

	for _, commit := range commits {
		if len(commit.Commit.Author.Date) >= 10 {
			days[commit.Commit.Author.Date[:10]] = struct{}{}
		}

		if commit.Commit.Author.Name != "" {
			authors[commit.Commit.Author.Name] = struct{}{}
		}
	}

	names := make([]string, 0, len(authors))
	for name := range authors {
		names = append(names, name)
	}

	sort.Strings(names)

	summary := fmt.Sprintf("%d commits over %d days", len(commits), len(days))
	detail := "Authors: " + strings.Join(capStrings(names, 5), ", ")

	return check("repo.commits", "Commit history", "repo", statusPass, severityInfo, summary, detail, checkedAt)
}

// capStrings keeps at most n values.
func capStrings(values []string, n int) []string {
	if len(values) <= n {
		return values
	}

	return append(values[:n:n], "…")
}

// repo fetches repository metadata.
func (c githubClient) repo(ctx context.Context, owner, name string) (githubRepo, int, error) {
	var meta githubRepo

	body, status, err := c.get(ctx, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name), "application/vnd.github+json")
	if err != nil || status != http.StatusOK {
		return meta, status, err
	}

	err = json.Unmarshal(body, &meta)

	return meta, status, err
}

// readme fetches the raw README.
func (c githubClient) readme(ctx context.Context, owner, name string) ([]byte, int, error) {
	return c.get(ctx, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name)+"/readme", "application/vnd.github.raw+json")
}

// tree fetches the recursive file tree for the default branch.
func (c githubClient) tree(ctx context.Context, owner, name, branch string) (githubTree, int, error) {
	if strings.TrimSpace(branch) == "" {
		branch = "main"
	}

	body, status, err := c.get(ctx, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name)+"/git/trees/"+url.PathEscape(branch)+"?recursive=1", "application/vnd.github+json")

	var tree githubTree
	if err != nil || status != http.StatusOK {
		return tree, status, err
	}

	err = json.Unmarshal(body, &tree)

	return tree, status, err
}

// commits fetches the latest commits.
func (c githubClient) commits(ctx context.Context, owner, name string) ([]githubCommit, int, error) {
	body, status, err := c.get(ctx, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name)+"/commits?per_page="+strconv.Itoa(maxCommitPages), "application/vnd.github+json")
	if err != nil || status != http.StatusOK {
		return nil, status, err
	}

	var commits []githubCommit

	err = json.Unmarshal(body, &commits)

	return commits, status, err
}

// get performs one bounded GitHub request.
func (c githubClient) get(ctx context.Context, path, accept string) ([]byte, int, error) {
	requestCtx, cancel := context.WithTimeout(ctx, repositoryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, strings.TrimRight(c.base, "/")+path, nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "half-life-review")
	req.Header.Set("X-Github-Api-Version", "2022-11-28")

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxCheckBody))

	return body, resp.StatusCode, readErr
}

// mergeChecks overlays forge checks on payload checks by key, so a real check replaces its payload approximation.
func mergeChecks(base, overlay []contract.CheckResult) []contract.CheckResult {
	out := append([]contract.CheckResult{}, base...)
	index := make(map[string]int, len(out))

	for i := range out {
		index[out[i].Key] = i
	}

	for _, result := range overlay {
		if i, ok := index[result.Key]; ok {
			out[i] = result
			continue
		}

		index[result.Key] = len(out)
		out = append(out, result)
	}

	return out
}
