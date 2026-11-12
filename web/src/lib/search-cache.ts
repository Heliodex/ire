import type { SearchResult } from "./api"

export interface SearchCache {
	get(query: string): SearchResult[] | undefined
	match(query: string): SearchResult[]
	set(query: string, results: SearchResult[]): void
}

// keyFor normalises a query so casing and surrounding spaces share one entry.
function keyFor(query: string): string {
	return query.trim().toLowerCase()
}

// matchesSearch mirrors the API's searchable fields, so a cached row only fills in when the new query would find it.
function matchesSearch(result: SearchResult, query: string): boolean {
	const needle = query.toLowerCase()

	return [result.title, result.makerName, result.externalId, result.theme ?? ""].some(value =>
		value.toLowerCase().includes(needle),
	)
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
		// match reuses rows from earlier queries that also fit the typed text, so extending a query can show
		// the known projects immediately while the API answers. Earlier prefixes come first, longest first.
		match(query) {
			const key = keyFor(query)
			if (key === "") return []

			const keys = [...entries.keys()]
			const prefixes = keys.filter(entry => key.startsWith(entry)).toSorted((a, b) => b.length - a.length)
			const rest = keys.filter(entry => !key.startsWith(entry)).toReversed()
			const found: SearchResult[] = []
			const seen = new Set<string>()

			for (const entry of [...prefixes, ...rest])
				for (const result of entries.get(entry) ?? []) {
					if (seen.has(result.id) || !matchesSearch(result, key)) continue

					seen.add(result.id)
					found.push(result)
				}

			return found
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
