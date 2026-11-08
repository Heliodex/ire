import { afterEach, describe, expect, setSystemTime, test } from "bun:test"
import { type AuditEntry } from "../src/lib/api"
import {
	type AuditCacheStore,
	type AuditFilters,
	auditFilterKey,
	createAuditCache,
	isFreshAuditView,
} from "../src/lib/audit-cache"

const cacheKey = "review:audit:half-life"

function memoryStore(): AuditCacheStore & { values: Map<string, string> } {
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

function filtersOf(overrides: Partial<AuditFilters> = {}): AuditFilters {
	return { action: "", days: 0, query: "", ...overrides }
}

function entryOf(id: string): AuditEntry {
	return { action: "review.decision", createdAt: "2026-02-01T12:00:00Z", id }
}

afterEach(() => setSystemTime())

describe("auditFilterKey", () => {
	test("normalises the search so casing and surrounding spaces share one page", () => {
		expect(auditFilterKey(filtersOf({ action: "review.decision", days: 7, query: "  Ada  " }))).toBe(
			auditFilterKey(filtersOf({ action: "review.decision", days: 7, query: "ada" })),
		)
		expect(auditFilterKey(filtersOf({ query: "ada" }))).not.toBe(auditFilterKey(filtersOf({ query: "grace" })))
	})
})

describe("audit filter cache", () => {
	test("restores the last selection for a day", () => {
		setSystemTime(new Date("2026-02-01T12:00:00Z"))
		const store = memoryStore()
		const cache = createAuditCache("half-life", store)
		const filters = filtersOf({ action: "member.update", days: 30, query: "riley" })

		cache.writeFilters(filters)
		expect(cache.readFilters()).toEqual(filters)

		setSystemTime(new Date("2026-02-02T13:00:00Z"))
		expect(cache.readFilters()).toBeUndefined()
	})

	test("ignores malformed and older-version copies", () => {
		const store = memoryStore()
		const cache = createAuditCache("half-life", store)

		store.setItem(cacheKey, "not json")
		expect(cache.readFilters()).toBeUndefined()

		store.setItem(cacheKey, JSON.stringify({ filters: undefined, version: 0, views: {} }))
		expect(cache.readFilters()).toBeUndefined()
	})

	test("keeps the first page keyed by the normalised filters", () => {
		setSystemTime(new Date("2026-02-01T12:00:00Z"))
		const store = memoryStore()
		const cache = createAuditCache("half-life", store)
		const filters = filtersOf({ action: "review.decision", days: 7, query: "Ada" })

		cache.writeView(filters, { entries: [entryOf("aud_1"), entryOf("aud_2")], hasMore: true })

		expect(cache.readView(filtersOf({ action: "review.decision", days: 7, query: "  ada " }))).toEqual({
			entries: [entryOf("aud_1"), entryOf("aud_2")],
			hasMore: true,
			savedAt: Date.now(),
		})

		setSystemTime(new Date("2026-02-01T12:11:00Z"))
		expect(cache.readView(filters)).toBeUndefined()
	})

	test("keeps the filters when a view is written", () => {
		const store = memoryStore()
		const cache = createAuditCache("half-life", store)
		const filters = filtersOf({ action: "note.create", days: 1, query: "timer" })

		cache.writeFilters(filters)
		cache.writeView(filters, { entries: [entryOf("aud_1")], hasMore: false })

		expect(cache.readFilters()).toEqual(filters)
	})

	test("caps a page and keeps older pages reachable", () => {
		const store = memoryStore()
		const cache = createAuditCache("half-life", store)
		const entries = Array.from({ length: 260 }, (_, index) => entryOf(`aud_${index}`))

		cache.writeView(filtersOf(), { entries, hasMore: false })

		const view = cache.readView(filtersOf())
		expect(view?.entries).toHaveLength(250)
		expect(view?.entries[0]?.id).toBe("aud_0")
		expect(view?.hasMore).toBe(true)
	})

	test("drops the least recently written filter pages", () => {
		setSystemTime(new Date("2026-02-01T12:00:00Z"))
		const store = memoryStore()
		const cache = createAuditCache("half-life", store)

		for (let index = 0; index < 10; index++) {
			setSystemTime(new Date(`2026-02-01T12:${String(index).padStart(2, "0")}:00Z`))
			cache.writeView(filtersOf({ action: `action.${index}` }), {
				entries: [entryOf(`aud_${index}`)],
				hasMore: false,
			})
		}

		expect(cache.readView(filtersOf({ action: "action.0" }))).toBeUndefined()
		expect(cache.readView(filtersOf({ action: "action.1" }))).toBeUndefined()
		expect(cache.readView(filtersOf({ action: "action.2" }))?.entries[0]?.id).toBe("aud_2")
		expect(cache.readView(filtersOf({ action: "action.9" }))?.entries[0]?.id).toBe("aud_9")
	})
})

describe("isFreshAuditView", () => {
	test("expires after ten minutes", () => {
		setSystemTime(new Date("2026-02-01T12:00:00Z"))
		const view = { entries: [entryOf("aud_1")], hasMore: false, savedAt: Date.now() }

		expect(isFreshAuditView(view)).toBe(true)

		setSystemTime(new Date("2026-02-01T12:10:01Z"))
		expect(isFreshAuditView(view)).toBe(false)
	})
})
