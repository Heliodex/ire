import type { Overview } from "./api"

export interface CachedOverview {
	overview: Overview
	savedAt: number
}

export interface OverviewCacheStore {
	getItem(key: string): string | null | undefined
	removeItem(key: string): void
	setItem(key: string, value: string): void
}

export interface OverviewCache {
	read(): CachedOverview | undefined
	write(overview: Overview): void
}

interface StoredOverview {
	overview: unknown
	savedAt: number
	version: number
}

const cacheVersion = 1
const maxViewAge = 60 * 1000

// memories keeps the last view per programme in memory, so navigating back
// paints instantly without touching storage.
const memories = new Map<string, CachedOverview>()

// isFreshOverview reports whether a cached overview is still worth painting before the network answers.
export function isFreshOverview(view: CachedOverview): boolean {
	return Date.now() - view.savedAt <= maxViewAge
}

function isOverview(value: unknown): value is Overview {
	if (typeof value !== "object" || value === null) return false
	if (!("needsReview" in value) || typeof value.needsReview !== "number") return false
	if (!("newToday" in value) || typeof value.newToday !== "number") return false
	if (!("reviewedToday" in value) || typeof value.reviewedToday !== "number") return false
	if (!("wait" in value) || typeof value.wait !== "object" || value.wait === null) return false
	if (!("daily" in value) || !Array.isArray(value.daily)) return false
	if (!("byStatus" in value) || !Array.isArray(value.byStatus)) return false

	return true
}

function readStored(store: OverviewCacheStore, key: string): CachedOverview | undefined {
	try {
		const raw = store.getItem(key)
		if (!raw) return undefined

		const parsed: unknown = JSON.parse(raw)
		if (typeof parsed !== "object" || parsed === null) return undefined
		if (!("version" in parsed) || parsed.version !== cacheVersion) return undefined
		if (!("savedAt" in parsed) || typeof parsed.savedAt !== "number") return undefined
		if (!("overview" in parsed) || !isOverview(parsed.overview)) return undefined

		return { overview: parsed.overview, savedAt: parsed.savedAt }
	} catch {
		return undefined
	}
}

// createOverviewCache keeps the last overview per programme in memory and tab
// storage, so navigating back paints instantly and revalidates quietly.
export function createOverviewCache(programSlug: string, store: OverviewCacheStore): OverviewCache {
	const key = `review:overview:${programSlug}`

	return {
		// read returns the cached view when it is still fresh, from memory first.
		read() {
			const memory = memories.get(key)
			if (memory && isFreshOverview(memory)) return memory
			if (memory) memories.delete(key)

			const stored = readStored(store, key)
			if (!stored || !isFreshOverview(stored)) return

			memories.set(key, stored)

			return stored
		},
		// write keeps the view for the next visit.
		write(overview) {
			const view: CachedOverview = { overview, savedAt: Date.now() }
			memories.set(key, view)

			try {
				const stored: StoredOverview = { overview, savedAt: view.savedAt, version: cacheVersion }
				store.setItem(key, JSON.stringify(stored))
			} catch {
				// Storage can be unavailable or full; the in-memory cache still works.
			}
		},
	}
}
