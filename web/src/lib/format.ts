// formatHours renders integer seconds as a compact hour count, matching the plan's display rule.
export function formatHours(seconds: number): string {
	const hours = seconds / 3600
	if (hours === 0) return "0h"
	if (Number.isInteger(hours)) return `${hours}h`
	return `${hours.toFixed(2).replace(/0+$/, "").replace(/\.$/, "")}h`
}

// formatDuration renders integer seconds as hours, minutes and seconds, e.g. 6h 24m 36s.
// A trailing zero-seconds part is left out, so exact minutes read as 6h 24m.
export function formatDuration(seconds: number): string {
	const total = Math.max(0, Math.round(seconds))
	const hours = Math.floor(total / 3600)
	const minutes = Math.floor((total % 3600) / 60)
	const secs = total % 60
	const base = `${hours}h ${minutes}m`
	return secs === 0 ? base : `${base} ${secs}s`
}

// formatDate renders an ISO timestamp in the reader's locale.
export function formatDate(value: string | undefined): string {
	if (value === undefined || value === "") return "–"
	const date = new Date(value)
	if (Number.isNaN(date.getTime())) return value
	return new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(date)
}

// formatDateTime renders an ISO timestamp with time.
export function formatDateTime(value: string | undefined): string {
	if (value === undefined || value === "") return "–"
	const date = new Date(value)
	if (Number.isNaN(date.getTime())) return value
	return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(date)
}

// timeAgo renders a coarse age for the queue.
export function timeAgo(value: string, now = Date.now()): string {
	const date = new Date(value)
	if (Number.isNaN(date.getTime())) return value
	const delta = Math.max(0, now - date.getTime())
	const minutes = Math.floor(delta / 60000)
	if (minutes < 1) return "just now"
	if (minutes < 60) return `${minutes}m ago`
	const hours = Math.floor(minutes / 60)
	if (hours < 24) return `${hours}h ago`
	return `${Math.floor(hours / 24)}d ago`
}

// formatMoney renders integer cents as dollars.
export function formatMoney(cents: number | null | undefined): string {
	if (cents === null || cents === undefined) return "–"
	return new Intl.NumberFormat(undefined, { currency: "USD", style: "currency" }).format(cents / 100)
}

// formatMoneyAmount formats the number without the currency symbol, for tables whose header names the currency.
export function formatMoneyAmount(cents: number | null | undefined): string {
	if (cents === null || cents === undefined) return "–"

	const parts = new Intl.NumberFormat(undefined, { currency: "USD", style: "currency" }).formatToParts(cents / 100)

	return parts
		.filter(part => part.type !== "currency")
		.map(part => part.value)
		.join("")
		.trim()
}

// Tone is the badge palette shared with Ari: green approved, yellow pending, orange changes, red rejected, purple 2nd pass.
export type Tone = "approved" | "ready" | "pending" | "changes" | "rejected" | "secondpass" | "neutral"

// statusTone maps a check, ship or delivery status to a badge tone.
export function statusTone(status: string): Tone {
	switch (status) {
		case "pass":
		case "approved":
		case "delivered":
		case "decided":
			return "approved"
		case "ready":
			return "ready"
		case "pending":
		case "claimed":
		case "in_review":
		case "checking":
			return "pending"
		case "warn":
		case "needs_attention":
		case "changes":
			return "changes"
		case "fail":
		case "rejected":
		case "withdrawn":
		case "failed":
		case "error":
			return "rejected"
		case "stage2_hold":
			return "secondpass"
		default:
			return "neutral"
	}
}

// humanStatus turns an internal status into a label for the queue and header.
export function humanStatus(status: string): string {
	switch (status) {
		case "ready":
			return "Ready"
		case "needs_attention":
			return "Needs attention"
		case "claimed":
			return "Claimed"
		case "in_review":
			return "In review"
		case "stage2_hold":
			return "2nd pass"
		case "decided":
			return "Decided"
		case "withdrawn":
			return "Withdrawn"
		case "checking":
			return "Checking"
		default:
			return status
	}
}

// decisionLabel turns an internal decision or prior review result into a sentence label.
export function decisionLabel(decision: string): string {
	switch (decision.toLowerCase()) {
		case "approved":
			return "Approved"
		case "changes":
		case "changes_requested":
			return "Changes requested"
		case "rejected":
			return "Rejected"
		default:
			return humanEvent(decision)
	}
}

// humanEvent turns a machine event or reason into a readable phrase.
export function humanEvent(kind: string): string {
	const spaced = kind.replaceAll("_", " ").trim()
	return spaced.charAt(0).toUpperCase() + spaced.slice(1)
}
