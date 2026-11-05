import { describe, expect, test } from "bun:test"
import { rowWindow, windowPads } from "../src/lib/virtual-list"

// inBandRow reports whether a row touches the band around the viewport.
function inBandRow(offsets: number[], index: number, top: number, bottom: number): boolean {
	return offsets[index + 1] > top && offsets[index] < bottom
}

// Even 40px rows with a 600px viewport and 100px overscan.
function even(count: number, height = 40): number[] {
	const offsets = [0]
	for (let index = 0; index < count; index++) offsets.push(offsets[index] + height)
	return offsets
}

describe("rowWindow", () => {
	test("returns nothing for an empty list", () => {
		expect(rowWindow([], 0, 600, 100)).toEqual({ end: 0, start: 0 })
		expect(rowWindow([0], 0, 600, 100)).toEqual({ end: 0, start: 0 })
	})

	test("renders the first rows plus overscan at the top", () => {
		// Rows 0-14 fill the viewport, 15-17 fall in the overscan band.
		expect(rowWindow(even(100), 0, 600, 100)).toEqual({ end: 18, start: 0 })
	})

	test("slides the window while scrolled", () => {
		// 400px down: rows 10-24 in view, 7-9 and 25-27 in overscan.
		expect(rowWindow(even(100), 400, 600, 100)).toEqual({ end: 28, start: 7 })
	})

	test("clamps to the last row at the bottom", () => {
		expect(rowWindow(even(100), 3400, 600, 100)).toEqual({ end: 100, start: 82 })
	})

	test("renders nothing past the end", () => {
		expect(rowWindow(even(100), 100000, 600, 100)).toEqual({ end: 100, start: 100 })
	})

	test("handles uneven rows", () => {
		// Tops at 0, 100, 120, 200 with viewport 0-150 and no overscan: rows 0-2.
		expect(rowWindow([0, 100, 120, 200, 260], 0, 150, 0)).toEqual({ end: 3, start: 0 })
		// Scrolled to 110: row 0 ends at 100, rows 1-2 visible.
		expect(rowWindow([0, 100, 120, 200, 260], 110, 150, 0)).toEqual({ end: 4, start: 1 })
	})

	test("matches a linear scan on uneven data", () => {
		const heights = [30, 55, 42, 80, 25, 60, 47, 90, 33, 51, 44, 70, 29, 58, 41]
		const offsets = [0]
		for (const height of heights) offsets.push((offsets.at(-1) ?? 0) + height)

		for (const scrollTop of [0, 1, 50, 200, 399, 800]) {
			const overscan = 60
			const viewportHeight = 300
			const top = scrollTop - overscan
			const bottom = scrollTop + viewportHeight + overscan
			let start = heights.length
			let end = 0
			for (let index = 0; index < heights.length; index++)
				if (inBandRow(offsets, index, top, bottom)) {
					start = Math.min(start, index)
					end = index + 1
				}
			if (end === 0) start = 0

			expect(rowWindow(offsets, scrollTop, viewportHeight, overscan)).toEqual({ end, start })
		}
	})
})

describe("windowPads", () => {
	test("splits the total height around the window", () => {
		expect(windowPads(even(100), { end: 18, start: 7 })).toEqual({ bottom: 3280, top: 280 })
	})

	test("pads nothing for empty and full windows", () => {
		expect(windowPads([], { end: 0, start: 0 })).toEqual({ bottom: 0, top: 0 })
		expect(windowPads(even(10), { end: 10, start: 0 })).toEqual({ bottom: 0, top: 0 })
	})
})
