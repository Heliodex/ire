// previewableKicadSuffixes mirrors the server allowlist: the KiCad files the evidence tab can render in-app with the vendored KiCanvas bundle.
const previewableKicadSuffixes = [".kicad_pcb", ".kicad_sch", ".kicad_pro"]

// isPreviewableKicad reports whether the evidence tab can render a file in-app. The server enforces the same suffix set.
export function isPreviewableKicad(path: string): boolean {
	const lowered = path.toLowerCase()
	return previewableKicadSuffixes.some(suffix => lowered.endsWith(suffix))
}
