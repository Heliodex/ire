// formatHours renders integer seconds as a compact hour count, matching the plan's display rule.
export function formatHours(seconds: number): string {
	const hours = seconds / 3600
	if (hours === 0) return "0h"
	if (Number.isInteger(hours)) return `${hours}h`
	return `${hours.toFixed(2).replace(/0+$/, "").replace(/\.$/, "")}h`
}

// formatMinutes renders seconds as hours and minutes, e.g. 6h 24m.
export function formatMinutes(seconds: number): string {
	const total = Math.round(seconds / 60)
	const hours = Math.floor(total / 60)
	const minutes = total % 60
	if (hours === 0) return `${minutes}m`
	if (minutes === 0) return `${hours}h`
	return `${hours}h ${minutes}m`
}

// formatDate renders an ISO timestamp in the reader's locale.
export function formatDate(value: string | null | undefined): string {
	if (!value) return "–"
	const date = new Date(value)
	if (Number.isNaN(date.getTime())) return value
	return new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(date)
}

// formatDateTime renders an ISO timestamp with time.
export function formatDateTime(value: string | null | undefined): string {
	if (!value) return "–"
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
	if (cents == null) return "–"
	return new Intl.NumberFormat(undefined, { currency: "USD", style: "currency" }).format(cents / 100)
}

// Tone is the badge palette shared with Ari: green approved, yellow pending, orange changes, red rejected, purple second pass.
export type Tone = "approved" | "pending" | "changes" | "rejected" | "secondpass" | "neutral"

// statusTone maps a check, ship or delivery status to a badge tone.
export function statusTone(status: string): Tone {
	switch (status) {
		case "pass":
		case "approved":
		case "delivered":
		case "decided":
			return "approved"
		case "ready":
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
			return "Needs a look"
		case "claimed":
			return "Claimed"
		case "in_review":
			return "In review"
		case "stage2_hold":
			return "Held for second pass"
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

// greeting picks a short line for the queue header.
export function greeting(name: string | null | undefined): string {
	return name ? `Signed in as ${name}` : "Signed in"
}
