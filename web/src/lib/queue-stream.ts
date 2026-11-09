import { array, boolean, literal, number, object, safeParse, string, variant } from "valibot"
import { type Queue } from "./api"
import { ApiError } from "./api-error"
import { program } from "./api-runtime"
import { errorResponseSchema, queueCountsSchema, queueItemSchema } from "./contract"

const metaEventSchema = object({ hasMore: boolean(), items: number(), type: literal("meta") })
const shipsEventSchema = object({ items: array(queueItemSchema), type: literal("ships") })
const countsEventSchema = object({ counts: queueCountsSchema, type: literal("counts") })
const failureEventSchema = object({ error: string(), type: literal("error") })
const doneEventSchema = object({ type: literal("done") })
const queueEventSchema = variant("type", [
	metaEventSchema,
	shipsEventSchema,
	countsEventSchema,
	failureEventSchema,
	doneEventSchema,
])

export interface QueueStreamHandlers {
	onCounts(counts: Queue["counts"]): void
	onMeta(hasMore: boolean, items: number): void
	onShips(items: Queue["items"]): void
}

// splitQueueLines separates the completed NDJSON lines from the tail that is still arriving.
export function splitQueueLines(buffer: string): { lines: string[]; rest: string } {
	const index = buffer.lastIndexOf("\n")
	if (index === -1) return { lines: [], rest: buffer }

	return {
		lines: buffer
			.slice(0, index)
			.split("\n")
			.filter(line => line.trim() !== ""),
		rest: buffer.slice(index + 1),
	}
}

// applyQueueLine validates one NDJSON event and hands it to the page. Reports true when the stream is finished.
export function applyQueueLine(line: string, handlers: QueueStreamHandlers): boolean {
	const parsed = safeParse(queueEventSchema, JSON.parse(line))
	if (!parsed.success) throw new Error("The review API returned an unexpected stream event.")

	const event = parsed.output

	switch (event.type) {
		case "meta":
			handlers.onMeta(event.hasMore, event.items)

			return false
		case "ships":
			handlers.onShips(event.items)

			return false
		case "counts":
			handlers.onCounts(event.counts)

			return false
		case "error":
			throw new ApiError(event.error, "http_error")
		case "done":
			return true
		default:
			throw new Error("The review API returned an unexpected stream event.")
	}
}

// streamQueue reads the queue as NDJSON so each batch of ships can render as soon as it arrives.
export async function streamQueue(
	filter: string,
	stage: number,
	limit: number,
	sortKey: string | null | undefined,
	sortDir: 1 | -1,
	q: string,
	handlers: QueueStreamHandlers,
	signal: AbortSignal,
): Promise<void> {
	const params = new URLSearchParams({ filter, limit: String(limit), stage: String(stage), stream: "1" })

	if (q.trim() !== "") params.set("q", q.trim())

	if (typeof sortKey === "string" && sortKey !== "") {
		params.set("dir", sortDir === -1 ? "desc" : "asc")
		params.set("sort", sortKey)
	}

	const response = await fetch(`/api/v1/programs/${program}/queue?${params}`, {
		headers: { Accept: "application/x-ndjson" },
		signal,
	})

	if (!response.ok) {
		const data: unknown = await response.json().catch(() => undefined)
		const error = safeParse(errorResponseSchema, data)
		if (error.success) throw new ApiError(error.output.error, error.output.code, error.output.field ?? "")

		throw new ApiError(`The review API returned ${response.status}`, "http_error")
	}

	if (!response.body) throw new Error("The review API did not return a stream.")

	const stream = response.body.pipeThrough(new TextDecoderStream())
	let buffer = ""

	for await (const value of stream) {
		const split = splitQueueLines(buffer + value)
		buffer = split.rest

		for (const line of split.lines) if (applyQueueLine(line, handlers)) return
	}

	const tail = buffer.trim()
	if (tail !== "") applyQueueLine(tail, handlers)
}
