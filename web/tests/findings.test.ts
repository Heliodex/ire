import { describe, expect, test } from "bun:test"
import {
	type Finding,
	isActive,
	isReturnable,
	returnNote,
	selectedFindingIds,
	severityTone,
	statusLabel,
} from "../src/lib/findings"

function finding(overrides: Partial<Finding>): Finding {
	return {
		createdAt: "2026-01-01T00:00:00Z",
		externalId: "ext-1",
		id: "finding-1",
		openedSubmissionId: "ship-1",
		openedVersion: 1,
		severity: "block",
		station: "triage",
		status: "open",
		summary: "Loose screw",
		updatedAt: "2026-01-01T00:00:00Z",
		...overrides,
	}
}

describe("isReturnable", () => {
	test("accepts open and disputed block and fixable findings", () => {
		expect(isReturnable(finding({ severity: "block" }))).toBe(true)
		expect(isReturnable(finding({ severity: "fixable" }))).toBe(true)
		expect(isReturnable(finding({ severity: "block", status: "disputed" }))).toBe(true)
		expect(isReturnable(finding({ severity: "note" }))).toBe(false)
		expect(isReturnable(finding({ severity: "hold" }))).toBe(false)
		expect(isReturnable(finding({ severity: "block", status: "fixed" }))).toBe(false)
	})
})

describe("isActive", () => {
	test("treats disputed findings as unresolved", () => {
		expect(isActive(finding({ status: "open" }))).toBe(true)
		expect(isActive(finding({ status: "disputed" }))).toBe(true)
		expect(isActive(finding({ status: "accepted" }))).toBe(false)
	})
})

describe("returnNote", () => {
	test("prefers the maker action and falls back to the summary", () => {
		const note = returnNote([
			finding({ makerAction: "Re-solder the joint" }),
			finding({ id: "finding-2", makerAction: undefined, summary: "Missing fuse" }),
		])

		expect(note).toBe("- Re-solder the joint\n- Missing fuse")
	})
})

describe("selectedFindingIds", () => {
	test("keeps selected IDs only while they stay returnable", () => {
		const findings = [
			finding({ id: "a" }),
			finding({ id: "b", status: "fixed" }),
			finding({ id: "c", severity: "note" }),
			finding({ id: "d" }),
		]

		expect(selectedFindingIds(findings, ["a", "b", "c", "d"])).toEqual(["a", "d"])
	})
})

describe("labels", () => {
	test("maps tones and labels with a fallback", () => {
		expect(severityTone("block")).toBe("rejected")
		expect(severityTone("fixable")).toBe("changes")
		expect(severityTone("hold")).toBe("secondpass")
		expect(severityTone("note")).toBe("neutral")
		expect(severityTone("unknown")).toBe("neutral")
		expect(statusLabel("open")).toBe("Open")
		expect(statusLabel("withdrawn")).toBe("Withdrawn")
		expect(statusLabel("unknown")).toBe("unknown")
	})
})
