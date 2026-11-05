import { afterEach, describe, expect, test } from "bun:test"
import { fetchAuthConfig, fetchMe, fetchQueue, toValidationError } from "../src/lib/api"
import { ApiError } from "../src/lib/api-error"
import { ValidationError } from "../src/lib/validation-error"

const realFetch = globalThis.fetch

afterEach(() => {
	globalThis.fetch = realFetch
})

// stubFetch records the requested URL and answers with the given body.
function stubFetch(status: number, body: unknown, seen: { url: string }): void {
	globalThis.fetch = ((url: string | URL | Request) => {
		seen.url = String(url)
		return Promise.resolve(new Response(JSON.stringify(body), { status }))
	}) as typeof fetch
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

		const failure = fetchMe().then(
			() => null,
			(error: unknown) => error,
		)

		expect(await failure).toMatchObject({ code: "unauthorized", name: "ApiError" })
	})

	test("falls back when the error has no shape", async () => {
		const seen = { url: "" }
		stubFetch(500, null, seen)

		const failure = fetchMe().then(
			() => null,
			(error: unknown) => error,
		)

		expect(await failure).toBeInstanceOf(ApiError)
		expect(((await failure) as ApiError).code).toBe("http_error")
	})

	test("rejects a success with an unexpected shape", async () => {
		const seen = { url: "" }
		stubFetch(200, { programs: [], readOnly: false }, seen)

		const failure = fetchMe().then(
			() => null,
			(error: unknown) => error,
		)

		expect(await failure).toBeInstanceOf(Error)
	})
})

describe("fetchQueue", () => {
	test("builds the filter, paging and sort query", async () => {
		const seen = { url: "" }
		stubFetch(
			200,
			{
				counts: { checking: 0, claimed: 0, needsAttention: 0, ready: 0, stage2Hold: 0 },
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
				counts: { checking: 0, claimed: 0, needsAttention: 0, ready: 0, stage2Hold: 0 },
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
		const error = toValidationError([
			{ message: "Bad email", path: [{ key: "email" }] },
			{ message: "Still bad", path: [{ key: "email" }] },
			{ message: "Bad role", path: [{ key: "role" }] },
		] as never)

		expect(error).toBeInstanceOf(ValidationError)
		expect(error.fields).toEqual({ email: "Bad email", role: "Bad role" })
	})
})
