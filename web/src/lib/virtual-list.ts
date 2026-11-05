export interface RowWindow {
	end: number
	start: number
}

// rowWindow returns the [start, end) row indexes to render for the current scroll position.
// offsets holds one top offset per row plus the total height as its last entry, so rows of
// different heights share one lookup. Rows touching the overscan band around the viewport render too.
// A band past every row yields an empty window at the end, which still pads to the full height.
export function rowWindow(offsets: number[], scrollTop: number, viewportHeight: number, overscan: number): RowWindow {
	const total = offsets.length - 1
	if (total <= 0) return { end: 0, start: 0 }

	const top = scrollTop - overscan
	const bottom = scrollTop + viewportHeight + overscan

	// First row whose bottom edge is below the band.
	let low = 0
	let high = total
	while (low < high) {
		const mid = Math.floor((low + high) / 2)
		if (offsets[mid + 1] <= top) low = mid + 1
		else high = mid
	}

	// First row past the band ends the window.
	let end = low
	while (end < total && offsets[end] < bottom) end += 1

	return { end, start: low }
}

// windowPads converts a window into the spacer heights above and below the rendered rows.
export function windowPads(offsets: number[], window: RowWindow): { bottom: number; top: number } {
	if (offsets.length === 0) return { bottom: 0, top: 0 }

	const total = offsets.at(-1) ?? 0

	return {
		bottom: Math.max(0, total - (offsets.at(window.end) ?? total)),
		top: Math.max(0, offsets.at(window.start) ?? 0),
	}
}
