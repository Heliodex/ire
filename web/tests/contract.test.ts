import { describe, expect, test } from "bun:test"
import { safeParse } from "valibot"
import {
	decisionRequestSchema,
	draftRequestSchema,
	inviteMemberSchema,
	journalReviewRequestSchema,
	noteRequestSchema,
	sendBackRequestSchema,
} from "../src/lib/contract"

describe("inviteMemberSchema", () => {
	test("accepts a valid invite", () => {
		const parsed = safeParse(inviteMemberSchema, { email: "ada@example.com", role: "reviewer" })

		expect(parsed.success).toBe(true)
	})

	test("rejects bad emails and roles", () => {
		expect(safeParse(inviteMemberSchema, { email: "x", role: "reviewer" }).success).toBe(false)
		expect(safeParse(inviteMemberSchema, { email: "ada@example.com", role: "superuser" }).success).toBe(false)
	})
})

describe("decisionRequestSchema", () => {
	const valid = {
		acknowledgements: [],
		decision: "approved",
		hoursApprovedSeconds: 3600,
		idempotencyKey: "key-1",
		internalNote: "",
		noteToMaker: "Nice board.",
		version: 1,
	}

	test("accepts a valid decision", () => {
		expect(safeParse(decisionRequestSchema, valid).success).toBe(true)
	})

	test("rejects unknown decisions", () => {
		expect(safeParse(decisionRequestSchema, { ...valid, decision: "maybe" }).success).toBe(false)
	})
})

describe("draftRequestSchema", () => {
	test("rejects notes over the limit", () => {
		const draft = {
			adjustments: "{}",
			checklist: "[]",
			internalNote: "",
			note: "x".repeat(4001),
			values: "{}",
		}

		expect(safeParse(draftRequestSchema, draft).success).toBe(false)
	})
})

describe("noteRequestSchema", () => {
	test("requires a body and a known subject", () => {
		expect(safeParse(noteRequestSchema, { body: "Hello", subjectType: "submission" }).success).toBe(true)
		expect(safeParse(noteRequestSchema, { body: "", subjectType: "submission" }).success).toBe(false)
		expect(safeParse(noteRequestSchema, { body: "Hello", subjectType: "planet" }).success).toBe(false)
	})
})

describe("journalReviewRequestSchema", () => {
	test("rejects negative approved time", () => {
		expect(
			safeParse(journalReviewRequestSchema, { approvedSeconds: 60, reviewerNote: "", version: 1 }).success,
		).toBe(true)
		expect(
			safeParse(journalReviewRequestSchema, { approvedSeconds: -1, reviewerNote: "", version: 1 }).success,
		).toBe(false)
	})
})

describe("sendBackRequestSchema", () => {
	test("requires a version", () => {
		expect(safeParse(sendBackRequestSchema, { reason: "Back to stage 1", version: 2 }).success).toBe(true)
		expect(safeParse(sendBackRequestSchema, { reason: "Back to stage 1", version: -1 }).success).toBe(false)
	})
})
