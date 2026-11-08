import { fetchBundle, fetchQueue } from './api';
import type { Bundle } from './api';

export interface CachedBundle {
	bundle: Bundle
	savedAt: number
	version: number
}

export interface BundleCacheIndexEntry {
	id: string
	savedAt: number
}

export interface BundleCacheStore {
	getItem(key: string): string | null | undefined
	removeItem(key: string): void
	setItem(key: string, value: string): void
}

export interface BundleCache {
	read(target: string): Bundle | undefined
	remove(target: string): void
	write(target: string, next: Bundle): void
}

const cacheVersion = 1
const maxCachedBundles = 3
const maxCachedBundleBytes = 1_500_000
const maxCacheAge = 24 * 60 * 60 * 1000

// keyFor scopes one ship's copy to its reviewer.
function keyFor(programSlug: string, owner: string, target: string): string {
	return `review:bundle:${programSlug}:${owner}:${target}`
}

// indexKeyFor scopes the eviction index to its reviewer.
function indexKeyFor(programSlug: string, owner: string): string {
	return `review:bundle:${programSlug}:${owner}:index`
}

// isIndexEntry reports a usable eviction entry.
function isIndexEntry(value: unknown): value is BundleCacheIndexEntry {
	if (typeof value !== "object" || value === null) return false

	return "id" in value && typeof value.id === "string" && "savedAt" in value && typeof value.savedAt === "number"
}

// isCachedBundle reports a copy stored for the same ship. It checks the envelope, not the
// whole bundle: the version field already invalidates copies from older contract shapes.
function isCachedBundle(value: unknown, version: number, target: string): value is CachedBundle {
	if (typeof value !== "object" || value === null) return false
	if (!("version" in value) || value.version !== version) return false
	if (!("savedAt" in value) || typeof value.savedAt !== "number") return false
	if (!("bundle" in value) || typeof value.bundle !== "object" || value.bundle === null) return false

	const { bundle } = value
	if (!("submission" in bundle) || typeof bundle.submission !== "object" || bundle.submission === null) return false

	return "id" in bundle.submission && bundle.submission.id === target
}

// readIndex lists cached ships, newest first, so old entries can be evicted.
function readIndex(store: BundleCacheStore, indexKey: string): BundleCacheIndexEntry[] {
	try {
		const raw = store.getItem(indexKey)
		if (typeof raw !== "string" || raw === "") return []

		const parsed: unknown = JSON.parse(raw)
		if (!Array.isArray(parsed)) return []

		return parsed.filter(isIndexEntry)
	} catch {
		return []
	}
}

// createBundleCache keeps the last few ships in tab storage so a reopened ship paints without waiting for the network.
export function createBundleCache(programSlug: string, owner: string, store: BundleCacheStore): BundleCache {
	const indexKey = indexKeyFor(programSlug, owner)

	return {
		// read returns a recently saved copy for the same ship.
		read(target) {
			try {
				const raw = store.getItem(keyFor(programSlug, owner, target))
				if (typeof raw !== "string" || raw === "") return

				const cached: unknown = JSON.parse(raw)
				if (!isCachedBundle(cached, cacheVersion, target)) return
				if (Date.now() - cached.savedAt > maxCacheAge) return

				return cached.bundle
			} catch {}
		},
		// remove drops a copy the reviewer can no longer open.
		remove(target) {
			try {
				store.removeItem(keyFor(programSlug, owner, target))
				store.setItem(indexKey, JSON.stringify(readIndex(store, indexKey).filter(entry => entry.id !== target)))
			} catch {
				// Storage can be unavailable; the next read will simply miss.
			}
		},
		// write keeps the last few ships and skips oversized bundles that would fill the tab's storage.
		write(target, next) {
			try {
				const savedAt = Date.now()
				const payload = JSON.stringify({ bundle: next, savedAt, version: cacheVersion } satisfies CachedBundle)

				if (payload.length > maxCachedBundleBytes) {
					store.removeItem(keyFor(programSlug, owner, target))
					return
				}

				const index = [
					{ id: target, savedAt },
					...readIndex(store, indexKey).filter(entry => entry.id !== target),
				]
				for (const entry of index.slice(maxCachedBundles))
					store.removeItem(keyFor(programSlug, owner, entry.id))

				store.setItem(keyFor(programSlug, owner, target), payload)
				store.setItem(indexKey, JSON.stringify(index.slice(0, maxCachedBundles)))
			} catch {
				// Storage can be unavailable or full; the page still works without a cache.
			}
		},
	}
}

// prefetchNextBundle fetches the first unclaimed ship after this one and keeps it in the tab cache.
export async function prefetchNextBundle(
	currentID: string,
	stage: number,
	owner: string | undefined,
	cache: BundleCache,
): Promise<void> {
	try {
		const queue = await fetchQueue("all", stage, 0, 5)
		const next = queue.items.find(
			item =>
				item.id !== currentID
				&& (item.claimedBy === undefined || item.claimedBy === "" || item.claimedBy === owner)
				&& cache.read(item.id) === undefined,
		)
		if (next === undefined) return

		cache.write(next.id, await fetchBundle(next.id))
	} catch {
		// Prefetching is best effort; opening the ship still loads it normally.
	}
}
