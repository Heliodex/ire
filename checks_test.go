package main

import (
	"strings"
	"testing"
	"time"

	"ire/contract"
)

// TestParseBOMCSV covers quoted names, quantities and prices.
func TestParseBOMCSV(t *testing.T) {
	bom := parseBOM("BOM.csv", []byte("Part,Qty,Unit Price,Vendor\n\"XIAO, Seeed\",2,4.99,Seeed\nResistor,10,0.02,DigiKey\n"))

	if bom.Rows != 2 || len(bom.Parts) != 2 {
		t.Fatalf("rows = %d, parts = %+v", bom.Rows, bom.Parts)
	}

	if !bom.HasPrice || !bom.HasVendor {
		t.Fatalf("flags = price %v vendor %v", bom.HasPrice, bom.HasVendor)
	}

	if bom.TotalCents != 1018 {
		t.Errorf("total = %d, want 1018", bom.TotalCents)
	}
}

// TestParseBOMMarkdown covers a Markdown table with a separator row.
func TestParseBOMMarkdown(t *testing.T) {
	bom := parseBOM("BOM.md", []byte(`# Board

| Part | Qty | Unit Price | Vendor |
| --- | --- | --- | --- |
| XIAO | 2 | $4.99 | Seeed |
| LED | 5 | $0.10 | DigiKey |
`))

	if bom.Rows != 2 || bom.TotalCents != 1048 {
		t.Fatalf("bom = %+v", bom)
	}
}

// TestParseBOMMultipleTables skips a title table and stops at the next header.
func TestParseBOMMultipleTables(t *testing.T) {
	bom := parseBOM("BOM.md", []byte(`| Week | Tier | Parts funding |
| --- | --- | --- |
| Week 1 | Tier 1 | $30.00 |

| Part | What it is | Qty | Unit | Total | Vendor |
| --- | --- | --- | --- | --- | --- |
| XIAO | processor | 2 | $4.99 | $9.98 | Seeed |
| LED | light | 5 | $0.10 | $0.50 | DigiKey |
| **Total** | — | — | — | **$10.48** | — |
`))

	if bom.Rows != 2 || bom.TotalCents != 1048 {
		t.Fatalf("bom = %+v", bom)
	}
}

// TestParseBOMMissingColumns reports what a partial table still proves.
func TestParseBOMMissingColumns(t *testing.T) {
	bom := parseBOM("BOM.csv", []byte("Part,Qty\nXIAO,2\n"))

	if bom.Rows != 1 || bom.HasPrice || bom.HasVendor {
		t.Fatalf("bom = %+v", bom)
	}
}

// TestLocalEvidenceChecks covers journal words and clip coverage on the ingest payload.
func TestLocalEvidenceChecks(t *testing.T) {
	seconds := 600
	clip := "http://example.com/clip.mp4"

	request := contract.IngestRequest{
		Title:       "Evidence",
		Description: strings.Repeat("A detailed board and firmware description. ", 5),
		Journals: []contract.IngestJournal{{
			At:   "2026-11-01T10:00:00Z",
			Text: "Built it.",
			Timelapses: []contract.IngestRequestJournalsElemTimelapsesElem{{
				CoveredSeconds: &seconds,
				URL:            &clip,
			}},
		}},
		Meta: &contract.IngestMeta{Hours: &contract.IngestHours{ClaimedSeconds: 3600}},
	}

	checks := map[string]contract.CheckResult{}

	for _, result := range localChecks(request, time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)) {
		checks[result.Key] = result
	}

	if checks["journal.present"].Status != statusPass || checks["journal.wordless"].Status != statusWarn || checks["journal.hours"].Status != statusWarn {
		t.Errorf("journal checks = %+v", checks)
	}

	if checks["lapse.clips"].Status != statusPass || checks["lapse.coverage"].Status != statusWarn || checks["lapse.links"].Status != statusError {
		t.Errorf("clip checks = %+v", checks)
	}

	empty := localChecks(contract.IngestRequest{Title: "Empty", Description: strings.Repeat("A description. ", 12)}, time.Now())
	emptyChecks := map[string]contract.CheckResult{}

	for _, result := range empty {
		emptyChecks[result.Key] = result
	}

	if emptyChecks["journal.present"].Status != statusFail || emptyChecks["lapse.coverage"].Status != statusSkip {
		t.Errorf("empty evidence = %+v", emptyChecks)
	}
}
