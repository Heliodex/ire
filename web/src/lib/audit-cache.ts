import type { AuditEntry } from "./api"

export interface AuditFilters {
	action: string
	days: number
	query: string
}

export interface CachedAuditView {
	entries: AuditEntry[]
	hasMore: boolean
	savedAt: number
}

export interface AuditCacheStore {
	getItem(key: string): string | null
	removeItem(key: string): void
	setItem(key: string, value: string): void
}

export interface AuditCache {
	readFilters(): AuditFilters | null
	readView(filters: AuditFilters): CachedAuditView | null
	writeFilters(filters: AuditFilters): void
	writeView(filters: AuditFilters, view: Omit<CachedAuditView, "savedAt">): void
}

interface StoredAuditFilters extends AuditFilters {
	savedAt: number
}

interface StoredAuditCache {
	filters: StoredAuditFilters | null
	version: number
	views: { [_: string]: CachedAuditView }
}

const cacheVersion = 1
const maxFilterAge = 24 * 60 * 60 * 1000
const maxViewAge = 10 * 60 * 1000
const maxCachedEntries = 250
const maxCachedViews = 8
const maxCacheBytes = 1_500_000

// auditFilterKey normalises a filter set so casing and surrounding search spaces share one entry.
export function auditFilterKey(filters: AuditFilters): string {
	return JSON.stringify([filters.action, filters.days, filters.query.trim().toLowerCase()])
}

// isFreshAuditView reports whether a cached page is still worth painting before the network answers.
export function isFreshAuditView(view: CachedAuditView): boolean {
	return Date.now() - view.savedAt <= maxViewAge
}

function isAuditEntry(value: unknown): value is AuditEntry {
	if (typeof value !== "object" || value === null) return false
	if (!("id" in value) || typeof value.id !== "string") return false
	if (!("action" in value) || typeof value.action !== "string") return false

	return "createdAt" in value && typeof value.createdAt === "string"
}

function isCachedView(value: unknown): value is CachedAuditView {
	if (typeof value !== "object" || value === null) return false
	if (!("entries" in value) || !Array.isArray(value.entries) || !value.entries.every(isAuditEntry)) return false
	if (!("hasMore" in value) || typeof value.hasMore !== "boolean") return false

	return "savedAt" in value && typeof value.savedAt === "number"
}

function isStoredFilters(value: unknown): value is StoredAuditFilters {
	if (typeof value !== "object" || value === null) return false
	if (!("action" in value) || typeof value.action !== "string") return false
	if (!("days" in value) || typeof value.days !== "number" || !Number.isFinite(value.days)) return false
	if (!("query" in value) || typeof value.query !== "string") return false

	return "savedAt" in value && typeof value.savedAt === "number"
}

function emptyCache(): StoredAuditCache {
	return { filters: null, version: cacheVersion, views: {} }
}

function readCache(store: AuditCacheStore, key: string): StoredAuditCache {
	try {
		const raw = store.getItem(key)
		if (!raw) return emptyCache()

		const parsed: unknown = JSON.parse(raw)
		if (typeof parsed !== "object" || parsed === null) return emptyCache()
		if (!("version" in parsed) || parsed.version !== cacheVersion) return emptyCache()
		if (!("filters" in parsed)) return emptyCache()
		if (!("views" in parsed) || typeof parsed.views !== "object" || parsed.views === null) return emptyCache()
		if (Array.isArray(parsed.views)) return emptyCache()

		const filters = parsed.filters === null ? null : isStoredFilters(parsed.filters) ? parsed.filters : null
		const views: { [_: string]: CachedAuditView } = {}

		for (const [viewKey, view] of Object.entries(parsed.views)) if (isCachedView(view)) views[viewKey] = view

		return { filters, version: cacheVersion, views }
	} catch {
		return emptyCache()
	}
}

function writeCache(store: AuditCacheStore, key: string, cache: StoredAuditCache): void {
	try {
		const cutoff = Date.now() - maxViewAge
		let views = Object.entries(cache.views)
			.filter(([, view]) => view.savedAt >= cutoff)
			.toSorted((left, right) => right[1].savedAt - left[1].savedAt)
			.slice(0, maxCachedViews)

		let next: StoredAuditCache = { ...cache, views: Object.fromEntries(views) }
		let payload = JSON.stringify(next)

		while (payload.length > maxCacheBytes && views.length > 0) {
			views = views.slice(0, -1)
			next = { ...cache, views: Object.fromEntries(views) }
			payload = JSON.stringify(next)
		}

		store.setItem(key, payload)
	} catch {
		// Storage can be unavailable or full; the in-memory cache still works.
	}
}

// createAuditCache keeps recent audit pages and the last filter selection in tab storage.
export function createAuditCache(programSlug: string, store: AuditCacheStore): AuditCache {
	const key = `review:audit:${programSlug}`

	return {
		// readFilters restores the last selection, ignoring stale or malformed copies.
		readFilters() {
			const { filters } = readCache(store, key)
			if (!filters || Date.now() - filters.savedAt > maxFilterAge) return null

			return { action: filters.action, days: filters.days, query: filters.query }
		},
		// readView returns the first page cached for these filters, if it is still fresh.
		readView(filters) {
			const view = readCache(store, key).views[auditFilterKey(filters)]
			if (!view || !isFreshAuditView(view)) return null

			return view
		},
		// writeFilters keeps the selection across reloads without touching the cached pages.
		writeFilters(filters) {
			const cache = readCache(store, key)
			cache.filters = { action: filters.action, days: filters.days, query: filters.query, savedAt: Date.now() }
			writeCache(store, key, cache)
		},
		// writeView keeps only the first page, capped so a long audit log cannot fill tab storage.
		writeView(filters, view) {
			const cache = readCache(store, key)
			const truncated = view.entries.length > maxCachedEntries

			cache.views[auditFilterKey(filters)] = {
				entries: view.entries.slice(0, maxCachedEntries),
				hasMore: view.hasMore || truncated,
				savedAt: Date.now(),
			}
			writeCache(store, key, cache)
		},
	}
}
