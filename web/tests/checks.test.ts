import { describe, expect, test } from "bun:test"
import {
	checkGroupLabel,
	checkStatusLabel,
	checkTone,
	groupChecks,
	isBlocking,
	isWarning,
	overallChecks,
} from "../src/lib/checks"

describe("checkTone", () => {
	test("maps pass, warn, fail and error", () => {
		expect(checkTone({ severity: "info", status: "pass" })).toBe("approved")
		expect(checkTone({ severity: "info", status: "warn" })).toBe("pending")
		expect(checkTone({ severity: "warn", status: "fail" })).toBe("changes")
		expect(checkTone({ severity: "block", status: "fail" })).toBe("rejected")
		expect(checkTone({ severity: "block", status: "error" })).toBe("rejected")
		expect(checkTone({ severity: "info", status: "skip" })).toBe("neutral")
	})
})

describe("checkGroupLabel", () => {
	test("labels known groups and passes the rest through", () => {
		expect(checkGroupLabel("repo")).toBe("Repository")
		expect(checkGroupLabel("evidence")).toBe("Evidence")
		expect(checkGroupLabel("fraud")).toBe("Fraud")
		expect(checkGroupLabel("custom")).toBe("custom")
	})
})

describe("checkStatusLabel", () => {
	test("labels known statuses and passes the rest through", () => {
		expect(checkStatusLabel("pass")).toBe("Passed")
		expect(checkStatusLabel("warn")).toBe("Warning")
		expect(checkStatusLabel("fail")).toBe("Failed")
		expect(checkStatusLabel("error")).toBe("Error")
		expect(checkStatusLabel("skip")).toBe("Skipped")
		expect(checkStatusLabel("queued")).toBe("queued")
	})
})

describe("isWarning", () => {
	test("flags warnings and warn-severity failures", () => {
		expect(isWarning({ severity: "info", status: "warn" })).toBe(true)
		expect(isWarning({ severity: "warn", status: "fail" })).toBe(true)
		expect(isWarning({ severity: "block", status: "fail" })).toBe(false)
		expect(isWarning({ severity: "info", status: "pass" })).toBe(false)
	})
})

describe("isBlocking", () => {
	test("flags block-severity failures and errors", () => {
		expect(isBlocking({ severity: "block", status: "fail" })).toBe(true)
		expect(isBlocking({ severity: "block", status: "error" })).toBe(true)
		expect(isBlocking({ severity: "block", status: "pass" })).toBe(false)
		expect(isBlocking({ severity: "warn", status: "fail" })).toBe(false)
	})
})

describe("overallChecks", () => {
	test("prefers blocking over warnings over passing", () => {
		const pass = { severity: "info", status: "pass" }
		const warn = { severity: "info", status: "warn" }
		const block = { severity: "block", status: "fail" }

		expect(overallChecks([])).toBe("pass")
		expect(overallChecks([pass])).toBe("pass")
		expect(overallChecks([pass, warn])).toBe("warn")
		expect(overallChecks([warn, block])).toBe("fail")
	})
})

describe("groupChecks", () => {
	test("returns groups in review order with unknown groups last", () => {
		const grouped = groupChecks([
			{ group: "fraud", key: "a" },
			{ group: "repo", key: "b" },
			{ group: "custom", key: "c" },
			{ group: "repo", key: "d" },
		])

		expect(grouped.map(group => group.group)).toEqual(["repo", "fraud", "custom"])
		expect(grouped[0].items.map(item => item.key)).toEqual(["b", "d"])
	})
})
