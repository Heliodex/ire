import type { Bundle } from "./api"

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
	getItem(key: string): string | null
	removeItem(key: string): void
	setItem(key: string, value: string): void
}

export interface BundleCache {
	read(target: string): Bundle | null
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

// readIndex lists cached ships, newest first, so old entries can be evicted.
function readIndex(store: BundleCacheStore, indexKey: string): BundleCacheIndexEntry[] {
	try {
		const raw = store.getItem(indexKey)
		if (!raw) return []

		const entries = JSON.parse(raw) as BundleCacheIndexEntry[]
		if (!Array.isArray(entries)) return []

		return entries.filter(entry => entry && typeof entry.id === "string" && typeof entry.savedAt === "number")
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
				if (!raw) return null

				const cached = JSON.parse(raw) as CachedBundle
				if (cached.version !== cacheVersion) return null
				if (typeof cached.savedAt !== "number" || Date.now() - cached.savedAt > maxCacheAge) return null
				if (cached.bundle?.submission?.id !== target) return null

				return cached.bundle
			} catch {
				return null
			}
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
