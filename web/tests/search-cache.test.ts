import { describe, expect, test } from "bun:test"
import type { SearchResult } from "../src/lib/api"
import { createSearchCache } from "../src/lib/search-cache"

function result(id: string, overrides: Partial<SearchResult> = {}): SearchResult {
	return {
		externalId: id,
		hoursClaimedSeconds: 0,
		id,
		ingestedAt: "2026-01-01T00:00:00Z",
		makerName: "Maker",
		stage: 1,
		status: "ready",
		title: id,
		version: 1,
		...overrides,
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

	test("reuses cached rows that fit a longer query", () => {
		const cache = createSearchCache()

		cache.set("starb", [
			result("sub_1", { title: "Starbie" }),
			result("sub_2", { title: "Starbie case" }),
			result("sub_3", { title: "Tea Timer" }),
		])

		expect(cache.match("starbi").map(item => item.id)).toEqual(["sub_1", "sub_2"])
		expect(cache.match(" STARBI ").map(item => item.id)).toEqual(["sub_1", "sub_2"])
	})

	test("matches maker, external id and theme like the API", () => {
		const cache = createSearchCache()

		cache.set("star", [
			result("sub_1", { makerName: "Star Builder" }),
			result("sub_2", { externalId: "starlight" }),
			result("sub_3", { theme: "starboard" }),
			result("sub_4", { title: "Tea Timer" }),
		])

		expect(cache.match("star").map(item => item.id)).toEqual(["sub_1", "sub_2", "sub_3"])
	})

	test("prefers the longest cached prefix and dedupes rows", () => {
		const cache = createSearchCache()
		const shared = result("sub_1", { title: "Starbie" })
		const older = result("sub_2", { title: "Starbie case" })

		cache.set("star", [older, shared])
		cache.set("starb", [shared])

		expect(cache.match("starbi").map(item => item.id)).toEqual(["sub_1", "sub_2"])
	})

	test("returns nothing for an empty query", () => {
		const cache = createSearchCache()

		cache.set("star", [result("sub_1", { title: "Starbie" })])

		expect(cache.match("  ")).toEqual([])
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
