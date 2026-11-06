import { afterEach, describe, expect, setSystemTime, test } from "bun:test"
import type { QueueItem } from "../src/lib/api"
import {
	hasStructuredQuery,
	matchesQueueQuery,
	parseQueueQuery,
	queueFilterSegments,
	setQueueFilterToken,
	sortQueueFilterTokens,
} from "../src/lib/queue-filter"

const emptyChecks = { error: 0, fail: 0, overall: "pass", pass: 0, running: 0, warn: 0 }

function itemOf(overrides: Partial<QueueItem> = {}): QueueItem {
	return {
		checkSummary: emptyChecks,
		checks: [],
		externalId: "ext_1",
		hoursClaimedSeconds: 0,
		id: "sub_1",
		ingestedAt: "2026-02-01T12:00:00Z",
		makerEmail: "ada@example.com",
		makerName: "Ada",
		priority: 0,
		stage: 1,
		status: "ready",
		title: "Tea Timer",
		version: 1,
		...overrides,
	}
}

afterEach(() => setSystemTime())

describe("parseQueueQuery", () => {
	test("reads the supported key filters", () => {
		expect(parseQueueQuery("week:1")).toEqual({
			age: null,
			checks: null,
			status: null,
			text: [],
			time: null,
			type: null,
			version: null,
			week: { op: "=", value: 1 },
		})
		expect(parseQueueQuery("type:cad")).toEqual({
			age: null,
			checks: null,
			status: null,
			text: [],
			time: null,
			type: "cad",
			version: null,
			week: null,
		})
		expect(parseQueueQuery("status:ready")).toEqual({
			age: null,
			checks: null,
			status: "ready",
			text: [],
			time: null,
			type: null,
			version: null,
			week: null,
		})
	})

	test("reads age, version and check comparisons", () => {
		expect(parseQueueQuery("age:>1d").age).toEqual({ op: ">", value: 86_400_000 })
		expect(parseQueueQuery("age:12h").age).toEqual({ op: ">=", value: 43_200_000 })
		expect(parseQueueQuery("age:>2h30m").age).toEqual({ op: ">", value: 9_000_000 })
		expect(parseQueueQuery("time:>6h24m0s").time).toEqual({ op: ">", value: 23_040_000 })
		expect(parseQueueQuery("ver:2").version).toEqual({ op: "=", value: 2 })
		expect(parseQueueQuery("ver:>5").version).toEqual({ op: ">", value: 5 })
		expect(parseQueueQuery("checks:warn<3").checks).toEqual({
			count: "warn",
			kind: "count",
			op: "<",
			value: 3,
		})
		expect(parseQueueQuery("checks:>=10").checks).toEqual({
			count: "total",
			kind: "count",
			op: ">=",
			value: 10,
		})
		expect(parseQueueQuery("checks:warn").checks).toEqual({
			count: "warn",
			kind: "count",
			op: ">",
			value: 0,
		})
		expect(parseQueueQuery("checks:pass").checks).toEqual({ kind: "overallPass" })
	})

	test("combines filters with plain terms and normalises casing", () => {
		expect(parseQueueQuery("Week:1 Type:PCB status:Needs-Attention timer")).toEqual({
			age: null,
			checks: null,
			status: "needs_attention",
			text: ["timer"],
			time: null,
			type: "pcb",
			version: null,
			week: { op: "=", value: 1 },
		})
	})

	test("keeps unknown or incomplete prefixes as plain text", () => {
		expect(parseQueueQuery("week: one checks:maybe status:unknown type: age:1y ver:>")).toEqual({
			age: null,
			checks: null,
			status: null,
			text: ["week:", "one", "checks:maybe", "status:unknown", "type:", "age:1y", "ver:>"],
			time: null,
			type: null,
			version: null,
			week: null,
		})
	})
})

describe("queueFilterSegments", () => {
	test("marks recognised filters and keeps plain text and spacing", () => {
		expect(queueFilterSegments("week:1 tea type:cad")).toEqual([
			{ filter: true, key: "week", text: "week:1", value: "1" },
			{ filter: false, key: "", text: " ", value: "" },
			{ filter: false, key: "", text: "tea", value: "" },
			{ filter: false, key: "", text: " ", value: "" },
			{ filter: true, key: "type", text: "type:cad", value: "cad" },
		])
	})

	test("leaves unknown or incomplete filters as normal text", () => {
		expect(queueFilterSegments("status:nope week:")).toEqual([
			{ filter: false, key: "", text: "status:nope", value: "" },
			{ filter: false, key: "", text: " ", value: "" },
			{ filter: false, key: "", text: "week:", value: "" },
		])
	})
})

describe("setQueueFilterToken", () => {
	test("replaces an existing filter of the same type", () => {
		expect(setQueueFilterToken("type:pcb ver:1 open", "ver", "2")).toBe("type:pcb ver:2 open")
	})

	test("removes the filter when the same value is activated again", () => {
		expect(setQueueFilterToken("type:pcb ver:1 open", "ver", "1")).toBe("type:pcb open")
	})

	test("sorts the new filter into the special filters", () => {
		expect(setQueueFilterToken("type:pcb ver:1 open", "age", ">=4h47m")).toBe("age:>=4h47m type:pcb ver:1 open")
	})

	test("keeps plain terms after the special filters", () => {
		expect(setQueueFilterToken("open type:pcb", "ver", "1")).toBe("type:pcb ver:1 open")
	})
})

describe("sortQueueFilterTokens", () => {
	test("puts recognised filters first in alphabetical order", () => {
		expect(sortQueueFilterTokens(["ver:1", "tea", "TYPE:CAD", "age:>=4h", "timer", "week:1"])).toEqual([
			"age:>=4h",
			"TYPE:CAD",
			"ver:1",
			"week:1",
			"tea",
			"timer",
		])
	})

	test("keeps unknown tokens with the plain terms", () => {
		expect(sortQueueFilterTokens(["status:nope", "type:pcb", "timer"])).toEqual([
			"type:pcb",
			"status:nope",
			"timer",
		])
	})
})

describe("hasStructuredQuery", () => {
	test("detects key filters but not plain text", () => {
		expect(hasStructuredQuery(parseQueueQuery("timer"))).toBe(false)
		expect(hasStructuredQuery(parseQueueQuery("week:1"))).toBe(true)
		expect(hasStructuredQuery(parseQueueQuery("checks:warn"))).toBe(true)
		expect(hasStructuredQuery(parseQueueQuery("type:cad"))).toBe(true)
		expect(hasStructuredQuery(parseQueueQuery("status:ready"))).toBe(true)
		expect(hasStructuredQuery(parseQueueQuery("age:>1d"))).toBe(true)
		expect(hasStructuredQuery(parseQueueQuery("time:>6h"))).toBe(true)
		expect(hasStructuredQuery(parseQueueQuery("ver:2"))).toBe(true)
	})
})

describe("matchesQueueQuery", () => {
	test("filters by week, type and status", () => {
		expect(matchesQueueQuery(itemOf({ week: 1 }), parseQueueQuery("week:1"))).toBe(true)
		expect(matchesQueueQuery(itemOf({ week: 2 }), parseQueueQuery("week:1"))).toBe(false)
		expect(matchesQueueQuery(itemOf({ theme: "cad" }), parseQueueQuery("type:CAD"))).toBe(true)
		expect(matchesQueueQuery(itemOf({ theme: "pcb" }), parseQueueQuery("type:cad"))).toBe(false)
		expect(matchesQueueQuery(itemOf({ status: "ready" }), parseQueueQuery("status:ready"))).toBe(true)
		expect(matchesQueueQuery(itemOf({ status: "claimed" }), parseQueueQuery("status:ready"))).toBe(false)
	})

	test("filters versions exactly and with comparisons", () => {
		expect(matchesQueueQuery(itemOf({ version: 2 }), parseQueueQuery("ver:2"))).toBe(true)
		expect(matchesQueueQuery(itemOf({ version: 3 }), parseQueueQuery("ver:2"))).toBe(false)
		expect(matchesQueueQuery(itemOf({ version: 6 }), parseQueueQuery("ver:>5"))).toBe(true)
		expect(matchesQueueQuery(itemOf({ version: 5 }), parseQueueQuery("ver:>5"))).toBe(false)
	})

	test("filters claimed time comparisons", () => {
		const item = itemOf({ hoursClaimedSeconds: 6 * 3600 + 24 * 60 })
		expect(matchesQueueQuery(item, parseQueueQuery("time:>6h"))).toBe(true)
		expect(matchesQueueQuery(item, parseQueueQuery("time:>6h24m"))).toBe(false)
		expect(matchesQueueQuery(item, parseQueueQuery("time:>=6h24m"))).toBe(true)
	})

	test("filters by age", () => {
		setSystemTime(new Date("2026-02-02T13:00:00Z"))
		expect(matchesQueueQuery(itemOf(), parseQueueQuery("age:>1d"))).toBe(true)
		expect(matchesQueueQuery(itemOf(), parseQueueQuery("age:>2d"))).toBe(false)
	})

	test("filters check counts and overall pass", () => {
		const warned = itemOf({ checkSummary: { ...emptyChecks, overall: "warn", warn: 2 } })
		expect(matchesQueueQuery(warned, parseQueueQuery("checks:warn"))).toBe(true)
		expect(matchesQueueQuery(warned, parseQueueQuery("checks:warn<3"))).toBe(true)
		expect(matchesQueueQuery(warned, parseQueueQuery("checks:warn<2"))).toBe(false)
		expect(matchesQueueQuery(warned, parseQueueQuery("checks:fail"))).toBe(false)

		const tenChecks = itemOf({ checkSummary: { ...emptyChecks, pass: 7, warn: 3 } })
		expect(matchesQueueQuery(tenChecks, parseQueueQuery("checks:>=10"))).toBe(true)
		expect(matchesQueueQuery(tenChecks, parseQueueQuery("checks:>10"))).toBe(false)
		expect(matchesQueueQuery(itemOf({ status: "checking" }), parseQueueQuery("checks:running"))).toBe(true)
		expect(matchesQueueQuery(itemOf(), parseQueueQuery("checks:pass"))).toBe(true)
	})

	test("requires every plain term and still searches maker and type", () => {
		const item = itemOf({ makerName: "Ada Lovelace", theme: "cad" })
		expect(matchesQueueQuery(item, parseQueueQuery("ada cad"))).toBe(true)
		expect(matchesQueueQuery(item, parseQueueQuery("ada pcb"))).toBe(false)
	})

	test("combines structured filters and plain terms", () => {
		const item = itemOf({ checkSummary: { ...emptyChecks, overall: "warn", warn: 1 }, theme: "pcb", week: 1 })
		expect(matchesQueueQuery(item, parseQueueQuery("week:1 type:pcb checks:warn cookie"))).toBe(false)
		expect(matchesQueueQuery(item, parseQueueQuery("week:1 type:pcb checks:warn tea"))).toBe(true)
	})
})
