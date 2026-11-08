import type { Queue } from "./api"
import type { BundleCacheStore } from "./bundle-cache"

export type QueueFilter = "all" | "fast" | "attention" | "mine"

export type QueueSortKey = "ship" | "maker" | "status" | "type" | "version" | "claimed" | "age" | "checks"

export interface SavedQueueState {
	filter: QueueFilter
	queue: Queue
	savedAt: number
	scrollY: number
	search: string
	selectedId: string
	sortDir: 1 | -1
	sortKey: QueueSortKey | undefined
	stage: number
	version: number
}

export interface QueueState {
	read(): SavedQueueState | undefined
	write(state: Omit<SavedQueueState, "savedAt" | "version">): void
}

const stateVersion = 2
const maxStateAge = 24 * 60 * 60 * 1000

// isQueueFilter narrows a cached filter value.
export function isQueueFilter(value: string): value is QueueFilter {
	return value === "all" || value === "fast" || value === "attention" || value === "mine"
}

// isSavedQueueState reports a fresh, well-shaped copy of the last visit. It checks the
// envelope, not the rows: the version field already invalidates older shapes.
function isSavedQueueState(value: unknown): value is SavedQueueState {
	if (typeof value !== "object" || value === null) return false
	if (!("version" in value) || value.version !== stateVersion) return false
	if (!("filter" in value) || typeof value.filter !== "string" || !isQueueFilter(value.filter)) return false
	if (!("stage" in value) || (value.stage !== 1 && value.stage !== 2)) return false
	if (!("queue" in value) || typeof value.queue !== "object" || value.queue === null) return false

	const { queue } = value
	if (!("items" in queue) || !Array.isArray(queue.items)) return false
	if (!("counts" in queue) || typeof queue.counts !== "object") return false
	if (!("search" in value) || typeof value.search !== "string") return false
	if (!("scrollY" in value) || typeof value.scrollY !== "number") return false
	if (!("savedAt" in value) || typeof value.savedAt !== "number" || Date.now() - value.savedAt > maxStateAge)
		return false

	return true
}

// createQueueState keeps the loaded rows, filter and scroll position for reloads and returns from a review.
export function createQueueState(programSlug: string, store: BundleCacheStore): QueueState {
	const key = `review:queue:${programSlug}`

	return {
		// read restores the last visit, ignoring stale or malformed copies.
		read() {
			try {
				const raw = store.getItem(key)
				if (!raw) return

				const parsed: unknown = JSON.parse(raw)

				return isSavedQueueState(parsed) ? parsed : undefined
			} catch {}
		},
		// write replaces the stored visit, stamping the version and time.
		write(state: Omit<SavedQueueState, "savedAt" | "version">) {
			try {
				store.setItem(
					key,
					JSON.stringify({ ...state, savedAt: Date.now(), version: stateVersion } satisfies SavedQueueState),
				)
			} catch {
				// Storage can be unavailable or full; the queue still works without a cache.
			}
		},
	}
}
