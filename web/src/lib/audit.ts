import type { AuditEntry } from "./api"
import type { IconName } from "./components/icon-paths"
import { decisionLabel, formatDateTime, formatHours, humanEvent, humanStatus, type Tone } from "./format"
import { reviewerHref } from "./reviewer"

export type AuditTone = Tone | "purple"

export interface AuditLook {
	icon: IconName
	label: string
	tone: AuditTone
}

export interface AuditChange {
	from: string
	key: string
	label: string
	to: string
}

export interface AuditDetail {
	label: string
	mono?: boolean
	value: string
}

const labels: { [_: string]: string } = {
	active: "Active",
	approvedSeconds: "Approved time",
	body: "Note",
	decision: "Decision",
	email: "Email",
	entry: "Journal entry",
	held: "Held",
	hours: "Approved hours",
	invalidated: "Invalidated review",
	reason: "Reason",
	reviewerNote: "Reviewer note",
	role: "Role",
	stage: "Stage",
	status: "Status",
	subjectId: "Subject ID",
	subjectType: "Note subject",
	version: "Version",
}

function isJSONObject(value: unknown): value is { [_: string]: unknown } {
	return value !== null && typeof value === "object" && !Array.isArray(value)
}

function sameValue(left: unknown, right: unknown): boolean {
	return JSON.stringify(left) === JSON.stringify(right)
}

function targetTypeLabel(type: string | undefined): string {
	if (!type) return "Target"

	switch (type) {
		case "submission":
			return "Ship"
		case "user":
			return "Member"
		case "note":
			return "Note"
		default:
			return type
	}
}

function auditValue(key: string, value: unknown): string {
	if ((key === "hours" || key === "approvedSeconds") && typeof value === "number") return formatHours(value)
	if (key === "status" && typeof value === "string") return humanStatus(value)
	if (key === "decision" && typeof value === "string") return decisionLabel(value)
	if (key === "role" && typeof value === "string") return humanEvent(value)
	if (key === "subjectType" && typeof value === "string") return humanEvent(value)
	if (typeof value === "boolean") return value ? "Yes" : "No"
	if (value === null || value === undefined || value === "") return "–"
	if (Array.isArray(value)) return value.map(item => auditValue(key, item)).join(", ") || "–"
	if (typeof value === "object") return JSON.stringify(value)
	if (typeof value === "string" || typeof value === "number") return String(value)
	return "–"
}

// parseAuditJSON reads a recorded before or after snapshot, ignoring anything that is not an object.
export function parseAuditJSON(value: string | undefined): { [_: string]: unknown } {
	if (!value) return {}
	try {
		const parsed = JSON.parse(value) as unknown
		return isJSONObject(parsed) ? parsed : {}
	} catch {
		return {}
	}
}

// auditLook names the event and picks the icon, colour and wording for its type.
export function auditLook(entry: AuditEntry): AuditLook {
	const after = parseAuditJSON(entry.afterJson)

	switch (entry.action) {
		case "review.decision":
			if (after.decision === "approved" && after.held === true)
				return { icon: "clock", label: "Held for a second pass", tone: "secondpass" }
			if (after.decision === "approved") return { icon: "check", label: "Approved", tone: "approved" }
			if (after.decision === "changes") return { icon: "clock", label: "Changes requested", tone: "changes" }
			if (after.decision === "rejected") return { icon: "x", label: "Rejected", tone: "rejected" }

			return { icon: "checkCircle", label: "Decision recorded", tone: "ready" }
		case "review.send_back":
			return { icon: "refresh", label: "Sent back", tone: "secondpass" }
		case "journal.adjust":
			return { icon: "config", label: "Journal adjusted", tone: "pending" }
		case "note.create":
			return { icon: "msg", label: "Note added", tone: "ready" }
		case "note.delete":
			return { icon: "msg", label: "Note removed", tone: "rejected" }
		case "member.invite":
			return { icon: "user", label: "Member invited", tone: "purple" }
		case "member.update":
			return { icon: "user", label: "Member updated", tone: "purple" }
		case "user.sessions_revoke":
			return { icon: "lock", label: "Sessions revoked", tone: "rejected" }
		case "user.bootstrap":
			return { icon: "shield", label: "Bootstrap account", tone: "ready" }
		default:
			return { icon: "info", label: humanEvent(entry.action.replaceAll(".", " ")), tone: "neutral" }
	}
}

// auditSummary finishes the sentence after the actor's name.
export function auditSummary(entry: AuditEntry): string {
	const after = parseAuditJSON(entry.afterJson)
	const role = typeof after.role === "string" ? after.role : ""

	switch (entry.action) {
		case "review.decision":
			if (after.decision === "approved")
				return after.held === true ? "approved the ship and held it for a second pass" : "approved the ship"
			if (after.decision === "changes") return "requested changes"
			if (after.decision === "rejected") return "rejected the ship"

			return "recorded a decision"
		case "review.send_back":
			return "sent the held ship back to stage 1"
		case "journal.adjust":
			return "adjusted journal hours"
		case "note.create":
			return "added a note"
		case "note.delete":
			return "removed a note"
		case "member.invite":
			return role === "" ? "invited a member" : `invited a member as ${role}`
		case "member.update":
			return "updated a member's role or access"
		case "user.sessions_revoke":
			return "revoked every session"
		case "user.bootstrap":
			return role === "" ? "bootstrapped the account" : `bootstrapped the account as ${role}`
		default:
			return entry.action.replaceAll(".", " ").replaceAll("_", " ")
	}
}

// auditChanges lists only the fields a snapshot changed, formatted for display.
export function auditChanges(entry: AuditEntry): AuditChange[] {
	const before = parseAuditJSON(entry.beforeJson)
	const after = parseAuditJSON(entry.afterJson)
	const keys = new Set([...Object.keys(before), ...Object.keys(after)])
	const rows: AuditChange[] = []

	for (const key of keys) {
		// The reason has its own block and the version is shown in the record header.
		if (key === "reason" || key === "version") continue

		const from = before[key]
		const to = after[key]
		if (sameValue(from, to)) continue
		// A held=false that was absent before is the default, not a change worth a row.
		if (key === "held" && to !== true && from === undefined) continue

		rows.push({
			from: auditValue(key, from),
			key,
			label: labels[key] ?? humanEvent(key),
			to: auditValue(key, to),
		})
	}

	return rows
}

// auditReason reads the event reason from its own column or from either recorded snapshot.
export function auditReason(entry: AuditEntry): string {
	if (entry.reason?.trim()) return entry.reason.trim()

	const after = parseAuditJSON(entry.afterJson)
	const before = parseAuditJSON(entry.beforeJson)
	const found = [after.reason, before.reason].find(value => typeof value === "string" && value.trim() !== "")

	return typeof found === "string" ? found.trim() : ""
}

// auditDetailRows builds the full record shown when a row is expanded.
export function auditDetailRows(entry: AuditEntry): AuditDetail[] {
	const before = parseAuditJSON(entry.beforeJson)
	const after = parseAuditJSON(entry.afterJson)
	const look = auditLook(entry)
	const rows: AuditDetail[] = [{ label: "Action", value: `${look.label} (${entry.action})` }]

	if (entry.actorEmail)
		rows.push({
			label: "Actor",
			value: entry.actorName ? `${entry.actorName} (${entry.actorEmail})` : entry.actorEmail,
		})

	const targetLabel = entry.targetLabel?.trim() ?? ""
	const targetID = entry.targetId?.trim() ?? ""
	const targetName = targetLabel || targetID

	if (targetName) rows.push({ label: "Target", value: `${targetTypeLabel(entry.targetType)} · ${targetName}` })
	if (targetLabel && targetID) rows.push({ label: "Target ID", mono: true, value: targetID })

	const version = after.version ?? before.version
	if (typeof version === "number") rows.push({ label: "Version", value: `v${version}` })

	rows.push(
		{ label: "When", value: formatDateTime(entry.createdAt) },
		{ label: "Event ID", mono: true, value: entry.id },
	)

	return rows
}

// auditTargetLabel is the short target shown in the row headline, if the event has one.
export function auditTargetLabel(entry: AuditEntry): string {
	const label = entry.targetLabel?.trim() ?? ""
	if (label) return label
	if (entry.targetType === "note") return ""
	return entry.targetId?.trim() ?? ""
}

// auditTargetHref links a ship or member target to its page.
export function auditTargetHref(entry: AuditEntry): string | undefined {
	if (entry.targetType === "submission" && entry.targetId) return `/review/${encodeURIComponent(entry.targetId)}`
	if ((entry.action.startsWith("member.") || entry.action.startsWith("user.")) && entry.targetId)
		return reviewerHref(entry.targetId)

	return undefined
}
