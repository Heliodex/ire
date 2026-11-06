import type { BaseIssue, InferOutput } from "valibot"
import { parseJSON, program } from "./api-runtime"
import {
	auditResponseSchema,
	type authConfigSchema,
	type checkHeadSchema,
	type checkRunSchema,
	type claimResponseSchema,
	type decisionResultSchema,
	draftResponseSchema,
	type evidenceSchema,
	type fakeDeliveriesResponseSchema,
	type journalReviewResponseSchema,
	type makerProfileSchema,
	type memberSchema,
	type meResponseSchema,
	type outboxResponseSchema,
	type queueResponseSchema,
	type reviewBundleSchema,
	type reviewedResponseSchema,
	type reviewerProfileSchema,
	type searchResponseSchema,
	type sendBackResultSchema,
	type submissionFileResponseSchema,
} from "./contract"
import { ValidationError } from "./validation-error"

export { parseJSON, program, requestVoid } from "./api-runtime"

// toValidationError flattens Valibot issues into the first message per field.
function toValidationError(issues: readonly BaseIssue<unknown>[]): ValidationError {
	const fields: { [_: string]: string } = {}
	for (const issue of issues) {
		const key = issue.path?.[0]?.key
		if (typeof key !== "string" || key in fields) continue
		fields[key] = issue.message
	}
	return new ValidationError(fields)
}

export type AuthConfig = InferOutput<typeof authConfigSchema>
export type Me = InferOutput<typeof meResponseSchema>
export type Queue = InferOutput<typeof queueResponseSchema>
export type QueueItem = Queue["items"][number]
export type Reviewed = InferOutput<typeof reviewedResponseSchema>
export type ReviewedItem = Reviewed["items"][number]
export type SearchResult = InferOutput<typeof searchResponseSchema>["results"][number]
export type Bundle = InferOutput<typeof reviewBundleSchema>
export type Check = Bundle["checks"][number]
export type Journal = Bundle["journals"][number]
export type JournalAdjustment = InferOutput<typeof journalReviewResponseSchema>
export type Note = Bundle["notes"][number]
export type BomLine = Bundle["bom"][number]
export type Review = Bundle["reviews"][number]
export type Claim = InferOutput<typeof claimResponseSchema>["claim"]
export type CheckRun = InferOutput<typeof checkRunSchema>
export type CheckHead = InferOutput<typeof checkHeadSchema>
export type Decision = InferOutput<typeof decisionResultSchema>
export type Evidence = InferOutput<typeof evidenceSchema>
export interface SendBackRequest {
	reason: string
	version: number
}
export type SendBackResult = InferOutput<typeof sendBackResultSchema>
export type SubmissionFile = InferOutput<typeof submissionFileResponseSchema>["file"]
export type OutboxRow = InferOutput<typeof outboxResponseSchema>["deliveries"][number]
export type FakeDelivery = InferOutput<typeof fakeDeliveriesResponseSchema>["deliveries"][number]
export type Member = InferOutput<typeof memberSchema>
export type AuditEntry = InferOutput<typeof auditResponseSchema>["entries"][number]
export type Audit = InferOutput<typeof auditResponseSchema>
export type ReviewerProfile = InferOutput<typeof reviewerProfileSchema>
export type MakerProfile = InferOutput<typeof makerProfileSchema>
export type MakerSubmission = MakerProfile["submissions"][number]

// saveDraft persists the review form.
export async function saveDraft(
	id: string,
	draft: { note: string; internalNote: string; values?: string; checklist?: string; adjustments?: string },
): Promise<string> {
	const response = await fetch(`/api/v1/programs/${program}/submissions/${encodeURIComponent(id)}/draft`, {
		body: JSON.stringify({
			adjustments: draft.adjustments ?? "{}",
			checklist: draft.checklist ?? "[]",
			internalNote: draft.internalNote,
			note: draft.note,
			values: draft.values ?? "{}",
		}),
		headers: { "content-type": "application/json" },
		method: "PUT",
	})
	const result = await parseJSON(response, draftResponseSchema)
	return result.updatedAt
}

export interface AuditFilters {
	action?: string
	days?: number
	q?: string
}

// fetchAudit lists audit entries, optionally narrowed by action, age or a search term.
export async function fetchAudit(offset = 0, limit = 50, filters: AuditFilters = {}): Promise<Audit> {
	const params = new URLSearchParams({ limit: String(limit), offset: String(offset) })
	if (filters.action) params.set("action", filters.action)
	if (filters.days) params.set("days", String(filters.days))
	if (filters.q?.trim()) params.set("q", filters.q.trim())

	const response = await fetch(`/api/v1/programs/${program}/audit?${params}`)
	return parseJSON(response, auditResponseSchema)
}

// Every other endpoint lives in the generated client. api.ts keeps the runtime
// (program, parseJSON, requestVoid), the hand-written leftovers above, and the
// shared types; the generated wrappers re-export here so existing imports keep working.
export {
	claimShip,
	createNote,
	decideShip,
	deleteNote,
	fetchAuthConfig,
	fetchBundle,
	fetchCheckHead,
	fetchMakerProfile,
	fetchMe,
	fetchMembers,
	fetchOutbox,
	fetchQueue,
	fetchReceived,
	fetchReviewed,
	fetchReviewerProfile,
	fetchSubmissionFile,
	heartbeat,
	inviteMember,
	login,
	logout,
	refreshRepositoryEvidence,
	releaseShip,
	rerunChecks,
	reviewJournal,
	searchProjects,
	sendShipBack,
	updateMember,
} from "./api.gen"

export { toValidationError }
