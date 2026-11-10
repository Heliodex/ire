import { type Bundle } from "./api"
import { formatHours } from "./format"

export type MainField = "technicalFeatures" | "deflationReason" | "note" | "internalNote"

export interface MainFields {
	deflationReason: string
	internalNote: string
	note: string
	technicalFeatures: string
}

const fieldKeys: MainField[] = ["technicalFeatures", "deflationReason", "note", "internalNote"]

// isMainField narrows a draft key to one of the fields the station UI owns.
function isMainField(key: string): key is MainField {
	return key === "technicalFeatures" || key === "deflationReason" || key === "note" || key === "internalNote"
}

// parseDraftValues reads the JSON stored in a draft, ignoring anything malformed.
export function parseDraftValues(raw?: string): Partial<MainFields> {
	if (raw === undefined || raw === "") return {}

	try {
		const value: unknown = JSON.parse(raw)
		if (typeof value !== "object" || value === null || Array.isArray(value)) return {}

		const out: Partial<MainFields> = {}
		const entries = Object.entries(value)
		for (const [key, field] of entries) if (isMainField(key) && typeof field === "string") out[key] = field

		return out
	} catch {
		return {}
	}
}

// draftValues serialises the fields that belong to the station draft.
export function draftValues(fields: MainFields): string {
	const out: { [_: string]: string } = {}
	for (const key of fieldKeys) if (fields[key].trim() !== "") out[key] = fields[key]

	return JSON.stringify(out)
}

const firmwareExtensions = [
	".ino",
	".c",
	".cc",
	".cpp",
	".cxx",
	".h",
	".hh",
	".hpp",
	".py",
	".rs",
	".uf2",
	".hex",
	".bin",
	".go",
	".ts",
	".js",
]
const boardExtensions = [
	".kicad_pcb",
	".kicad_sch",
	".kicad_pro",
	".brd",
	".sch",
	".pcbdoc",
	".schdoc",
	".fzz",
	".epcb",
	".epro",
	".esch",
]
const cadExtensions = [
	".step",
	".stp",
	".f3d",
	".f3z",
	".fcstd",
	".sldprt",
	".sldasm",
	".iges",
	".igs",
	".stl",
	".obj",
	".3mf",
]

const firmwareKinds = new Set(["firmware", "code", "source"])
const boardKinds = new Set(["pcb", "pcbSource", "schematic", "logicSchematic", "design"])
const cadKinds = new Set(["cad", "cadSource", "cadModel", "enclosure", "model"])

// hasExtension reports whether any file path ends with one of the extensions.
function hasExtension(paths: string[], extensions: string[]): boolean {
	return paths.some(path => extensions.some(extension => path.toLowerCase().endsWith(extension)))
}

// firstSentence trims a description to its first useful sentence for the suggestion.
function firstSentence(text: string): string {
	const clean = text.trim().replaceAll(/\s+/g, " ")
	if (clean === "") return ""
	const end = clean.search(/[.!?](\s|$)/)
	return end === -1 ? clean : clean.slice(0, end + 1)
}

// suggestTechnicalFeatures builds a first draft from the description, BOM and file kinds.
export function suggestTechnicalFeatures(bundle: Bundle): string {
	const paths = (bundle.evidence?.files ?? []).map(file => file.path)
	const kinds = new Set((bundle.evidence?.files ?? []).map(file => file.kind))
	const parts: string[] = []
	const description = firstSentence(bundle.submission.description ?? "")

	if (description !== "") parts.push(description)

	const partsList = bundle.bom
		.slice(0, 6)
		.map(line => line.name)
		.filter(name => name.trim() !== "")

	const hasBoard = [...kinds].some(kind => boardKinds.has(kind)) || hasExtension(paths, boardExtensions)
	const hasFirmware = [...kinds].some(kind => firmwareKinds.has(kind)) || hasExtension(paths, firmwareExtensions)
	const hasCad = [...kinds].some(kind => cadKinds.has(kind)) || hasExtension(paths, cadExtensions)

	if (hasBoard) {
		const components = partsList.length > 0 ? ` with ${partsList.join(", ")}` : ""
		parts.push(`Custom PCB: schematic and board files${components}.`)
	} else if (partsList.length > 0) parts.push(`Hardware: ${partsList.join(", ")}.`)

	if (hasFirmware) parts.push("Firmware: source files drive the board and the interactive behaviour.")
	if (hasCad) parts.push("Enclosure and mechanical parts: CAD source included.")

	if (parts.length === 0)
		parts.push(
			"Specific technical features: describe the board, firmware and enclosure work that accounts for these hours.",
		)

	return parts.join("\n")
}

// suggestDeflationReason builds the cut summary from the journal entries that were reduced.
export function suggestDeflationReason(bundle: Bundle, capturedSeconds: number): string {
	const labels: { [_: string]: string } = {
		duplicate: "duplicate with Hackatime",
		idle: "idle gap",
		long: "unreasonably long",
		nonproject: "not project work",
		other: "other",
		starter: "starter build",
		tutorial: "tutorial consumption",
		unverifiable: "unverifiable",
	}
	const groups = new Map<string, { seconds: number; entries: number }>()
	let total = 0

	for (const entry of bundle.journals) {
		if (entry.approvedSeconds === undefined || entry.approvedSeconds >= entry.seconds) continue
		const key = entry.deflationReason ?? "other"
		const group = groups.get(key) ?? { entries: 0, seconds: 0 }
		group.seconds += entry.seconds - entry.approvedSeconds
		group.entries += 1
		total += entry.seconds - entry.approvedSeconds
		groups.set(key, group)
	}

	if (total === 0) return "The approved time matches the captured time, so no deflation is needed."

	const cuts = [...groups.entries()]
		.map(([key, group]) => {
			const suffix = group.entries > 1 ? ` (${group.entries} entries)` : ""
			return `${formatHours(group.seconds)} ${labels[key] ?? key}${suffix}`
		})
		.join(", ")

	return `Cut ${formatHours(total)} from ${formatHours(capturedSeconds)}: ${cuts}. Compare with what an intermediate maker would take for the same scope.`
}

// suggestInternalNote collects the findings and station evidence a synthesis reviewer needs.
export function suggestInternalNote(bundle: Bundle): string {
	const lines: string[] = []
	for (const finding of bundle.findings) {
		if (finding.status !== "open") continue
		const action = finding.makerAction?.trim()
		const suffix = action !== undefined && action !== "" ? ` — ${action}` : ""
		lines.push(`${finding.severity}: ${finding.summary}${suffix}`)
	}

	for (const answer of bundle.stationAnswers) {
		if (answer.answer !== "fail") continue
		const station = bundle.stations.find(item => item.key === answer.stationKey)
		const question = station?.questions.find(item => item.key === answer.questionKey)
		const suffix = answer.evidence === "" ? "" : ` — ${answer.evidence}`
		lines.push(`${station?.label ?? answer.stationKey}: ${question?.prompt ?? answer.questionKey}${suffix}`)
	}

	return lines.join("\n")
}
