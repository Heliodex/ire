import { afterEach, describe, expect, test } from "bun:test"
import type { Bundle, QueueItem } from "../src/lib/api"
import { type BundleCacheStore, createBundleCache, prefetchNextBundle } from "../src/lib/bundle-cache"

const realFetch = globalThis.fetch

afterEach(() => {
	globalThis.fetch = realFetch
})

// memoryStore backs the cache without a browser.
function memoryStore(): BundleCacheStore & { entries(): [string, string][] } {
	const map = new Map<string, string>()
	return {
		entries: () => [...map.entries()],
		getItem: key => map.get(key) ?? null,
		removeItem: key => void map.delete(key),
		setItem: (key, value) => void map.set(key, value),
	}
}

function bundle(id: string): Bundle {
	// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- Test fakes only carry the envelope the cache reads.
	return { submission: { id } } as Bundle
}

describe("bundle cache", () => {
	test("returns a written ship", () => {
		const store = memoryStore()
		const cache = createBundleCache("half-life", "ada@example.com", store)

		cache.write("sub_1", bundle("sub_1"))

		expect(cache.read("sub_1")?.submission.id).toBe("sub_1")
	})

	test("misses for other ships and other reviewers", () => {
		const store = memoryStore()
		const cache = createBundleCache("half-life", "ada@example.com", store)

		cache.write("sub_1", bundle("sub_1"))

		expect(cache.read("sub_2")).toBeNull()
		expect(createBundleCache("half-life", "oli@example.com", store).read("sub_1")).toBeNull()
	})

	test("rejects stale, versioned and mismatched copies", () => {
		const store = memoryStore()
		const cache = createBundleCache("half-life", "ada@example.com", store)
		const key = "review:bundle:half-life:ada@example.com:sub_1"

		store.setItem(
			key,
			JSON.stringify({ bundle: bundle("sub_1"), savedAt: Date.now() - 25 * 60 * 60 * 1000, version: 1 }),
		)
		expect(cache.read("sub_1")).toBeNull()

		store.setItem(key, JSON.stringify({ bundle: bundle("sub_1"), savedAt: Date.now(), version: 999 }))
		expect(cache.read("sub_1")).toBeNull()

		store.setItem(key, JSON.stringify({ bundle: bundle("sub_2"), savedAt: Date.now(), version: 1 }))
		expect(cache.read("sub_1")).toBeNull()

		store.setItem(key, "not json")
		expect(cache.read("sub_1")).toBeNull()
	})

	test("keeps only the last few ships", () => {
		const store = memoryStore()
		const cache = createBundleCache("half-life", "ada@example.com", store)

		for (const id of ["sub_1", "sub_2", "sub_3", "sub_4"]) cache.write(id, bundle(id))

		expect(cache.read("sub_1")).toBeNull()
		expect(cache.read("sub_4")?.submission.id).toBe("sub_4")
	})

	test("removing drops the copy and the index entry", () => {
		const store = memoryStore()
		const cache = createBundleCache("half-life", "ada@example.com", store)

		cache.write("sub_1", bundle("sub_1"))
		cache.remove("sub_1")

		expect(cache.read("sub_1")).toBeNull()
	})
})

function item(id: string, claimedBy?: string): QueueItem {
	return {
		checkSummary: { error: 0, fail: 0, overall: "pass", pass: 1, running: 0, warn: 0 },
		checks: [],
		claimedBy,
		externalId: id,
		hoursClaimedSeconds: 3600,
		id,
		ingestedAt: "2026-01-01T00:00:00Z",
		makerName: "Maker",
		priority: 0,
		stage: 1,
		status: "ready",
		title: id,
		version: 1,
	}
}

// stubQueue answers the queue endpoint with the given items and records every request URL.
function stubQueue(items: QueueItem[], seen: string[], status = 200): void {
	const queue = {
		counts: { checking: 0, claimed: 0, needsAttention: 0, ready: 0, stage2Hold: 0 },
		hasMore: false,
		items,
	}
	// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- The stub implements only the call signature the client uses, not fetch's statics.
	globalThis.fetch = ((input: RequestInfo | URL): Promise<Response> => {
		const url = input instanceof Request ? input.url : input.toString()
		seen.push(url)

		if (url.includes("/queue?")) return Promise.resolve(Response.json(queue, { status }))
		return Promise.resolve(Response.json({ error: "unreachable bundle" }, { status: 500 }))
	}) as typeof fetch
}

describe("prefetchNextBundle", () => {
	test("fetches the first unclaimed, uncached ship", async () => {
		const store = memoryStore()
		const cache = createBundleCache("half-life", "ada@example.com", store)
		const seen: string[] = []
		stubQueue([item("sub_1"), item("sub_2", "oli@example.com"), item("sub_3")], seen)

		await prefetchNextBundle("sub_1", 1, "ada@example.com", cache)

		expect(seen.some(url => url.includes("/queue?"))).toBe(true)
		expect(seen.filter(url => url.includes("/submissions/"))).toHaveLength(1)
		expect(seen.some(url => url.includes("/submissions/sub_3"))).toBe(true)
	})

	test("does nothing when every ship is claimed", async () => {
		const store = memoryStore()
		const cache = createBundleCache("half-life", "ada@example.com", store)
		const seen: string[] = []
		stubQueue([item("sub_1"), item("sub_2", "oli@example.com")], seen)

		await prefetchNextBundle("sub_1", 1, "ada@example.com", cache)

		expect(seen.filter(url => url.includes("/submissions/"))).toHaveLength(0)
	})

	test("skips ships already in the cache", async () => {
		const store = memoryStore()
		const cache = createBundleCache("half-life", "ada@example.com", store)
		store.setItem(
			"review:bundle:half-life:ada@example.com:sub_3",
			JSON.stringify({ bundle: { submission: { id: "sub_3" } }, savedAt: Date.now(), version: 1 }),
		)

		const seen: string[] = []
		stubQueue([item("sub_1"), item("sub_3"), item("sub_4")], seen)

		await prefetchNextBundle("sub_1", 1, "ada@example.com", cache)

		expect(seen.some(url => url.includes("/submissions/sub_4"))).toBe(true)
		expect(seen.some(url => url.includes("/submissions/sub_3"))).toBe(false)
	})

	test("treats a failed queue fetch as best effort", async () => {
		const store = memoryStore()
		const cache = createBundleCache("half-life", "ada@example.com", store)
		const seen: string[] = []
		stubQueue([], seen, 500)

		await prefetchNextBundle("sub_1", 1, "ada@example.com", cache)

		expect(seen.some(url => url.includes("/queue?"))).toBe(true)
	})
})
