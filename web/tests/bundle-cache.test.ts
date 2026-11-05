import { describe, expect, test } from "bun:test"
import type { Bundle } from "../src/lib/api"
import { type BundleCacheStore, createBundleCache } from "../src/lib/bundle-cache"

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
