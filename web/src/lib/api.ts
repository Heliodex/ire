import { type BaseIssue, type GenericSchema, type InferOutput, safeParse } from "valibot"
import { ApiError } from "./api-error"
import {
	auditResponseSchema,
	authConfigSchema,
	checkHeadSchema,
	checkRunSchema,
	claimResponseSchema,
	decisionResultSchema,
	draftResponseSchema,
	errorResponseSchema,
	evidenceSchema,
	fakeDeliveriesResponseSchema,
	journalReviewResponseSchema,
	memberSchema,
	membersResponseSchema,
	meResponseSchema,
	noteCreatedSchema,
	outboxResponseSchema,
	queueResponseSchema,
	reviewBundleSchema,
	reviewedResponseSchema,
	reviewerProfileSchema,
	searchResponseSchema,
	sendBackResultSchema,
	submissionFileResponseSchema,
} from "./contract"
import { ValidationError } from "./validation-error"

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

export const program = "half-life"

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

// parseJSON parses a response through the contract schema, including API errors.
async function parseJSON<T>(response: Response, schema: GenericSchema<T>): Promise<T> {
	const data: unknown = await response.json().catch(() => null)

	if (!response.ok) {
		const error = safeParse(errorResponseSchema, data)
		if (error.success) throw new ApiError(error.output.error, error.output.code, error.output.field ?? "")
		throw new ApiError(`The review API returned ${response.status}`, "http_error")
	}

	const parsed = safeParse(schema, data)
	if (!parsed.success) throw new Error("The review API returned an unexpected response shape.")

	return parsed.output
}

// requestVoid performs a request whose success has no body.
async function requestVoid(path: string, init: RequestInit): Promise<void> {
	const response = await fetch(path, init)
	if (response.ok) return

	const data: unknown = await response.json().catch(() => null)
	const error = safeParse(errorResponseSchema, data)
	if (error.success) throw new ApiError(error.output.error, error.output.code, error.output.field ?? "")
	throw new ApiError(`The review API returned ${response.status}`, "http_error")
}

// fetchAuthConfig returns which sign-in methods are enabled.
export async function fetchAuthConfig(): Promise<AuthConfig> {
	const response = await fetch("/api/v1/auth/config")
	return parseJSON(response, authConfigSchema)
}

// fetchMe returns the signed-in reviewer, throwing ApiError 401 when there is no session.
export async function fetchMe(): Promise<Me> {
	const response = await fetch("/api/v1/me")
	return parseJSON(response, meResponseSchema)
}

// login is the development sign-in placeholder for Hack Club Auth.
export async function login(email: string): Promise<Me> {
	const response = await fetch("/api/v1/dev/login", {
		body: JSON.stringify({ email }),
		headers: { "content-type": "application/json" },
		method: "POST",
	})
	return parseJSON(response, meResponseSchema)
}

// logout deletes the session cookie.
export async function logout(): Promise<void> {
	await requestVoid("/api/v1/logout", { method: "POST" })
}

// fetchQueue returns one page of the stage 1 work list.
export async function fetchQueue(filter = "all", stage = 1, offset = 0, limit = 50): Promise<Queue> {
	const response = await fetch(
		`/api/v1/programs/${program}/queue?stage=${stage}&filter=${encodeURIComponent(filter)}&offset=${offset}&limit=${limit}`,
	)
	return parseJSON(response, queueResponseSchema)
}

// fetchReviewed returns a page of decided ships, newest first.
export async function fetchReviewed(decision = "all", offset = 0, limit = 50): Promise<Reviewed> {
	const response = await fetch(
		`/api/v1/programs/${program}/reviewed?decision=${encodeURIComponent(decision)}&offset=${offset}&limit=${limit}`,
	)
	return parseJSON(response, reviewedResponseSchema)
}

// searchProjects finds open ships by title, maker, external id or theme for the header search.
export async function searchProjects(query: string, limit = 8): Promise<SearchResult[]> {
	const response = await fetch(`/api/v1/programs/${program}/search?q=${encodeURIComponent(query)}&limit=${limit}`)
	const body = await parseJSON(response, searchResponseSchema)

	return body.results
}

// fetchBundle returns everything the review screen shows.
export async function fetchBundle(id: string): Promise<Bundle> {
	const response = await fetch(`/api/v1/programs/${program}/submissions/${encodeURIComponent(id)}`)
	return parseJSON(response, reviewBundleSchema)
}

// refreshRepositoryEvidence refetches the cached repository snapshot for an organiser.
export async function refreshRepositoryEvidence(id: string): Promise<Evidence> {
	const response = await fetch(`/api/v1/programs/${program}/submissions/${encodeURIComponent(id)}/evidence/refresh`, {
		method: "POST",
	})
	return parseJSON(response, evidenceSchema)
}

// claimShip takes or refreshes the claim.
export async function claimShip(id: string): Promise<Claim> {
	const response = await fetch(`/api/v1/programs/${program}/submissions/${encodeURIComponent(id)}/claim`, {
		method: "POST",
	})
	const result = await parseJSON(response, claimResponseSchema)
	return result.claim
}

// heartbeat extends the claim while the tab is open.
export async function heartbeat(id: string): Promise<void> {
	await requestVoid(`/api/v1/programs/${program}/submissions/${encodeURIComponent(id)}/heartbeat`, { method: "POST" })
}

// releaseShip gives the ship back to the queue. keepalive lets the request finish when the review page unmounts.
export async function releaseShip(id: string): Promise<void> {
	await requestVoid(`/api/v1/programs/${program}/submissions/${encodeURIComponent(id)}/claim`, {
		keepalive: true,
		method: "DELETE",
	})
}

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

// reviewJournal records one entry's approved time and deflation note.
export async function reviewJournal(
	id: string,
	entry: string,
	request: { approvedSeconds: number; reviewerNote: string; version: number },
	keepalive = false,
): Promise<JournalAdjustment> {
	const response = await fetch(
		`/api/v1/programs/${program}/submissions/${encodeURIComponent(id)}/journal/${encodeURIComponent(entry)}`,
		{
			body: JSON.stringify(request),
			headers: { "content-type": "application/json" },
			keepalive,
			method: "PATCH",
		},
	)
	return parseJSON(response, journalReviewResponseSchema)
}

// createNote adds an internal note on the ship, its project or its maker.
export async function createNote(id: string, request: { body: string; subjectType: string }): Promise<Note> {
	const response = await fetch(`/api/v1/programs/${program}/submissions/${encodeURIComponent(id)}/notes`, {
		body: JSON.stringify(request),
		headers: { "content-type": "application/json" },
		method: "POST",
	})
	const result = await parseJSON(response, noteCreatedSchema)
	return result.note
}

// deleteNote removes an internal note.
export async function deleteNote(id: string): Promise<void> {
	await requestVoid(`/api/v1/programs/${program}/notes/${encodeURIComponent(id)}`, { method: "DELETE" })
}

// decideShip records the stage 1 decision.
export async function decideShip(
	id: string,
	request: {
		decision: "approved" | "changes" | "rejected"
		version: number
		noteToMaker: string
		internalNote: string
		hoursApprovedSeconds: number
		acknowledgements: string[]
		idempotencyKey: string
		overrideReason?: string
	},
): Promise<Decision> {
	const response = await fetch(`/api/v1/programs/${program}/submissions/${encodeURIComponent(id)}/decision`, {
		body: JSON.stringify(request),
		headers: { "content-type": "application/json" },
		method: "POST",
	})
	return parseJSON(response, decisionResultSchema)
}

// rerunChecks queues a fresh repository check for an organiser.
export async function rerunChecks(id: string): Promise<CheckRun> {
	const response = await fetch(`/api/v1/programs/${program}/submissions/${encodeURIComponent(id)}/checks/rerun`, {
		method: "POST",
	})
	return parseJSON(response, checkRunSchema)
}

// fetchMembers lists the programme roster.
export async function fetchMembers(): Promise<Member[]> {
	const response = await fetch(`/api/v1/programs/${program}/members`)
	const body = await parseJSON(response, membersResponseSchema)

	return body.members
}

// inviteMember adds an email to the roster.
export async function inviteMember(email: string, role: string): Promise<Member> {
	const response = await fetch(`/api/v1/programs/${program}/members`, {
		body: JSON.stringify({ email, role }),
		headers: { "Content-Type": "application/json" },
		method: "POST",
	})
	return parseJSON(response, memberSchema)
}

// updateMember changes a role or reactivates an account.
export async function updateMember(id: string, role: string, active: boolean): Promise<Member> {
	const response = await fetch(`/api/v1/programs/${program}/members/${encodeURIComponent(id)}`, {
		body: JSON.stringify({ active, role }),
		headers: { "Content-Type": "application/json" },
		method: "PATCH",
	})
	return parseJSON(response, memberSchema)
}

export interface AuditFilters {
	action?: string
	days?: number
	q?: string
}

// fetchReviewerProfile returns one reviewer's roster entry, stats and recent decisions.
export async function fetchReviewerProfile(key: string): Promise<ReviewerProfile> {
	const response = await fetch(`/api/v1/programs/${program}/reviewers/${encodeURIComponent(key)}`)
	return parseJSON(response, reviewerProfileSchema)
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

// fetchCheckHead compares the snapshot with the live repository head.
export async function fetchCheckHead(id: string): Promise<CheckHead> {
	const response = await fetch(`/api/v1/programs/${program}/submissions/${encodeURIComponent(id)}/checks/head`)
	return parseJSON(response, checkHeadSchema)
}

// fetchSubmissionFile returns the capped text of one snapshot-listed KiCad file for in-app preview.
export async function fetchSubmissionFile(id: string, path: string): Promise<SubmissionFile> {
	const response = await fetch(
		`/api/v1/programs/${program}/submissions/${encodeURIComponent(id)}/file?path=${encodeURIComponent(path)}`,
	)
	const body = await parseJSON(response, submissionFileResponseSchema)

	return body.file
}

// sendShipBack invalidates a held review and returns the ship to stage 1.
export async function sendShipBack(id: string, request: SendBackRequest): Promise<SendBackResult> {
	const response = await fetch(`/api/v1/programs/${program}/submissions/${encodeURIComponent(id)}/send-back`, {
		body: JSON.stringify(request),
		headers: { "content-type": "application/json" },
		method: "POST",
	})
	return parseJSON(response, sendBackResultSchema)
}

// fetchOutbox returns recent deliveries for the operations page.
export async function fetchOutbox(): Promise<OutboxRow[]> {
	const response = await fetch(`/api/v1/programs/${program}/outbox?limit=50`)
	const result = await parseJSON(response, outboxResponseSchema)
	return result.deliveries
}

// fetchReceived returns what the in-process receiver verified.
export async function fetchReceived(): Promise<FakeDelivery[]> {
	const response = await fetch("/api/v1/dev/outbox/received?limit=50")
	const result = await parseJSON(response, fakeDeliveriesResponseSchema)
	return result.deliveries
}

export { toValidationError }
