package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"ire/contract"
)

// TestPathExtension covers the suffix helper behind fileKind.
func TestPathExtension(t *testing.T) {
	cases := []struct{ in, want string }{
		{in: "board.kicad_pcb", want: ".kicad_pcb"},
		{in: "dir/archive.tar.gz", want: ".gz"},
		{in: "noext", want: ""},
		{in: "", want: ""},
	}

	for _, test := range cases {
		if got := pathExtension(test.in); got != test.want {
			t.Errorf("pathExtension(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

// TestFileKind covers the evidence tree labels.
func TestFileKind(t *testing.T) {
	cases := []struct{ in, want string }{
		{in: "BOM.csv", want: "bom"},
		{in: "docs/bill-of-materials.csv", want: "bom"},
		{in: "README.md", want: "readme"},
		{in: "board.kicad_pcb", want: "design"},
		{in: "gerbers/board-F_Cu.gtl", want: "design"},
		{in: "docs/guide.md", want: "doc"},
		{in: "img/photo.png", want: "image"},
		{in: "src/main.go", want: "code"},
		{in: "notes.txt", want: "file"},
	}

	for _, test := range cases {
		if got := fileKind(test.in); got != test.want {
			t.Errorf("fileKind(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

// TestParseGitHubRepo covers URL parsing for the repository checks.
func TestParseGitHubRepo(t *testing.T) {
	owner, name, ok := parseGitHubRepo("https://github.com/hackclub/ire")
	if !ok || owner != "hackclub" || name != "ire" {
		t.Errorf("parseGitHubRepo = %q %q %v, want hackclub ire true", owner, name, ok)
	}

	owner, name, ok = parseGitHubRepo("https://github.com/hackclub/ire.git")
	if !ok || name != "ire" {
		t.Errorf("parseGitHubRepo(.git) = %q %q %v", owner, name, ok)
	}

	for _, raw := range []string{"", "https://gitlab.com/a/b", "https://github.com/onlyone", "https://github.com//"} {
		if _, _, ok := parseGitHubRepo(raw); ok {
			t.Errorf("parseGitHubRepo(%q) = true, want false", raw)
		}
	}
}

// TestRepoAccessFailure covers the 404, rate-limit and generic branches.
func TestRepoAccessFailure(t *testing.T) {
	if _, summary, _ := repoAccessFailure(http.StatusNotFound, nil); summary == "" {
		t.Error("404 should have a summary")
	}

	if _, summary, _ := repoAccessFailure(http.StatusTooManyRequests, nil); summary == "" {
		t.Error("429 should have a summary")
	}

	if status, _, _ := repoAccessFailure(0, errors.New("boom")); status != statusError {
		t.Errorf("error status = %q, want error", status)
	}

	if _, _, detail := repoAccessFailure(http.StatusBadGateway, nil); detail == "" {
		t.Error("generic failure should have detail")
	}
}

// TestReadmeCheck covers missing, unreadable, short and full READMEs.
func TestReadmeCheck(t *testing.T) {
	if got := readmeCheck(nil, http.StatusNotFound, nil, "now"); got.Status != statusFail {
		t.Errorf("missing readme = %q, want fail", got.Status)
	}

	if got := readmeCheck(nil, http.StatusBadGateway, errors.New("boom"), "now"); got.Status != statusError {
		t.Errorf("failed readme = %q, want error", got.Status)
	}

	if got := readmeCheck([]byte("short"), http.StatusOK, nil, "now"); got.Status != statusWarn {
		t.Errorf("short readme = %q, want warn", got.Status)
	}

	long := make([]byte, minReadmeLength+10)
	for i := range long {
		long[i] = 'a'
	}

	if got := readmeCheck(long, http.StatusOK, nil, "now"); got.Status != statusPass {
		t.Errorf("long readme = %q, want pass", got.Status)
	}
}

// TestImageAndCommitChecks covers the small pure check builders.
func TestImageAndCommitChecks(t *testing.T) {
	if got := imageCheck([]byte("no images"), http.StatusOK, nil, "now"); got.Status != statusWarn {
		t.Errorf("imageless readme = %q, want warn", got.Status)
	}

	if count := imageCount([]byte("![a](https://example.com/a.png) ![a](https://example.com/a.png)")); count != 1 {
		t.Errorf("imageCount dedup = %d, want 1", count)
	}

	if got := commitCheck(nil, "now"); got.Status != statusFail {
		t.Errorf("empty commits = %q, want fail", got.Status)
	}
}

// TestCheckSmallHelpers covers plural, capStrings and source round-trips.
func TestCheckSmallHelpers(t *testing.T) {
	if plural(1) != "" || plural(2) != "s" {
		t.Errorf("plural = %q %q", plural(1), plural(2))
	}

	if got := capStrings([]string{"a", "b", "c"}, 2); len(got) != 3 || got[2] != "…" {
		t.Errorf("capStrings = %v", got)
	}

	if sourceData(nil) != "" {
		t.Error("nil source should encode empty")
	}

	source := "forge"
	encoded := sourceData(&source)

	decoded := sourceFromData(encoded)
	if decoded == nil || *decoded != "forge" {
		t.Errorf("source round-trip = %v", decoded)
	}

	if sourceFromData("not-json") != nil || sourceFromData("") != nil {
		t.Error("bad source data should decode to nil")
	}

	if optionalText("") != nil || *optionalText("x") != "x" {
		t.Error("optionalText should map empty to nil")
	}
}

// TestParseSearchLimit covers the dropdown page-size clamping.
func TestParseSearchLimit(t *testing.T) {
	cases := []struct {
		query string
		want  int
	}{
		{query: "", want: searchLimit},
		{query: "not-a-number", want: searchLimit},
		{query: "0", want: searchLimit},
		{query: "5", want: 5},
		{query: "99", want: maxSearchLimit},
	}

	for _, test := range cases {
		request := httptest.NewRequest(http.MethodGet, "/search?limit="+test.query, nil)
		if got := parseSearchLimit(request); got != test.want {
			t.Errorf("parseSearchLimit(%q) = %d, want %d", test.query, got, test.want)
		}
	}
}

// TestParseLimitOffsetSince covers the shared pagination helpers.
func TestParseLimitOffsetSince(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/queue", nil)
	if got := parseLimit(request); got != defaultLimit {
		t.Errorf("default limit = %d, want %d", got, defaultLimit)
	}

	request = httptest.NewRequest(http.MethodGet, "/queue?limit=5&offset=3", nil)
	if got := parseLimit(request); got != 5 {
		t.Errorf("limit = %d, want 5", got)
	}

	if got := parseOffset(request); got != 3 {
		t.Errorf("offset = %d, want 3", got)
	}

	request = httptest.NewRequest(http.MethodGet, "/queue?limit=99999&offset=-1", nil)
	if got := parseLimit(request); got != maxLimit {
		t.Errorf("clamped limit = %d, want %d", got, maxLimit)
	}

	if got := parseOffset(request); got != 0 {
		t.Errorf("negative offset = %d, want 0", got)
	}

	request = httptest.NewRequest(http.MethodGet, "/queue", nil)
	if value, ok := parseSince(request); value != "" || !ok {
		t.Errorf("empty since = %q %v", value, ok)
	}

	request = httptest.NewRequest(http.MethodGet, "/queue?since=nope", nil)
	if _, ok := parseSince(request); ok {
		t.Error("invalid since should be false")
	}

	request = httptest.NewRequest(http.MethodGet, "/queue?since=2026-01-01T00:00:00Z", nil)
	if value, ok := parseSince(request); !ok || value == "" {
		t.Errorf("valid since = %q %v", value, ok)
	}
}

// TestProtectedPage covers the auth gate list.
func TestProtectedPage(t *testing.T) {
	for _, path := range []string{"/overview", "/queue", "/review/123", "/makers/a%40example.com", "/admin/members"} {
		if !protectedPage(path) {
			t.Errorf("protectedPage(%q) = false, want true", path)
		}
	}

	if protectedPage("/api/v1/me") {
		t.Error("API route should not be a protected page")
	}
}

// TestIngestScalars covers the nil-safe conversions.
func TestIngestScalars(t *testing.T) {
	if firstLine("") != "Work session" {
		t.Errorf("empty firstLine = %q", firstLine(""))
	}

	if got := firstLine("# Title\nsecond"); got != "Title" {
		t.Errorf("firstLine = %q, want Title", got)
	}

	if nullString("") != nil || nullString("x") != "x" {
		t.Error("nullString mismatch")
	}

	if nullInt(nil) != nil {
		t.Error("nullInt(nil) should be nil")
	}

	value := 7
	if nullInt(&value) != 7 || intValue(nil) != 0 || intValue(&value) != 7 {
		t.Error("int helpers mismatch")
	}

	if stringValue(nil) != "" {
		t.Error("stringValue(nil) should be empty")
	}

	if absInt64(-3) != 3 || absInt64(3) != 3 {
		t.Error("absInt64 mismatch")
	}
}

// TestLocalCheckHelpers covers the offline check scalars.
func TestLocalCheckHelpers(t *testing.T) {
	if pluralCount(1, "entry", "entries") != "1 entry" || pluralCount(2, "entry", "entries") != "2 entries" {
		t.Error("pluralCount mismatch")
	}

	if descriptionLength(10) == "" {
		t.Error("descriptionLength should explain the minimum")
	}

	if !validHTTPSURL("https://example.com/clip") || validHTTPSURL("http://example.com") || validHTTPSURL("nope") {
		t.Error("validHTTPSURL mismatch")
	}

	if !validHTTPURL("http://example.com") || validHTTPURL("ftp://example.com") {
		t.Error("validHTTPURL mismatch")
	}
}

// TestDemoResultMapping covers the probe status mapping.
func TestDemoResultMapping(t *testing.T) {
	if got := demoResult(http.StatusOK, "now"); got.Status != statusPass {
		t.Errorf("200 demo = %q, want pass", got.Status)
	}

	if got := demoResult(http.StatusNotFound, "now"); got.Status != statusWarn {
		t.Errorf("404 demo = %q, want warn", got.Status)
	}

	if got := demoResult(http.StatusTeapot, "now"); got.Status != statusWarn {
		t.Errorf("418 demo = %q, want warn", got.Status)
	}
}

// TestIDHelpers covers the identifier utilities.
func TestIDHelpers(t *testing.T) {
	seen := map[string]bool{}

	for range 10 {
		id := newID("evt")
		if len(id) < 5 || seen[id] {
			t.Fatalf("newID = %q, want unique prefixed ids", id)
		}

		seen[id] = true
	}

	if len(sha256Hex([]byte("x"))) != 64 {
		t.Error("sha256Hex should be 64 hex chars")
	}
}

// TestTimelapseAndJournalScalars covers the ingest record helpers.
func TestTimelapseAndJournalScalars(t *testing.T) {
	provider, external, url, covered := "youtube", "clip1", "https://example.com/c", 30
	entry := contract.IngestJournal{
		Timelapses: []contract.IngestRequestJournalsElemTimelapsesElem{
			{Provider: &provider, ExternalID: &external, URL: &url, CoveredSeconds: &covered},
			{},
		},
	}

	records := timelapseRecords(entry)
	if len(records) != 2 || records[0]["provider"] != "youtube" || records[0]["coveredSeconds"] != 30 {
		t.Errorf("timelapseRecords = %v", records)
	}

	if len(records[1]) != 0 {
		t.Errorf("empty clip should give empty record, got %v", records[1])
	}

	seconds, minutes := 90, 2
	if journalSeconds(contract.IngestJournal{Seconds: &seconds}) != 90 {
		t.Error("journalSeconds should prefer seconds")
	}

	if journalSeconds(contract.IngestJournal{Minutes: &minutes}) != 120 {
		t.Error("journalSeconds should fall back to minutes")
	}

	if journalSeconds(contract.IngestJournal{}) != 0 {
		t.Error("journalSeconds should default to zero")
	}

	negative := -5
	if journalSeconds(contract.IngestJournal{Seconds: &negative}) != 0 {
		t.Error("journalSeconds should clamp negatives")
	}

	if !isBOMFile("BOM.csv") || !isBOMFile("bill_of_materials.xlsx") || isBOMFile("board.kicad_pcb") {
		t.Error("isBOMFile mismatch")
	}

	if !isDesignFile("board.kicad_pcb") || !isDesignFile("gerbers/board-F_Cu.gtl") || isDesignFile("notes.txt") {
		t.Error("isDesignFile mismatch")
	}
}
