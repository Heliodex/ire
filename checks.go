package main

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"ire/contract"
)

const (
	githubDefaultAPI = "https://api.github.com"

	repositoryTimeout  = 12 * time.Second
	maxCheckBody       = 8 << 20
	minReadmeLength    = 200
	maxCommitPages     = 100
	maxSnapshotFiles   = 2000
	maxSnapshotCommits = 200
	maxReadmeText      = 200 << 10
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

// githubTreeEntry is one path in the recursive tree.
type githubTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Mode string `json:"mode"`
	Size *int   `json:"size"`
}

// githubTree is the recursive tree response.
type githubTree struct {
	Sha       string            `json:"sha"`
	Tree      []githubTreeEntry `json:"tree"`
	Truncated bool              `json:"truncated"`
	Message   string            `json:"message"`
}

// githubCommit is one commit in the list response.
type githubCommit struct {
	Sha    string `json:"sha"`
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
	http        *http.Client
	base, token string
	cache       *forgeCache
	clock       func() time.Time
}

// now returns the client's clock, falling back to wall time when a test did not set one.
func (c githubClient) now() time.Time {
	if c.clock == nil {
		return time.Now()
	}

	return c.clock()
}

// repoChecks runs the real repository checks and returns the snapshot the evidence tab reuses. Only GitHub is supported in this slice; other forges return a warning so the reviewer knows the repo was not fetched.
func (a *ireApp) repoChecks(ctx context.Context, repoURL, since string, now time.Time, maker []contract.BomLine) ([]contract.CheckResult, *contract.Evidence) {
	checkedAt := now.Format(timestampFormat)

	if strings.TrimSpace(repoURL) == "" {
		return []contract.CheckResult{check("repo.accessible", "Repository reachable", "repo", statusFail, severityBlock, "No repository URL", "Half Life sent no repo_url, so the repository cannot be checked.", checkedAt)}, nil
	}

	owner, name, ok := parseGitHubRepo(repoURL)
	if !ok {
		return []contract.CheckResult{check("repo.accessible", "Repository reachable", "repo", statusWarn, severityWarn, "Only GitHub repositories are checked", "This slice fetches GitHub repositories. Open the link and check the repository by hand.", checkedAt)}, nil
	}

	client := a.githubClient()
	if client.base == "" {
		client.base = githubDefaultAPI
	}

	meta, status, err := client.repo(ctx, owner, name)
	if err != nil || status != http.StatusOK {
		status, summary, detail := repoAccessFailure(status, err)
		return []contract.CheckResult{check("repo.accessible", "Repository reachable", "repo", status, severityBlock, summary, detail, checkedAt)}, nil
	}

	if meta.Private {
		return []contract.CheckResult{check("repo.accessible", "Repository reachable", "repo", statusFail, severityBlock, "Repository is private", "Make the repository public so the board and design files can be reviewed.", checkedAt)}, nil
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
		bom := a.fetchBOM(ctx, client, owner, name, tree)
		if bom.Rows == 0 && len(readme) > 0 {
			// A BOM table in the README is as good as a CSV file, and the price comparison can use it.
			if table := parseBOM("README.md", readme); table.Rows > 0 {
				bom = table
			}
		}

		checks = append(checks, fileChecks(tree, bom, maker, checkedAt)...)
	}

	commits, commitStatus, commitErr := client.commits(ctx, owner, name, since)
	if commitErr == nil && commitStatus == http.StatusOK && len(commits) == 0 && since != "" {
		// Older repositories predate the programme record in Review; keep their history visible rather than reporting an empty repository.
		commits, commitStatus, commitErr = client.commits(ctx, owner, name, "")
	}

	if commitErr != nil || commitStatus != http.StatusOK {
		checks = append(checks, check("repo.commits", "Commit history", "repo", statusError, severityWarn, "Commit history could not be read", "The commit request failed; the reviewer should open the repository.", checkedAt))
	} else {
		checks = append(checks, commitCheck(commits, checkedAt))
	}

	for i := range checks {
		checks[i].Source = stringPtr("forge")
	}

	snapshot := repositoryEvidence(owner, name, meta, readme, readmeStatus, readmeErr, tree, treeStatus, treeErr, commits, commitStatus, commitErr, now)

	return checks, &snapshot
}

// fileChecks checks the file tree for the BOM, design files and placeholder files, and adds the README image count.
func fileChecks(tree githubTree, bom bomSummary, maker []contract.BomLine, checkedAt string) []contract.CheckResult {
	design := classifyDesignFiles(tree)
	designPaths := design.all()

	checks := []contract.CheckResult{bomCheck(bom, checkedAt), bomMatchCheck(bom, maker, checkedAt)}

	if len(designPaths) > 0 {
		checks = append(checks, check("repo.design_files", "Design files", "repo", statusPass, severityInfo, "Design files found", design.summary()+": "+strings.Join(capStrings(designPaths, 6), ", "), checkedAt))
	} else {
		checks = append(checks, check("repo.design_files", "Design files", "repo", statusFail, severityWarn, "No design files", "Add the KiCad, Eagle, EasyEDA, CAD or gerber source files.", checkedAt))
	}

	checks = append(checks, design.kindChecks(checkedAt)...)
	checks = append(checks, emptyFilesCheck(tree, checkedAt))

	return checks
}

// plural returns the plural suffix for a count.
func plural(count int) string {
	if count == 1 {
		return ""
	}

	return "s"
}

// repositoryEvidence builds the cached snapshot shown in the evidence tab.
func repositoryEvidence(owner, name string, meta githubRepo, readme []byte, readmeStatus int, readmeErr error, tree githubTree, treeStatus int, treeErr error, commits []githubCommit, commitStatus int, commitErr error, now time.Time) contract.Evidence {
	fullName := meta.FullName
	if fullName == "" {
		fullName = owner + "/" + name
	}

	evidence := contract.Evidence{
		Commits:   []contract.EvidenceCommit{},
		FetchedAt: now.Format(timestampFormat),
		Files:     []contract.EvidenceFile{},
		Forge:     "github",
		FullName:  fullName,
	}

	if meta.DefaultBranch != "" {
		evidence.DefaultBranch = &meta.DefaultBranch
	}

	if treeErr == nil && treeStatus == http.StatusOK {
		evidence.HeadSha = optionalText(tree.Sha)

		for _, file := range tree.Tree {
			if file.Type != "blob" || file.Path == "" {
				continue
			}

			if len(evidence.Files) >= maxSnapshotFiles {
				evidence.Truncated = true
				break
			}

			evidence.Files = append(evidence.Files, contract.EvidenceFile{Path: file.Path, Kind: fileKind(file.Path)})
		}

		if tree.Truncated {
			evidence.Truncated = true
		}
	}

	if readmeErr == nil && readmeStatus == http.StatusOK {
		text := strings.TrimSpace(string(readme))
		if len(text) > maxReadmeText {
			text = text[:maxReadmeText]
			evidence.Truncated = true
		}

		evidence.Readme = optionalText(text)
	}

	if commitErr == nil && commitStatus == http.StatusOK && len(commits) > 0 {
		evidence.HeadSha = optionalText(commits[0].Sha)
	}

	if commitErr == nil && commitStatus == http.StatusOK {
		for _, commit := range commits {
			if len(evidence.Commits) >= maxSnapshotCommits {
				evidence.Truncated = true
				break
			}

			message := commit.Commit.Message
			if index := strings.IndexByte(message, '\n'); index >= 0 {
				message = message[:index]
			}

			evidence.Commits = append(evidence.Commits, contract.EvidenceCommit{
				Author:  commit.Commit.Author.Name,
				Date:    commit.Commit.Author.Date,
				Message: message,
				Sha:     commit.Sha,
			})
		}
	}

	return evidence
}

// fileKind labels a path for the file tree without guessing at its contents.
func fileKind(path string) string {
	lower := strings.ToLower(path)

	switch {
	case isBOMFile(lower[strings.LastIndex(lower, "/")+1:]):
		return "bom"
	case strings.Contains(lower, "readme"):
		return "readme"
	case isDesignFile(lower):
		return "design"
	case strings.HasSuffix(lower, ".md"):
		return "doc"
	case slices.Contains([]string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg"}, pathExtension(lower)):
		return "image"
	case slices.Contains([]string{".go", ".ts", ".js", ".py", ".c", ".cpp", ".h", ".rs", ".svelte"}, pathExtension(lower)):
		return "code"
	default:
		return "file"
	}
}

// sourceData encodes a check source for the unused data column.
func sourceData(source *string) string {
	if source == nil || strings.TrimSpace(*source) == "" {
		return ""
	}

	data, _ := json.Marshal(map[string]string{"source": *source})

	return string(data)
}

// sourceFromData reads the source from a stored check data value.
func sourceFromData(raw string) *string {
	if raw == "" {
		return nil
	}

	var data struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil || data.Source == "" {
		return nil
	}

	return &data.Source
}

// optionalText returns a pointer for a value the contract treats as absent.
func optionalText(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}

// pathExtension returns the suffix including the dot.
func pathExtension(path string) string {
	index := strings.LastIndex(path, ".")
	if index < 0 {
		return ""
	}

	return path[index:]
}

// file fetches one repository file as raw bytes.
func (c githubClient) file(ctx context.Context, owner, name, path string) ([]byte, int, error) {
	segments := strings.Split(path, "/")
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}

	return c.get(ctx, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name)+"/contents/"+strings.Join(segments, "/"), "application/vnd.github.raw+json")
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
	var count int
	if err == nil && status == http.StatusOK {
		count = imageCount(readme)
	}

	switch {
	case count >= 2:
		return check("repo.readme.images", "README images", "repo", statusPass, severityInfo, fmt.Sprintf("%d images", count), "The README has enough images for a 1st pass.", checkedAt)
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
// isBOMFile matches common bill-of-materials file names.
func isBOMFile(base string) bool {
	base = strings.ToLower(base)
	return strings.HasPrefix(base, "bom") || strings.Contains(base, "bill-of-materials") || strings.Contains(base, "bill_of_materials")
}

// isDesignFile matches PCB, CAD and fabrication files for the evidence tree.
func isDesignFile(path string) bool {
	return isPCBPath(path) || isGerberPath(path) || isCADPath(path)
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
func (c githubClient) commits(ctx context.Context, owner, name, since string) ([]githubCommit, int, error) {
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/commits?per_page=" + strconv.Itoa(maxCommitPages)
	if since != "" {
		path += "&since=" + url.QueryEscape(since)
	}

	body, status, err := c.get(ctx, path, "application/vnd.github+json")
	if err != nil || status != http.StatusOK {
		return nil, status, err
	}

	var commits []githubCommit

	err = json.Unmarshal(body, &commits)

	return commits, status, err
}

// head returns the newest commit on the default branch, used to spot a repository that moved after its snapshot.
func (c githubClient) head(ctx context.Context, owner, name string) (string, int, error) {
	body, status, err := c.get(ctx, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name)+"/commits?per_page=1", "application/vnd.github+json")
	if err != nil || status != http.StatusOK {
		return "", status, err
	}

	var commits []githubCommit
	if err := json.Unmarshal(body, &commits); err != nil {
		return "", status, err
	}

	if len(commits) == 0 {
		return "", status, nil
	}

	return commits[0].Sha, status, nil
}

// githubClient builds the configured client with the app's cache and clock.
func (a *ireApp) githubClient() githubClient {
	base := a.config.GitHubAPIBase
	if base == "" {
		base = githubDefaultAPI
	}

	return githubClient{http: a.client, base: base, token: a.config.GitHubToken, cache: a.forgeCache, clock: a.clock}
}

// get serves a request through the cache when one is configured.
func (c githubClient) get(ctx context.Context, path, accept string) ([]byte, int, error) {
	if c.cache != nil {
		return c.cachedGet(ctx, path, accept)
	}

	body, status, _, err := c.fetch(ctx, path, accept, "")

	return body, status, err
}

// fetch performs one bounded GitHub request, returning the response ETag for revalidation.
func (c githubClient) fetch(ctx context.Context, path, accept, etag string) ([]byte, int, string, error) {
	requestCtx, cancel := context.WithTimeout(ctx, repositoryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, strings.TrimRight(c.base, "/")+path, nil)
	if err != nil {
		return nil, 0, "", err
	}

	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "half-life-ire")
	req.Header.Set("X-Github-Api-Version", "2022-11-28")

	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxCheckBody))

	return body, resp.StatusCode, resp.Header.Get("ETag"), readErr
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
