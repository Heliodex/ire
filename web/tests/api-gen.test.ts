import { afterEach, describe, expect, test } from "bun:test"
import {
	fetchAuthConfig as fetchAuthConfigHand,
	fetchMe as fetchMeHand,
	fetchQueue as fetchQueueHand,
} from "../src/lib/api"
import {
	fetchAuthConfig as fetchAuthConfigGen,
	fetchMe as fetchMeGen,
	fetchQueue as fetchQueueGen,
} from "../src/lib/api.gen"

const realFetch = globalThis.fetch

afterEach(() => {
	globalThis.fetch = realFetch
})

// stubFetch answers the queue, config and profile endpoints and records every request URL.
function stubFetch(seen: string[]): void {
	const queueBody = {
		counts: { checking: 0, claimed: 0, fast: 0, mine: 0, needsAttention: 0, ready: 0, stage2Hold: 0 },
		hasMore: false,
		items: [],
	}
	const bodies: { [_: string]: unknown } = {
		"/auth/config": { dev: true, hca: false },
		"/me": { programs: [], readOnly: false, user: { email: "a@b.c", id: "u1" } },
		"/queue": queueBody,
	}
	// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- The stub implements only the call signature the client uses, not fetch's statics.
	globalThis.fetch = ((input: RequestInfo | URL): Promise<Response> => {
		const url = input instanceof Request ? input.url : input.toString()
		seen.push(url)
		const body = Object.entries(bodies).find(([fragment]) => url.includes(fragment))?.[1] ?? null
		return Promise.resolve(Response.json(body, { status: 200 }))
	}) as typeof fetch
}

describe("generated client parity", () => {
	test("fetchAuthConfig and fetchMe match", async () => {
		const seen: string[] = []
		stubFetch(seen)

		expect(await fetchAuthConfigGen()).toEqual(await fetchAuthConfigHand())
		expect(await fetchMeGen()).toEqual(await fetchMeHand())
		expect(seen).toHaveLength(4)
	})

	test("fetchQueue matches across paging and sort shapes", async () => {
		const seen: string[] = []
		stubFetch(seen)

		const shapes: Parameters<typeof fetchQueueHand>[] = [
			[],
			["ready", 1, 10, 25, "updated", -1],
			["all", 2, 0, 50, null, 1],
		]

		for (const args of shapes) expect(await fetchQueueGen(...args)).toEqual(await fetchQueueHand(...args))

		const urls = seen.filter(url => url.includes("/queue"))
		expect(urls).toHaveLength(6)
		expect(urls.filter(url => url.includes("sort=updated"))).toHaveLength(2)
		expect(urls.filter(url => url.includes("sort="))).toHaveLength(2)
		expect(urls.filter(url => url.includes("dir=desc"))).toHaveLength(2)
		expect(urls[0]).toContain("stage=1")
	})
})
