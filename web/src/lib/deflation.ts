// Shared deflation reasons for journal, Hackatime and timelapse cuts.

export const reasonLabels = {
	duplicate: "Duplicate with Hackatime",
	idle: "Idle gap",
	long: "Unreasonably long",
	nonproject: "Not project work",
	other: "Other",
	starter: "Starter build, no original work",
	tutorial: "Tutorial consumption without application",
	unverifiable: "Unverifiable",
} as const

export type DeflationReason = keyof typeof reasonLabels

// reasonLabel returns the human label for a stored reason code.
export function reasonLabel(value: string): string {
	const labels: { [_: string]: string } = reasonLabels
	return labels[value] ?? value
}
