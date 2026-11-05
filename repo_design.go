package main

import (
	"fmt"
	"sort"
	"strings"

	"ire/contract"
)

const emptyFileBytes = 8

// designFiles groups the repository files a hardware reviewer cares about.
type designFiles struct {
	PCB, CAD, Firmware, Gerbers []string
}

// classifyDesignFiles sorts the tree into PCB, CAD, firmware and Gerber files.
func classifyDesignFiles(tree githubTree) designFiles {
	var files designFiles

	for _, file := range tree.Tree {
		if file.Type != "blob" {
			continue
		}

		path := strings.ToLower(file.Path)

		switch {
		case isPCBPath(path):
			files.PCB = append(files.PCB, file.Path)
		case isGerberPath(path):
			files.Gerbers = append(files.Gerbers, file.Path)
		case isCADPath(path):
			files.CAD = append(files.CAD, file.Path)
		case isFirmwarePath(path):
			files.Firmware = append(files.Firmware, file.Path)
		}
	}

	for _, list := range [][]string{files.PCB, files.CAD, files.Firmware, files.Gerbers} {
		sort.Strings(list)
	}

	return files
}

// any reports whether the repository has a design file of any kind.
func (d designFiles) any() bool {
	return len(d.PCB)+len(d.CAD)+len(d.Firmware)+len(d.Gerbers) > 0
}

// all lists every design file once, sorted.
func (d designFiles) all() []string {
	paths := append(append(append(append([]string{}, d.PCB...), d.CAD...), d.Firmware...), d.Gerbers...)
	sort.Strings(paths)

	return paths
}

// summary names the kinds present, such as "2 PCB, 1 CAD and 4 firmware files".
func (d designFiles) summary() string {
	var parts []string

	for _, group := range []struct {
		label string
		files []string
	}{
		{"PCB", d.PCB},
		{"CAD", d.CAD},
		{"firmware", d.Firmware},
		{"gerber", d.Gerbers},
	} {
		if len(group.files) > 0 {
			parts = append(parts, fmt.Sprintf("%d %s file%s", len(group.files), group.label, plural(len(group.files))))
		}
	}

	return strings.Join(parts, ", ")
}

// kindChecks adds a check per design kind so a reviewer can see what is missing without opening the tree.
func (d designFiles) kindChecks(checkedAt string) []contract.CheckResult {
	return []contract.CheckResult{
		designKindCheck("repo.pcb", "PCB source and fabrication", d.PCB, d.Gerbers, "pcb", "A CAD-only project needs no PCB files.", checkedAt),
		designKindCheck("repo.cad", "CAD source", d.CAD, nil, "cad", "A PCB-only project needs no CAD files.", checkedAt),
		designKindCheck("repo.firmware", "Firmware", d.Firmware, nil, "firmware", "Some hardware has no firmware.", checkedAt),
	}
}

// designKindCheck reports one kind, or skips it with the reason it may be absent.
func designKindCheck(key, label string, files, extra []string, kind, absent string, checkedAt string) contract.CheckResult {
	count := len(files) + len(extra)
	if count == 0 {
		return check(key, label, "repo", statusSkip, severityInfo, "None found", absent, checkedAt)
	}

	detail := strings.Join(capStrings(append(append([]string{}, files...), extra...), 6), ", ")

	return check(key, label, "repo", statusPass, severityInfo, fmt.Sprintf("%d %s file%s", count, kind, plural(count)), detail, checkedAt)
}

// emptyFilesCheck warns about placeholder files a maker committed instead of the real design.
func emptyFilesCheck(tree githubTree, checkedAt string) contract.CheckResult {
	var (
		empty []string
		known bool
	)

	for _, file := range tree.Tree {
		if file.Type != "blob" || file.Size == nil || file.Mode == "120000" {
			continue
		}

		base := file.Path[strings.LastIndex(file.Path, "/")+1:]
		if strings.HasPrefix(base, ".") {
			// .gitkeep and similar placeholders are allowed to be empty.
			continue
		}

		known = true

		if *file.Size <= emptyFileBytes {
			empty = append(empty, file.Path)
		}
	}

	if !known {
		return check("repo.files.empty", "Empty files", "repo", statusSkip, severityInfo, "File sizes not available", "The forge did not return file sizes.", checkedAt)
	}

	if len(empty) > 0 {
		sort.Strings(empty)

		return check("repo.files.empty", "Empty files", "repo", statusWarn, severityWarn,
			fmt.Sprintf("%d empty file%s", len(empty), plural(len(empty))),
			strings.Join(capStrings(empty, 5), ", ")+". Commit the real design files instead of placeholders.", checkedAt)
	}

	return check("repo.files.empty", "Empty files", "repo", statusPass, severityInfo, "No empty files", "Every design blob has content.", checkedAt)
}

// isPCBPath matches schematic, board and project files from common EDA tools.
func isPCBPath(path string) bool {
	return hasSuffix(path,
		".kicad_pcb", ".kicad_sch", ".kicad_pro", ".brd", ".sch", ".schdoc", ".pcbdoc",
		".fzz", ".fzpz", ".epcb", ".esch", ".epro", ".epro2", ".eprj", ".lpp", ".lppz")
}

// isGerberPath matches fabrication output and a Gerber archive by name.
func isGerberPath(path string) bool {
	if strings.Contains(path, "gerber") || strings.Contains(path, "drill") {
		return true
	}

	return hasSuffix(path, ".gbr", ".gtl", ".gbl", ".gts", ".gbs", ".gto", ".gbo", ".gko", ".gm1", ".drl", ".xln")
}

// isCADPath matches mechanical source, models and drawings.
func isCADPath(path string) bool {
	return hasSuffix(path,
		".step", ".stp", ".iges", ".igs", ".f3d", ".f3z", ".fcstd", ".sldprt", ".sldasm",
		".scad", ".3mf", ".stl", ".obj", ".blend", ".dwg", ".dxf")
}

// isFirmwarePath matches source files and the folders firmware usually lives in.
func isFirmwarePath(path string) bool {
	if hasSuffix(path, ".ino", ".c", ".cc", ".cpp", ".cxx", ".h", ".hh", ".hpp", ".py", ".rs", ".go",
		".uf2", ".hex", ".bin", ".asm", ".s", ".v", ".sv", ".vhd", ".keymap", ".dts", ".dtsi") {
		return true
	}

	for _, dir := range []string{"firmware/", "software/", "fw/", "src/", "code/"} {
		if strings.HasPrefix(path, dir) || strings.Contains(path, "/"+dir) {
			return hasSuffix(path, ".ts", ".js", ".txt", ".md", ".json", ".yaml", ".yml", ".toml")
		}
	}

	return false
}

// hasSuffix reports whether the path ends with any suffix.
func hasSuffix(path string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}

	return false
}
