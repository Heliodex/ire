import { describe, expect, test } from "bun:test"
import { isPreviewableBoard } from "../src/lib/preview"

describe("isPreviewableBoard", () => {
	test("matches KiCad boards regardless of case", () => {
		expect(isPreviewableBoard("board.kicad_pcb")).toBe(true)
		expect(isPreviewableBoard("pcb/FRONT.KICAD_PCB")).toBe(true)
	})

	test("rejects schematics, projects and other files", () => {
		expect(isPreviewableBoard("board.kicad_sch")).toBe(false)
		expect(isPreviewableBoard("board.kicad_pro")).toBe(false)
		expect(isPreviewableBoard("gerbers/board-F_Cu.gtl")).toBe(false)
		expect(isPreviewableBoard("README.md")).toBe(false)
	})
})
