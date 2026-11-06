import { describe, expect, test } from "bun:test"
import {
	decisionLabel,
	formatDate,
	formatDateTime,
	formatDuration,
	formatHours,
	formatMoney,
	formatMoneyAmount,
	humanEvent,
	humanStatus,
	statusTone,
	timeAgo,
} from "../src/lib/format"

describe("formatHours", () => {
	test("renders whole and fractional hours", () => {
		expect(formatHours(0)).toBe("0h")
		expect(formatHours(3600)).toBe("1h")
		expect(formatHours(5400)).toBe("1.5h")
		expect(formatHours(5436)).toBe("1.51h")
	})
})

describe("formatDuration", () => {
	test("splits seconds into hours, minutes and seconds", () => {
		expect(formatDuration(0)).toBe("0h 0m")
		expect(formatDuration(3661)).toBe("1h 1m 1s")
		expect(formatDuration(23076)).toBe("6h 24m 36s")
	})

	test("drops a trailing zero-seconds part", () => {
		expect(formatDuration(3600)).toBe("1h 0m")
		expect(formatDuration(90)).toBe("0h 1m 30s")
	})

	test("clamps negative input", () => {
		expect(formatDuration(-5)).toBe("0h 0m")
	})
})

describe("formatDate", () => {
	test("returns a dash for missing values and echoes invalid ones", () => {
		expect(formatDate(null)).toBe("–")
		expect(formatDate(undefined)).toBe("–")
		expect(formatDate("not-a-date")).toBe("not-a-date")
	})

	test("formats a valid timestamp", () => {
		expect(formatDate("2026-01-15T12:00:00Z")).toContain("2026")
		expect(formatDateTime("2026-01-15T12:00:00Z")).toContain("2026")
		expect(formatDateTime(null)).toBe("–")
	})
})

describe("timeAgo", () => {
	test("buckets ages for the queue", () => {
		const now = new Date("2026-02-01T12:00:00Z").getTime()

		expect(timeAgo("2026-02-01T11:59:40Z", now)).toBe("just now")
		expect(timeAgo("2026-02-01T11:50:00Z", now)).toBe("10m ago")
		expect(timeAgo("2026-02-01T09:00:00Z", now)).toBe("3h ago")
		expect(timeAgo("2026-01-28T12:00:00Z", now)).toBe("4d ago")
	})

	test("echoes invalid input", () => {
		expect(timeAgo("nope")).toBe("nope")
	})
})

describe("formatMoney", () => {
	test("renders cents as dollars and dashes for missing values", () => {
		expect(formatMoney(null)).toBe("–")
		expect(formatMoney(undefined)).toBe("–")
		expect(formatMoney(1234)).toContain("12.34")
		expect(formatMoneyAmount(1234)).toContain("12.34")
		expect(formatMoneyAmount(null)).toBe("–")
	})
})

describe("statusTone", () => {
	test("maps statuses to badge tones", () => {
		expect(statusTone("pass")).toBe("approved")
		expect(statusTone("delivered")).toBe("approved")
		expect(statusTone("ready")).toBe("ready")
		expect(statusTone("claimed")).toBe("pending")
		expect(statusTone("warn")).toBe("changes")
		expect(statusTone("fail")).toBe("rejected")
		expect(statusTone("stage2_hold")).toBe("secondpass")
		expect(statusTone("something-else")).toBe("neutral")
	})
})

describe("humanStatus", () => {
	test("labels known statuses and passes the rest through", () => {
		expect(humanStatus("ready")).toBe("Ready")
		expect(humanStatus("needs_attention")).toBe("Needs attention")
		expect(humanStatus("stage2_hold")).toBe("Second pass")
		expect(humanStatus("custom")).toBe("custom")
	})
})

describe("decisionLabel", () => {
	test("labels decisions case-insensitively", () => {
		expect(decisionLabel("Approved")).toBe("Approved")
		expect(decisionLabel("changes_requested")).toBe("Changes requested")
		expect(decisionLabel("rejected")).toBe("Rejected")
	})
})

describe("humanEvent", () => {
	test("turns machine names into phrases", () => {
		expect(humanEvent("claim_expired")).toBe("Claim expired")
		expect(humanEvent("decided")).toBe("Decided")
	})
})
