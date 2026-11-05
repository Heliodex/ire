package main

import "testing"

// TestClassifyDesignFiles covers the PCB, CAD, firmware and Gerber groups.
func TestClassifyDesignFiles(t *testing.T) {
	tree := githubTree{Tree: []githubTreeEntry{
		{Path: "hardware/board.kicad_pcb", Type: "blob"},
		{Path: "hardware/board.kicad_sch", Type: "blob"},
		{Path: "fabrication/gerbers.zip", Type: "blob"},
		{Path: "case/enclosure.step", Type: "blob"},
		{Path: "firmware/main.ino", Type: "blob"},
		{Path: "web/app.ts", Type: "blob"},
		{Path: "README.md", Type: "blob"},
		{Path: "hardware", Type: "tree"},
	}}

	files := classifyDesignFiles(tree)
	if len(files.PCB) != 2 || len(files.CAD) != 1 || len(files.Firmware) != 1 || len(files.Gerbers) != 1 {
		t.Errorf("design groups = PCB %v, CAD %v, firmware %v, gerbers %v", files.PCB, files.CAD, files.Firmware, files.Gerbers)
	}

	if !files.any() || len(files.all()) != 5 {
		t.Errorf("all = %v", files.all())
	}
}

// TestEmptyFilesCheck covers placeholder blobs and the missing-size case.
func TestEmptyFilesCheck(t *testing.T) {
	small := 8
	full := 100

	tree := githubTree{Tree: []githubTreeEntry{
		{Path: "hardware/board.kicad_pcb", Type: "blob", Size: &small},
		{Path: "firmware/main.ino", Type: "blob", Size: &full},
	}}

	if result := emptyFilesCheck(tree, "now"); result.Status != statusWarn || result.Key != "repo.files.empty" {
		t.Errorf("empty files = %+v", result)
	}

	empty := githubTree{Tree: []githubTreeEntry{{Path: "board.kicad_pcb", Type: "blob"}}}
	if result := emptyFilesCheck(empty, "now"); result.Status != statusSkip {
		t.Errorf("missing sizes = %+v", result)
	}
}
