import { afterEach, describe, expect, test } from "bun:test"
import { object, safeParse, string } from "valibot"
import { fetchAuthConfig, fetchMe, fetchQueue, toValidationError } from "../src/lib/api"
import { ApiError } from "../src/lib/api-error"
import { ValidationError } from "../src/lib/validation-error"

const realFetch = globalThis.fetch

afterEach(() => {
	globalThis.fetch = realFetch
})

// stubFetch records the requested URL and answers with the given body.
function stubFetch(status: number, body: unknown, seen: { url: string }): void {
	// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- The stub implements only the call signature the client uses, not fetch's statics.
	globalThis.fetch = ((input: RequestInfo | URL): Promise<Response> => {
		seen.url = input instanceof Request ? input.url : input.toString()
		return Promise.resolve(Response.json(body, { status }))
	}) as typeof fetch
}

// catchError resolves with the rejection reason so failures can be asserted like values.
async function catchError(promise: Promise<unknown>): Promise<unknown> {
	try {
		await promise
		return null
	} catch (err: unknown) {
		return err
	}
}

describe("fetchAuthConfig", () => {
	test("calls the config endpoint and parses the body", async () => {
		const seen = { url: "" }
		stubFetch(200, { dev: true, hca: false }, seen)

		const config = await fetchAuthConfig()

		expect(seen.url).toBe("/api/v1/auth/config")
		expect(config).toEqual({ dev: true, hca: false })
	})
})

describe("API errors", () => {
	test("maps the standard error body to ApiError", async () => {
		const seen = { url: "" }
		stubFetch(401, { code: "unauthorized", error: "No session", field: "" }, seen)

		expect(await catchError(fetchMe())).toMatchObject({ code: "unauthorized", name: "ApiError" })
	})

	test("falls back when the error has no shape", async () => {
		const seen = { url: "" }
		stubFetch(500, null, seen)

		const failure = await catchError(fetchMe())

		expect(failure).toBeInstanceOf(ApiError)
		expect(failure).toMatchObject({ code: "http_error" })
	})

	test("rejects a success with an unexpected shape", async () => {
		const seen = { url: "" }
		stubFetch(200, { programs: [], readOnly: false }, seen)

		expect(await catchError(fetchMe())).toBeInstanceOf(Error)
	})
})

describe("fetchQueue", () => {
	test("builds the filter, paging and sort query", async () => {
		const seen = { url: "" }
		stubFetch(
			200,
			{
				counts: { checking: 0, claimed: 0, fast: 0, mine: 0, needsAttention: 0, ready: 0, stage2Hold: 0 },
				hasMore: false,
				items: [],
			},
			seen,
		)

		await fetchQueue("ready", 1, 10, 25, "updated", -1)

		expect(seen.url).toContain("stage=1")
		expect(seen.url).toContain("filter=ready")
		expect(seen.url).toContain("offset=10")
		expect(seen.url).toContain("limit=25")
		expect(seen.url).toContain("sort=updated")
		expect(seen.url).toContain("dir=desc")
	})

	test("omits sort when none is given", async () => {
		const seen = { url: "" }
		stubFetch(
			200,
			{
				counts: { checking: 0, claimed: 0, fast: 0, mine: 0, needsAttention: 0, ready: 0, stage2Hold: 0 },
				hasMore: false,
				items: [],
			},
			seen,
		)

		await fetchQueue()

		expect(seen.url).not.toContain("sort=")
	})
})

describe("toValidationError", () => {
	test("keeps the first message per field", () => {
		const parsed = safeParse(object({ email: string(), role: string() }), { email: 42, role: 43 })
		expect(parsed.success).toBe(false)
		if (parsed.success) return

		const [emailIssue, roleIssue] = parsed.issues
		const error = toValidationError([
			{ ...emailIssue, message: "Bad email" },
			{ ...emailIssue, message: "Still bad" },
			{ ...roleIssue, message: "Bad role" },
		])

		expect(error).toBeInstanceOf(ValidationError)
		expect(error.fields).toEqual({ email: "Bad email", role: "Bad role" })
	})
})
