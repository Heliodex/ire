import { afterEach, describe, expect, setSystemTime, test } from "bun:test"
import { parse } from "valibot"
import type { Overview } from "../src/lib/api"
import { overviewResponseSchema } from "../src/lib/contract"
import { createOverviewCache, isFreshOverview } from '../src/lib/overview-cache';
import type { OverviewCacheStore } from '../src/lib/overview-cache';

const cacheKey = "review:overview:half-life"

function memoryStore(): OverviewCacheStore & { values: Map<string, string> } {
	const values = new Map<string, string>()

	return {
		getItem: key => values.get(key) ?? undefined,
		removeItem: key => {
			values.delete(key)
		},
		setItem: (key, value) => {
			values.set(key, value)
		},
		values,
	}
}

function overviewOf(): Overview {
	return parse(overviewResponseSchema, {
		byStatus: [],
		daily: [],
		leaderboard: [],
		needsReview: 3,
		newToday: 1,
		outcomes: [],
		reviewedToday: 0,
		turnaround: { decisions: 0, medianDecisionMs: 0, medianFirstMs: 0 },
		wait: { count: 0, maxMs: 0, p50Ms: 0, p95Ms: 0 },
	})
}

afterEach(() => setSystemTime())

describe("isFreshOverview", () => {
	test("keeps views for a minute", () => {
		setSystemTime(new Date("2026-02-01T12:00:00Z"))
		expect(isFreshOverview({ overview: overviewOf(), savedAt: Date.now() })).toBe(true)

		setSystemTime(new Date("2026-02-01T12:01:01Z"))
		expect(isFreshOverview({ overview: overviewOf(), savedAt: Date.parse("2026-02-01T12:00:00Z") })).toBe(false)
	})
})

describe("overview cache", () => {
	test("round-trips the last view", () => {
		setSystemTime(new Date("2026-02-01T12:00:00Z"))
		const store = memoryStore()
		const cache = createOverviewCache("half-life", store)

		expect(cache.read()).toBeUndefined()

		cache.write(overviewOf())
		expect(cache.read()).toEqual({ overview: overviewOf(), savedAt: Date.now() })
		const stored: unknown = JSON.parse(store.values.get(cacheKey) ?? "{}")
		expect(typeof stored === "object" && stored !== null && "version" in stored && stored.version).toBe(1)
	})

	test("drops stale and malformed copies", () => {
		setSystemTime(new Date("2026-02-01T12:00:00Z"))
		const store = memoryStore()
		const cache = createOverviewCache("half-life", store)
		cache.write(overviewOf())

		setSystemTime(new Date("2026-02-01T12:02:00Z"))
		expect(createOverviewCache("half-life", store).read()).toBeUndefined()

		store.setItem(cacheKey, "not json")
		expect(createOverviewCache("half-life", store).read()).toBeUndefined()

		store.setItem(cacheKey, JSON.stringify({ overview: { needsReview: "three" }, savedAt: Date.now(), version: 1 }))
		expect(createOverviewCache("half-life", store).read()).toBeUndefined()
	})
})
