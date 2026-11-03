import { type BaseIssue, type GenericSchema, type InferOutput, safeParse } from "valibot"
import { ApiError } from "./api-error"
import {
	authConfigSchema,
	claimResponseSchema,
	decisionResultSchema,
	draftResponseSchema,
	errorResponseSchema,
	fakeDeliveriesResponseSchema,
	meResponseSchema,
	outboxResponseSchema,
	queueResponseSchema,
	reviewBundleSchema,
} from "./contract"
import { ValidationError } from "./validation-error"

export type AuthConfig = InferOutput<typeof authConfigSchema>
export type Me = InferOutput<typeof meResponseSchema>
export type Queue = InferOutput<typeof queueResponseSchema>
export type QueueItem = Queue["items"][number]
export type Bundle = InferOutput<typeof reviewBundleSchema>
export type Check = Bundle["checks"][number]
export type Journal = Bundle["journals"][number]
export type BomLine = Bundle["bom"][number]
export type Review = Bundle["reviews"][number]
export type Claim = InferOutput<typeof claimResponseSchema>["claim"]
export type Decision = InferOutput<typeof decisionResultSchema>
export type OutboxRow = InferOutput<typeof outboxResponseSchema>["deliveries"][number]
export type FakeDelivery = InferOutput<typeof fakeDeliveriesResponseSchema>["deliveries"][number]

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

// fetchQueue returns the stage 1 work list.
export async function fetchQueue(filter = "all", stage = 1): Promise<Queue> {
	const response = await fetch(
		`/api/v1/programs/${program}/queue?stage=${stage}&filter=${encodeURIComponent(filter)}`,
	)
	return parseJSON(response, queueResponseSchema)
}

// fetchBundle returns everything the review screen shows.
export async function fetchBundle(id: string): Promise<Bundle> {
	const response = await fetch(`/api/v1/programs/${program}/submissions/${encodeURIComponent(id)}`)
	return parseJSON(response, reviewBundleSchema)
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
	},
): Promise<Decision> {
	const response = await fetch(`/api/v1/programs/${program}/submissions/${encodeURIComponent(id)}/decision`, {
		body: JSON.stringify(request),
		headers: { "content-type": "application/json" },
		method: "POST",
	})
	return parseJSON(response, decisionResultSchema)
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
