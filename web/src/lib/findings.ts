import { type Bundle } from "./api"
import { type Tone } from "./format"

export type Finding = Bundle["findings"][number]
export type FindingSeverity = Finding["severity"]
export type FindingStatus = Finding["status"]

export const findingStations = ["triage", "time", "narrative", "hardware", "synthesis"] as const

export const findingSeverities: FindingSeverity[] = ["block", "fixable", "note", "hold"]

const severityToneBySeverity: { [key: string]: Tone } = Object.freeze({
	block: "rejected",
	fixable: "changes",
	hold: "secondpass",
	note: "neutral",
})

const statusLabelByStatus: { [key: string]: string } = Object.freeze({
	accepted: "Accepted",
	disputed: "Disputed",
	fixed: "Fixed",
	open: "Open",
	withdrawn: "Withdrawn",
})

export const severityTone = (severity: FindingSeverity): Tone => severityToneBySeverity[severity] ?? "neutral"

export const statusLabel = (status: FindingStatus): string => statusLabelByStatus[status] ?? status

// isActive treats disputed findings as unresolved, so their actions stay reachable.
export const isActive = (finding: Finding): boolean => finding.status === "open" || finding.status === "disputed"

// isReturnable finds the open or disputed findings that can block an approval and belong on a return.
export const isReturnable = (finding: Finding): boolean =>
	(finding.status === "open" || finding.status === "disputed")
	&& (finding.severity === "block" || finding.severity === "fixable")

// returnNote builds the maker-facing bullet list from the findings chosen for a return.
function returnLine(finding: Finding): string {
	const action = finding.makerAction?.trim() ?? ""

	return action === "" ? `- ${finding.summary}` : `- ${action}`
}

export const returnNote = (findings: Finding[]): string => findings.map(returnLine).join("\n")

// selectedFindingIds keeps only the IDs that are still open and returnable.
export const selectedFindingIds = (findings: Finding[], selected: string[]): string[] =>
	findings.filter(finding => isReturnable(finding) && selected.includes(finding.id)).map(finding => finding.id)
