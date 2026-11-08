import { describe, expect, test } from "bun:test"
import type { AuditEntry } from "../src/lib/api"
import {
	auditChanges,
	auditDetailRows,
	auditLook,
	auditReason,
	auditSummary,
	auditTargetHref,
	auditTargetLabel,
	parseAuditJSON,
} from "../src/lib/audit"

function entryOf(overrides: Partial<AuditEntry> = {}): AuditEntry {
	return {
		action: "review.decision",
		createdAt: "2026-02-01T12:00:00Z",
		id: "aud_1",
		...overrides,
	}
}

describe("parseAuditJSON", () => {
	test("reads objects and ignores everything else", () => {
		expect(parseAuditJSON('{"status":"ready"}')).toEqual({ status: "ready" })
		expect(parseAuditJSON("[]")).toEqual({})
		expect(parseAuditJSON("null")).toEqual({})
		expect(parseAuditJSON("nope")).toEqual({})
		expect(parseAuditJSON(undefined)).toEqual({})
	})
})

describe("auditLook", () => {
	test("maps review decisions to their outcome", () => {
		expect(auditLook(entryOf({ afterJson: '{"decision":"approved"}' }))).toEqual({
			icon: "check",
			label: "Approved",
			tone: "approved",
		})
		expect(auditLook(entryOf({ afterJson: '{"decision":"approved","held":true}' }))).toEqual({
			icon: "clock",
			label: "Held for a second pass",
			tone: "secondpass",
		})
		expect(auditLook(entryOf({ afterJson: '{"decision":"changes"}' })).tone).toBe("changes")
		expect(auditLook(entryOf({ afterJson: '{"decision":"rejected"}' })).icon).toBe("x")
	})

	test("gives every admin action its own look", () => {
		expect(auditLook(entryOf({ action: "review.send_back" })).label).toBe("Sent back")
		expect(auditLook(entryOf({ action: "journal.adjust" })).tone).toBe("pending")
		expect(auditLook(entryOf({ action: "note.create" })).label).toBe("Note added")
		expect(auditLook(entryOf({ action: "note.delete" })).tone).toBe("rejected")
		expect(auditLook(entryOf({ action: "member.invite" })).tone).toBe("purple")
		expect(auditLook(entryOf({ action: "member.update" })).icon).toBe("user")
		expect(auditLook(entryOf({ action: "user.sessions_revoke" })).icon).toBe("lock")
		expect(auditLook(entryOf({ action: "user.bootstrap" })).icon).toBe("shield")
		expect(auditLook(entryOf({ action: "custom.action" }))).toEqual({
			icon: "info",
			label: "Custom action",
			tone: "neutral",
		})
	})
})

describe("auditSummary", () => {
	test("describes decisions and member changes", () => {
		expect(auditSummary(entryOf({ afterJson: '{"decision":"approved"}' }))).toBe("approved the ship")
		expect(auditSummary(entryOf({ afterJson: '{"decision":"approved","held":true}' }))).toBe(
			"approved the ship and held it for a second pass",
		)
		expect(auditSummary(entryOf({ action: "member.invite", afterJson: '{"role":"reviewer"}' }))).toBe(
			"invited a member as reviewer",
		)
		expect(auditSummary(entryOf({ action: "user.bootstrap", afterJson: '{"role":"admin"}' }))).toBe(
			"bootstrapped the account as admin",
		)
	})
})

describe("auditChanges", () => {
	test("shows changed fields with readable values", () => {
		const changes = auditChanges(
			entryOf({
				afterJson: '{"status":"decided","decision":"approved","held":false,"hours":5400}',
				beforeJson: '{"status":"ready","version":3}',
			}),
		)

		expect(changes).toEqual([
			{ from: "Ready", key: "status", label: "Status", to: "Decided" },
			{ from: "–", key: "decision", label: "Decision", to: "Approved" },
			{ from: "–", key: "hours", label: "Approved hours", to: "1.5h" },
		])
	})

	test("keeps the reason for its own block and ignores unchanged fields", () => {
		const changes = auditChanges(
			entryOf({
				action: "review.send_back",
				afterJson: '{"status":"ready","stage":1,"invalidated":"rev_9","reason":"Check the wiring"}',
				beforeJson: '{"status":"stage2_hold","stage":2,"version":4}',
			}),
		)

		expect(changes.map(change => change.key)).toEqual(["status", "stage", "invalidated"])
		expect(changes[0]).toEqual({ from: "Second pass", key: "status", label: "Status", to: "Ready" })
	})

	test("turns booleans and roles into words", () => {
		const changes = auditChanges(
			entryOf({
				action: "member.update",
				afterJson: '{"role":"admin","active":false}',
				beforeJson: '{"role":"reviewer","active":true}',
			}),
		)

		expect(changes).toEqual([
			{ from: "Reviewer", key: "role", label: "Role", to: "Admin" },
			{ from: "Yes", key: "active", label: "Active", to: "No" },
		])
	})
})

describe("auditReason", () => {
	test("prefers the column and falls back to a snapshot", () => {
		expect(auditReason(entryOf({ afterJson: '{"reason":"After"}', reason: "Column reason" }))).toBe("Column reason")
		expect(auditReason(entryOf({ afterJson: '{"reason":"After"}' }))).toBe("After")
		expect(auditReason(entryOf({ beforeJson: '{"reason":"Before"}' }))).toBe("Before")
		expect(auditReason(entryOf())).toBe("")
	})
})

describe("auditDetailRows", () => {
	test("collects the full record for the expanded view", () => {
		const rows = auditDetailRows(
			entryOf({
				actorEmail: "ada@example.com",
				actorName: "Ada",
				afterJson: '{"status":"decided","decision":"approved"}',
				beforeJson: '{"status":"ready","version":3}',
				targetId: "sub_1",
				targetLabel: "Tea Timer",
				targetType: "submission",
			}),
		)

		expect(rows.map(row => row.label)).toEqual([
			"Action",
			"Actor",
			"Target",
			"Target ID",
			"Version",
			"When",
			"Event ID",
		])
		expect(rows.find(row => row.label === "Actor")?.value).toBe("Ada (ada@example.com)")
		expect(rows.find(row => row.label === "Target")?.value).toBe("Ship · Tea Timer")
		expect(rows.find(row => row.label === "Target ID")?.mono).toBe(true)
		expect(rows.find(row => row.label === "Version")?.value).toBe("v3")
		expect(rows.find(row => row.label === "Event ID")?.value).toBe("aud_1")
	})
})

describe("target links", () => {
	test("links ships and members but not notes", () => {
		expect(auditTargetHref(entryOf({ targetId: "sub_1", targetType: "submission" }))).toBe("/review/sub_1")
		expect(auditTargetHref(entryOf({ action: "member.update", targetId: "usr_1", targetType: "user" }))).toBe(
			"/reviewers/usr_1",
		)
		expect(auditTargetHref(entryOf({ targetId: "note_1", targetType: "note" }))).toBeUndefined()
		expect(auditTargetLabel(entryOf({ targetId: "note_1", targetType: "note" }))).toBe("")
	})
})
