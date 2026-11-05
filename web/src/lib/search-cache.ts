import type { SearchResult } from "./api"

export interface SearchCache {
	get(query: string): SearchResult[] | undefined
	set(query: string, results: SearchResult[]): void
}

// keyFor normalises a query so casing and surrounding spaces share one entry.
function keyFor(query: string): string {
	return query.trim().toLowerCase()
}

// createSearchCache keeps recent query results for quick retyping and backspacing.
export function createSearchCache(maxEntries = 50): SearchCache {
	const entries = new Map<string, SearchResult[]>()

	return {
		get(query) {
			const key = keyFor(query)
			const results = entries.get(key)
			if (!results) return

			// Refresh the entry so the least recently used query leaves first.
			entries.delete(key)
			entries.set(key, results)

			return results
		},
		set(query, results) {
			const key = keyFor(query)

			entries.delete(key)
			entries.set(key, results)

			if (entries.size <= maxEntries) return

			const oldest = entries.keys().next().value
			if (oldest === undefined) return

			entries.delete(oldest)
		},
	}
}
