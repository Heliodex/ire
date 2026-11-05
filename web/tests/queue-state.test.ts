import { describe, expect, test } from "bun:test"
import type { Queue } from "../src/lib/api"
import type { BundleCacheStore } from "../src/lib/bundle-cache"
import { createQueueState, isQueueFilter, type SavedQueueState } from "../src/lib/queue-state"

// memoryStore backs the state without a browser.
function memoryStore(): BundleCacheStore {
	const map = new Map<string, string>()
	return {
		getItem: key => map.get(key) ?? null,
		removeItem: key => void map.delete(key),
		setItem: (key, value) => void map.set(key, value),
	}
}

function queue(): Queue {
	return {
		counts: { checking: 0, claimed: 0, needsAttention: 0, ready: 0, stage2Hold: 0 },
		hasMore: false,
		items: [],
	}
}

function state(): SavedQueueState {
	return {
		filter: "all",
		queue: queue(),
		savedAt: Date.now(),
		scrollY: 120,
		search: "star",
		selectedId: "sub_1",
		sortDir: 1,
		sortKey: "ship",
		stage: 1,
		version: 1,
	}
}

describe("queue state", () => {
	test("round-trips a visit", () => {
		const store = memoryStore()
		const cache = createQueueState("half-life", store)

		expect(cache.read()).toBeNull()

		cache.write(state())

		expect(cache.read()).toMatchObject({ filter: "all", search: "star", stage: 1 })
	})

	test("rejects stale versions and shapes", () => {
		const store = memoryStore()
		const cache = createQueueState("half-life", store)
		const key = "review:queue:half-life"

		for (const bad of [
			{ ...state(), version: 999 },
			{ ...state(), filter: "planets" },
			{ ...state(), stage: 3 },
			{ ...state(), queue: null },
			{ ...state(), savedAt: Date.now() - 25 * 60 * 60 * 1000 },
			{ ...state(), search: 42 },
		]) {
			store.setItem(key, JSON.stringify(bad))
			expect(cache.read()).toBeNull()
		}

		store.setItem(key, JSON.stringify({ ...state(), queue: { counts: {}, hasMore: false } }))
		expect(cache.read()).toBeNull()

		store.setItem(key, "not json")
		expect(cache.read()).toBeNull()
	})

	test("scopes visits to the program", () => {
		const store = memoryStore()

		createQueueState("half-life", store).write(state())

		expect(createQueueState("other", store).read()).toBeNull()
	})
})

describe("isQueueFilter", () => {
	test("accepts the four filters", () => {
		expect(isQueueFilter("all")).toBe(true)
		expect(isQueueFilter("mine")).toBe(true)
		expect(isQueueFilter("planets")).toBe(false)
	})
})
