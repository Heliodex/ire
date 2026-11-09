import { afterEach, describe, expect, test } from "bun:test"
import { type Queue } from "../src/lib/api"
import { ApiError } from "../src/lib/api-error"
import { fakeQueueResponse } from "../src/lib/fixtures.gen"
import { applyQueueLine, type QueueStreamHandlers, splitQueueLines, streamQueue } from "../src/lib/queue-stream"

const queue = fakeQueueResponse()

let requested = ""

// streamFetch answers the queue request with three chunks, splitting the NDJSON lines mid-record.
function streamFetch(input: RequestInfo | URL): Promise<Response> {
	requested = input instanceof Request ? input.url : String(input)

	const chunks = [
		`{"type":"meta","hasMore":false,"items":1}\n{"type":"ships","items":[`,
		`${JSON.stringify(queue.items[0])}]}\n{"ty`,
		`pe":"done"}\n`,
	]

	return Promise.resolve(
		new Response(
			new ReadableStream({
				start(controller) {
					const encoder = new TextEncoder()
					for (const chunk of chunks) controller.enqueue(encoder.encode(chunk))
					controller.close()
				},
			}),
			{ status: 200 },
		),
	)
}

// unauthorizedFetch answers with an API error so streamQueue has to surface it.
function unauthorizedFetch(): Promise<Response> {
	return Promise.resolve(Response.json({ code: "unauthorized", error: "sign in to review" }, { status: 401 }))
}

interface RecordedHandlers extends QueueStreamHandlers {
	counts: Queue["counts"][]
	hasMore: boolean[]
	items: Queue["items"][]
	meta: number[]
}

function recorder(): RecordedHandlers {
	const seen: RecordedHandlers = {
		counts: [],
		hasMore: [],
		items: [],
		meta: [],
		onCounts(value: Queue["counts"]): void {
			seen.counts.push(value)
		},
		onMeta(value: boolean, total: number): void {
			seen.hasMore.push(value)
			seen.meta.push(total)
		},
		onShips(value: Queue["items"]): void {
			seen.items.push(value)
		},
	}

	return seen
}

describe("splitQueueLines", () => {
	test("holds an unterminated line back", () => {
		expect(splitQueueLines(`{"type":"meta"}`)).toEqual({ lines: [], rest: `{"type":"meta"}` })
	})

	test("returns complete lines and the tail", () => {
		expect(splitQueueLines(`{"a":1}\n{"b":2\n`)).toEqual({ lines: [`{"a":1}`, `{"b":2`], rest: "" })
	})

	test("skips blank lines", () => {
		expect(splitQueueLines('\n\n{"a":1}\n\n').lines).toEqual([`{"a":1}`])
	})
})

describe("applyQueueLine", () => {
	test("hands meta, ships and counts to the page", () => {
		const handlers = recorder()

		expect(applyQueueLine(`{"type":"meta","hasMore":true,"items":1}`, handlers)).toBe(false)
		expect(applyQueueLine(JSON.stringify({ items: queue.items, type: "ships" }), handlers)).toBe(false)
		expect(applyQueueLine(JSON.stringify({ counts: queue.counts, type: "counts" }), handlers)).toBe(false)

		expect(handlers.hasMore).toEqual([true])
		expect(handlers.meta).toEqual([1])
		expect(handlers.items).toEqual([queue.items])
		expect(handlers.counts).toEqual([queue.counts])
	})

	test("reports the end of the stream", () => {
		expect(applyQueueLine(`{"type":"done"}`, recorder())).toBe(true)
	})

	test("throws the API error from an error event", () => {
		expect(() => applyQueueLine(`{"type":"error","error":"could not load the queue"}`, recorder())).toThrow(
			ApiError,
		)
	})

	test("rejects an event the contract does not know", () => {
		expect(() => applyQueueLine(`{"type":"ships","items":[{"id":"x"}]}`, recorder())).toThrow(
			"unexpected stream event",
		)
	})
})

describe("streamQueue", () => {
	const originalFetch = globalThis.fetch

	afterEach(() => {
		globalThis.fetch = originalFetch
	})

	test("reads batches split across chunks", async () => {
		requested = ""

		// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- The stub has the same call shape as fetch.
		globalThis.fetch = streamFetch as typeof fetch

		const handlers = recorder()
		await streamQueue("all", 1, 50, "ship", -1, "", handlers, new AbortController().signal)

		expect(requested).toContain("filter=all")
		expect(requested).toContain("limit=50")
		expect(requested).toContain("sort=ship")
		expect(requested).toContain("dir=desc")
		expect(requested).toContain("stream=1")
		expect(handlers.hasMore).toEqual([false])
		expect(handlers.items).toEqual([[queue.items[0]]])
	})

	test("surfaces an HTTP error before the stream starts", async () => {
		// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- The stub has the same call shape as fetch.
		globalThis.fetch = unauthorizedFetch as unknown as typeof fetch

		let caught: unknown

		try {
			await streamQueue("all", 1, 50, undefined, 1, "", recorder(), new AbortController().signal)
		} catch (err) {
			caught = err
		}

		expect(caught).toBeInstanceOf(ApiError)

		const message = caught instanceof ApiError ? caught.message : ""
		expect(message).toBe("sign in to review")
	})
})
