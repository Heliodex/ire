import { describe, expect, test } from "bun:test"
import { isPreviewableKicad } from "../src/lib/preview"

describe("isPreviewableKicad", () => {
	test("matches KiCad boards, schematics and projects regardless of case", () => {
		expect(isPreviewableKicad("board.kicad_pcb")).toBe(true)
		expect(isPreviewableKicad("pcb/FRONT.KICAD_PCB")).toBe(true)
		expect(isPreviewableKicad("board.kicad_sch")).toBe(true)
		expect(isPreviewableKicad("board.kicad_pro")).toBe(true)
	})

	test("rejects gerbers and other files", () => {
		expect(isPreviewableKicad("gerbers/board-F_Cu.gtl")).toBe(false)
		expect(isPreviewableKicad("README.md")).toBe(false)
		expect(isPreviewableKicad("BOM.csv")).toBe(false)
	})
})
