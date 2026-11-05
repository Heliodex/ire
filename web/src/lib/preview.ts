// isPreviewableBoard reports whether the evidence tab can render a file in-app. The first slice covers self-contained KiCad boards only; the server enforces the same suffix.
export function isPreviewableBoard(path: string): boolean {
	return path.toLowerCase().endsWith(".kicad_pcb")
}
