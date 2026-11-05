package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"review/contract"
)

const maxBOMBody = 1 << 20

var bomLinkPattern = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)

// bomSummary is the parsed shape of the repository's bill of materials.
type bomSummary struct {
	Path                string
	Rows, TotalCents    int
	HasPrice, HasVendor bool
	Parts               []string
	Items               []bomItem
	Err                 error
}

// bomItem is one parsed repository BOM row with the values compared against the submitted BOM.
type bomItem struct {
	Name                string
	Quantity, UnitCents int
}

// fetchBOM reads the first bill-of-materials file and parses its rows.
func (a *reviewApp) fetchBOM(ctx context.Context, client githubClient, owner, name string, tree githubTree) bomSummary {
	path := findBOMPath(tree)
	if path == "" {
		return bomSummary{}
	}

	body, status, err := client.file(ctx, owner, name, path)
	if err != nil || status != http.StatusOK {
		return bomSummary{Path: path, Err: fmt.Errorf("BOM could not be read (HTTP %d)", status)}
	}

	if len(body) > maxBOMBody {
		body = body[:maxBOMBody]
	}

	return parseBOM(path, body)
}

// findBOMPath returns the first blob that looks like a bill of materials.
func findBOMPath(tree githubTree) string {
	for _, file := range tree.Tree {
		if file.Type != "blob" || file.Path == "" {
			continue
		}

		if isBOMFile(file.Path[strings.LastIndex(file.Path, "/")+1:]) {
			return file.Path
		}
	}

	return ""
}

// bomCheck reports the parsed BOM, not just the file.
func bomCheck(bom bomSummary, checkedAt string) contract.CheckResult {
	if bom.Path == "" {
		return check("repo.bom", "Bill of materials", "repo", statusFail, severityWarn, "No BOM file", "Add a BOM with part, quantity, unit price and vendor.", checkedAt)
	}

	if bom.Err != nil {
		return check("repo.bom", "Bill of materials", "repo", statusError, severityWarn, "BOM could not be read", bom.Err.Error(), checkedAt)
	}

	if bom.Rows == 0 {
		return check("repo.bom", "Bill of materials", "repo", statusWarn, severityWarn, bom.Path+" has no recognisable rows", "Use columns for part, quantity, unit price and vendor.", checkedAt)
	}

	summary := fmt.Sprintf("%s · %d part%s", bom.Path, bom.Rows, plural(bom.Rows))
	status, severity := statusPass, severityInfo

	if bom.HasPrice {
		summary += fmt.Sprintf(" · $%.2f", float64(bom.TotalCents)/100)
	} else {
		summary += " · no prices"
		status, severity = statusWarn, severityWarn
	}

	if !bom.HasVendor {
		status, severity = statusWarn, severityWarn
	}

	detail := strings.Join(capStrings(bom.Parts, 6), ", ")
	if !bom.HasVendor {
		detail += "; add vendor names"
	}

	return check("repo.bom", "Bill of materials", "repo", status, severity, summary, detail, checkedAt)
}

// bomMatchCheck compares repository unit prices with the maker's submitted BOM.
func bomMatchCheck(bom bomSummary, maker []contract.BomLine, checkedAt string) contract.CheckResult {
	if bom.Path == "" || len(maker) == 0 {
		return check("repo.bom.prices", "BOM prices match", "repo", statusSkip, severityInfo, "Not compared", "The repository BOM or the submitted BOM is missing.", checkedAt)
	}

	matched := 0
	mismatches := []string{}

	for _, line := range maker {
		if line.UnitCostCents <= 0 {
			continue
		}

		item, ok := matchBomItem(bom.Items, line.Name)
		if !ok {
			continue
		}

		matched++

		if item.UnitCents <= 0 {
			continue
		}

		tolerance := max(line.UnitCostCents/20, 2)
		if math.Abs(float64(item.UnitCents-line.UnitCostCents)) > float64(tolerance) {
			mismatches = append(mismatches, fmt.Sprintf("%s: repo $%.2f, submitted $%.2f", line.Name,
				float64(item.UnitCents)/100, float64(line.UnitCostCents)/100))
		}
	}

	if matched == 0 {
		return check("repo.bom.prices", "BOM prices match", "repo", statusSkip, severityInfo, "No matching parts", "No submitted part name matched a row in the repository BOM.", checkedAt)
	}

	if len(mismatches) > 0 {
		result := check("repo.bom.prices", "BOM prices match", "repo", statusWarn, severityWarn,
			fmt.Sprintf("%d price mismatch%s", len(mismatches), plural(len(mismatches))), strings.Join(capStrings(mismatches, 4), "; ")+".", checkedAt)
		result.Source = stringPtr("forge")

		return result
	}

	result := check("repo.bom.prices", "BOM prices match", "repo", statusPass, severityInfo,
		fmt.Sprintf("%d part%s compared", matched, plural(matched)), "Repository unit prices agree with the submitted BOM.", checkedAt)
	result.Source = stringPtr("forge")

	return result
}

// matchBomItem finds the repository row that best matches a submitted part name.
func matchBomItem(items []bomItem, name string) (bomItem, bool) {
	target := normalizePartName(name)
	if target == "" {
		return bomItem{}, false
	}

	for _, item := range items {
		candidate := normalizePartName(item.Name)
		if candidate == "" {
			continue
		}

		if candidate == target || strings.Contains(target, candidate) || strings.Contains(candidate, target) {
			return item, true
		}
	}

	return bomItem{}, false
}

// normalizePartName keeps the words that identify a part across naming styles.
func normalizePartName(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return ' '
		}
	}, cleanBOMPart(value))

	return strings.Join(strings.Fields(cleaned), " ")
}

// parseBOM reads a CSV, TSV, semicolon or Markdown table into part rows.
func parseBOM(path string, content []byte) bomSummary {
	summary := bomSummary{Path: path, Parts: []string{}}
	text := strings.TrimPrefix(string(content), "\ufeff")

	if strings.TrimSpace(text) == "" {
		return summary
	}

	delimiter := bomDelimiter(text)
	rows := bomRows(text, delimiter)

	for i, row := range rows {
		name, qty, price, vendor := bomColumns(row)
		if name < 0 || (qty < 0 && price < 0 && vendor < 0) {
			continue
		}

		for _, line := range rows[i+1:] {
			if headerName, headerQty, _, _ := bomColumns(line); headerName >= 0 && headerQty >= 0 {
				break
			}

			part := cleanBOMPart(bomCell(line, name))
			if part == "" || bomSeparator(line) || bomSummaryRow(part) {
				continue
			}

			summary.Rows++
			summary.Parts = append(summary.Parts, part)

			quantity := bomQuantity(bomCell(line, qty))
			cents, priced := bomMoney(bomCell(line, price))

			if priced {
				summary.TotalCents += quantity * cents
				summary.HasPrice = true
			}

			summary.Items = append(summary.Items, bomItem{Name: part, Quantity: quantity, UnitCents: cents})

			if bomCell(line, vendor) != "" {
				summary.HasVendor = true
			}
		}

		break
	}

	return summary
}

// bomDelimiter picks the separator that appears most in the first lines.
func bomDelimiter(text string) rune {
	best, count := ',', -1

	for _, delimiter := range []rune{',', ';', '\t', '|'} {
		total := strings.Count(text, string(delimiter))
		if total > count {
			best, count = delimiter, total
		}
	}

	return best
}

// bomRows parses the table, using a small Markdown splitter for pipe tables.
func bomRows(text string, delimiter rune) [][]string {
	if delimiter == '|' {
		rows := [][]string{}

		for line := range strings.SplitSeq(text, "\n") {
			if !strings.Contains(line, "|") {
				continue
			}

			trimmed := strings.Trim(strings.TrimSpace(line), "|")

			cells := strings.Split(trimmed, "|")
			for i := range cells {
				cells[i] = strings.TrimSpace(cells[i])
			}

			rows = append(rows, cells)
		}

		return rows
	}

	reader := csv.NewReader(bytes.NewReader([]byte(text)))
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	rows, err := reader.ReadAll()
	if err != nil {
		return nil
	}

	return rows
}

// bomColumns finds the part, quantity, price and vendor columns in a header row.
func bomColumns(row []string) (int, int, int, int) {
	name, qty, price, vendor := -1, -1, -1, -1

	for i, cell := range row {
		header := normalizeHeader(cell)

		switch {
		case name < 0 && (strings.Contains(header, "part") || strings.Contains(header, "component") || strings.Contains(header, "item") || strings.Contains(header, "name")):
			name = i
		case qty < 0 && (strings.Contains(header, "qty") || strings.Contains(header, "quantity") || strings.Contains(header, "count")):
			qty = i
		case price < 0 && (strings.Contains(header, "unit") || strings.Contains(header, "price") || strings.Contains(header, "cost")):
			price = i
		case vendor < 0 && (strings.Contains(header, "vendor") || strings.Contains(header, "supplier") || strings.Contains(header, "source") || strings.Contains(header, "store")):
			vendor = i
		}
	}

	return name, qty, price, vendor
}

// normalizeHeader lower-cases a column name and drops decoration.
func normalizeHeader(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("*", "", "`", "", "_", " ", "-", " ", "$", "", "(", "", ")", "").Replace(value)

	return strings.Join(strings.Fields(value), " ")
}

// cleanBOMPart strips Markdown links and emphasis from a part name.
func cleanBOMPart(value string) string {
	value = bomLinkPattern.ReplaceAllString(value, "$1")
	value = strings.NewReplacer("*", "", "`", "", "_", " ").Replace(value)

	return strings.Join(strings.Fields(value), " ")
}

// bomSummaryRow reports subtotal, tax and total rows that are not parts.
func bomSummaryRow(part string) bool {
	value := strings.ToLower(strings.Trim(part, "*_ `"))

	return strings.Contains(value, "subtotal") || strings.HasPrefix(value, "total") || strings.Contains(value, "tax") || strings.Contains(value, "shipping")
}

// bomCell returns a trimmed cell or an empty string when the column is absent.
func bomCell(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}

	return strings.TrimSpace(row[index])
}

// bomSeparator reports whether a Markdown row is only dashes and colons.
func bomSeparator(row []string) bool {
	for _, cell := range row {
		trimmed := strings.Trim(strings.TrimSpace(cell), "-: ")
		if trimmed != "" {
			return false
		}
	}

	return len(row) > 0
}

// bomQuantity reads a quantity, defaulting to one.
func bomQuantity(value string) int {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}

		return -1
	}, value)

	quantity, err := strconv.Atoi(digits)
	if err != nil || quantity < 1 {
		return 1
	}

	return quantity
}

// bomMoney reads a price into cents.
func bomMoney(value string) (int, bool) {
	cleaned := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' {
			return r
		}

		return -1
	}, value)

	amount, err := strconv.ParseFloat(cleaned, 64)
	if err != nil || amount < 0 {
		return 0, false
	}

	return int(math.Round(amount * 100)), true
}

// submittedBom converts the ingest payload's BOM lines for comparison with the repository.
func submittedBom(meta *contract.IngestMeta) []contract.BomLine {
	if meta == nil || len(meta.Bom) == 0 {
		return nil
	}

	lines := make([]contract.BomLine, 0, len(meta.Bom))

	for _, line := range meta.Bom {
		quantity := 1
		if line.Qty != nil {
			quantity = max(*line.Qty, 0)
		}

		unit := 0
		if line.UnitUsd != nil {
			unit = int(math.Round(*line.UnitUsd * 100))
		}

		lines = append(lines, contract.BomLine{
			Name:          line.Name,
			Quantity:      quantity,
			UnitCostCents: unit,
			Vendor:        line.Vendor,
			Link:          line.Link,
		})
	}

	return lines
}

// loadBomLines reads the maker's submitted BOM for the check job and evidence refresh.
func (a *reviewApp) loadBomLines(ctx context.Context, submissionID string) []contract.BomLine {
	rows, err := a.store.Query(ctx, `select name, quantity, unit_cost_cents, coalesce(vendor, ''), coalesce(link, '')
		from bom_line where submission_id = ? order by sort asc`, submissionID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	lines := []contract.BomLine{}

	for rows.Next() {
		var line contract.BomLine

		var vendor, link string

		if err := rows.Scan(&line.Name, &line.Quantity, &line.UnitCostCents, &vendor, &link); err != nil {
			return nil
		}

		line.Vendor = optionalText(vendor)
		line.Link = optionalText(link)
		lines = append(lines, line)
	}

	return lines
}
