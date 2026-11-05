package main

import (
	"testing"

	"ire/contract"
)

// TestParseBOM covers CSV and Markdown tables, quantities and prices.
func TestParseBOM(t *testing.T) {
	csv := parseBOM("BOM.csv", []byte("Part,Qty,Unit Price,Vendor\nSeeed XIAO ESP32-C3,2,4.99,Seeed\n0.96 inch OLED,1,3.12,AliExpress\nSubtotal,,,\n"))
	if csv.Rows != 2 || !csv.HasPrice || !csv.HasVendor {
		t.Fatalf("csv = %+v", csv)
	}

	if csv.TotalCents != 2*499+312 {
		t.Errorf("total = %d", csv.TotalCents)
	}

	markdown := parseBOM("BOM.md", []byte("| Part | Qty | Unit Price | Vendor |\n| --- | --- | --- | --- |\n| XIAO | 1 | $4.99 | Seeed |\n"))
	if markdown.Rows != 1 || markdown.TotalCents != 499 {
		t.Fatalf("markdown = %+v", markdown)
	}
}

// TestBomMatchCheck covers matching names, agreeing prices and a mismatch.
func TestBomMatchCheck(t *testing.T) {
	bom := parseBOM("BOM.csv", []byte("Part,Qty,Unit Price,Vendor\nSeeed XIAO ESP32-C3,1,4.99,Seeed\nOLED,1,3.12,AliExpress\n"))
	maker := []contract.BomLine{
		{Name: "XIAO ESP32-C3", Quantity: 1, UnitCostCents: 499},
		{Name: "0.96 OLED", Quantity: 1, UnitCostCents: 450},
	}

	result := bomMatchCheck(bom, maker, "2026-11-01T00:00:00Z")
	if result.Status != statusWarn || result.Key != "repo.bom.prices" {
		t.Fatalf("result = %+v", result)
	}

	agreeing := []contract.BomLine{{Name: "XIAO", Quantity: 1, UnitCostCents: 499}}
	if result := bomMatchCheck(bom, agreeing, "2026-11-01T00:00:00Z"); result.Status != statusPass {
		t.Fatalf("agreeing = %+v", result)
	}

	if result := bomMatchCheck(bomSummary{}, maker, "2026-11-01T00:00:00Z"); result.Status != statusSkip {
		t.Fatalf("missing repo BOM = %+v", result)
	}

	if result := bomMatchCheck(bom, nil, "2026-11-01T00:00:00Z"); result.Status != statusSkip {
		t.Fatalf("missing maker BOM = %+v", result)
	}
}
