import { describe, expect, test } from "bun:test"
import type { SearchResult } from "../src/lib/api"
import { createSearchCache } from "../src/lib/search-cache"

function result(id: string): SearchResult {
	return {
		externalId: id,
		id,
		makerName: "Maker",
		stage: 1,
		status: "ready",
		title: id,
	}
}

describe("createSearchCache", () => {
	test("matches trimmed queries regardless of case", () => {
		const cache = createSearchCache()
		const found = [result("sub_1")]

		cache.set("Starbie ", found)

		expect(cache.get(" starbie")).toBe(found)
	})

	test("keeps empty results so a previous miss does not search again", () => {
		const cache = createSearchCache()

		cache.set("missing", [])

		expect(cache.get("missing")).toEqual([])
	})

	test("evicts the least recently used query", () => {
		const cache = createSearchCache(2)
		const first = [result("sub_1")]
		const second = [result("sub_2")]

		cache.set("one", first)
		cache.set("two", second)
		cache.get("one")
		cache.set("three", [result("sub_3")])

		expect(cache.get("two")).toBeUndefined()
		expect(cache.get("one")).toBe(first)
		expect(cache.get("three")).toBeDefined()
	})
})
