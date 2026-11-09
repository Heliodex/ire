import { type QueueItem } from "./api"
import { humanStatus } from "./format"

export type CompareOp = "=" | ">" | ">=" | "<" | "<=" | "!="

export type CheckCount = "error" | "fail" | "pass" | "running" | "total" | "warn"

export interface NumericFilter {
	op: CompareOp
	value: number
}

export type ChecksFilter =
	| { kind: "overallPass" }
	| {
			count: CheckCount
			kind: "count"
			op: CompareOp
			value: number
	  }

export interface QueueQuery {
	age: NumericFilter | undefined
	check: { key: string; status: string } | undefined
	checks: ChecksFilter | undefined
	journals: NumericFilter | undefined
	status: string | undefined
	text: string[]
	time: NumericFilter | undefined
	type: string | undefined
	version: NumericFilter | undefined
	week: NumericFilter | undefined
}

const durationUnits: { [_: string]: { milliseconds: number; rank: number } } = {
	d: { milliseconds: 24 * 60 * 60 * 1000, rank: 4 },
	h: { milliseconds: 60 * 60 * 1000, rank: 3 },
	m: { milliseconds: 60 * 1000, rank: 2 },
	s: { milliseconds: 1000, rank: 1 },
	w: { milliseconds: 7 * 24 * 60 * 60 * 1000, rank: 5 },
}

const statuses = new Set([
	"checking",
	"claimed",
	"decided",
	"in_review",
	"needs_attention",
	"ready",
	"stage2_hold",
	"withdrawn",
])

const checkStatuses = new Set(["error", "fail", "pass", "running", "skip", "warn"])

// isCompareOp narrows the operator captured by the filter regexes.
function isCompareOp(value: string): value is CompareOp {
	switch (value) {
		case "=":
		case ">":
		case ">=":
		case "<":
		case "<=":
		case "!=":
			return true
		default:
			return false
	}
}

// isCheckCount narrows the check count name captured by the check filter regex.
function isCheckCount(value: string): value is CheckCount {
	switch (value) {
		case "error":
		case "fail":
		case "pass":
		case "running":
		case "total":
		case "warn":
			return true
		default:
			return false
	}
}

// parseOp reads an operator, falling back when the filter has none.
function parseOp(value: string | undefined, fallback: CompareOp): CompareOp {
	if (value === undefined) return fallback

	return isCompareOp(value) ? value : fallback
}

// normalizeStatus matches the API's status spelling whether the reader types a human label or the raw key.
function normalizeStatus(value: string): string {
	return value.trim().toLowerCase().replaceAll(" ", "_").replaceAll("-", "_")
}

// parseComparison reads an optional operator and a whole number, defaulting to equality.
function parseComparison(value: string): NumericFilter | undefined {
	const match = /^([<>]=?|=|!=)?(\d+)$/.exec(value)
	if (!match) return undefined

	return { op: parseOp(match[1], "="), value: Number(match[2]) }
}

// parseDuration reads a comparison and one or more units, such as >1d, >=12h, 30m or 6h24m.
// Units must descend in size with no repeats, so 10h30m10s5m is rejected.
// A bare duration means "at least".
function parseDuration(value: string, fallback: CompareOp): NumericFilter | undefined {
	const match = /^([<>]=?|=|!=)?((?:\d+(?:\.\d+)?[smhdw])+)$/.exec(value.toLowerCase())
	if (!match) return undefined

	let total = 0
	let previousRank = Number.POSITIVE_INFINITY

	for (const part of (match[2] ?? "").matchAll(/(\d+(?:\.\d+)?)([smhdw])/g)) {
		const unit = durationUnits[part[2]]
		if (unit === undefined || unit.rank >= previousRank) return undefined

		previousRank = unit.rank
		total += Number(part[1]) * unit.milliseconds
	}

	return { op: parseOp(match[1], fallback), value: total }
}

// parseChecks reads presence filters like checks:warn or count comparisons like checks:warn<3 and checks:>=10.
function parseChecks(value: string): ChecksFilter | undefined {
	const lower = value.toLowerCase()

	if (isCheckCount(lower)) {
		if (lower === "pass") return { kind: "overallPass" }

		return { count: lower, kind: "count", op: ">", value: 0 }
	}

	const match = /^(error|fail|pass|running|total|warn)?([<>]=?|=|!=)?(\d+)$/.exec(lower)
	if (!match) return undefined

	const [, count, op, digits] = match
	if (count !== undefined && !isCheckCount(count)) return undefined

	return {
		count: count ?? "total",
		kind: "count",
		op: parseOp(op, "="),
		value: Number(digits),
	}
}

// parseQueueQuery turns the filter box into structured filters and plain search terms.
export function parseQueueQuery(input: string): QueueQuery {
	const query: QueueQuery = {
		age: undefined,
		check: undefined,
		checks: undefined,
		journals: undefined,
		status: undefined,
		text: [],
		time: undefined,
		type: undefined,
		version: undefined,
		week: undefined,
	}

	for (const token of input.trim().split(/\s+/)) {
		if (token === "") continue

		const separator = token.indexOf(":")
		if (separator === -1) {
			query.text.push(token.toLowerCase())
			continue
		}

		const key = token.slice(0, separator).toLowerCase()
		const value = token.slice(separator + 1).trim()

		switch (key) {
			case "age": {
				const age = parseDuration(value, ">=")
				if (age) query.age = age
				else query.text.push(token.toLowerCase())
				break
			}
			case "time": {
				const time = parseDuration(value, ">=")
				if (time) query.time = time
				else query.text.push(token.toLowerCase())
				break
			}
			case "check": {
				const equals = value.lastIndexOf("=")
				const checkKey = equals === -1 ? "" : value.slice(0, equals).trim().toLowerCase()
				const checkStatus =
					equals === -1
						? ""
						: value
								.slice(equals + 1)
								.trim()
								.toLowerCase()

				if (checkKey === "" || !checkStatuses.has(checkStatus)) query.text.push(token.toLowerCase())
				else query.check = { key: checkKey, status: checkStatus }

				break
			}
			case "checks": {
				const checks = parseChecks(value)
				if (checks) query.checks = checks
				else query.text.push(token.toLowerCase())
				break
			}
			case "journals": {
				const journals = parseComparison(value)
				if (journals) query.journals = journals
				else query.text.push(token.toLowerCase())
				break
			}
			case "status": {
				const status = normalizeStatus(value)
				if (statuses.has(status)) query.status = status
				else query.text.push(token.toLowerCase())
				break
			}
			case "type":
				if (value !== "") query.type = value.toLowerCase()
				else query.text.push(token.toLowerCase())
				break
			case "ver": {
				const version = parseComparison(value)
				if (version) query.version = version
				else query.text.push(token.toLowerCase())
				break
			}
			case "week": {
				const week = parseComparison(value)
				if (week) query.week = week
				else query.text.push(token.toLowerCase())
				break
			}
			default:
				query.text.push(token.toLowerCase())
		}
	}

	return query
}

export interface QueueFilterSegment {
	filter: boolean
	key: string
	text: string
	value: string
}

// queueFilterSegments splits the filter box for the highlighted overlay, marking recognised tokens.
export function queueFilterSegments(input: string): QueueFilterSegment[] {
	const segments: QueueFilterSegment[] = []

	for (const part of input.split(/(\s+)/)) {
		if (part === "") continue

		const filter = !/^\s+$/.test(part) && hasStructuredQuery(parseQueueQuery(part))
		const colon = part.indexOf(":")

		segments.push({
			filter,
			key: filter ? part.slice(0, colon) : "",
			text: part,
			value: filter ? part.slice(colon + 1) : "",
		})
	}

	return segments
}

// sortQueueFilterTokens puts recognised filters first in alphabetical order, keeping plain terms after.
export function sortQueueFilterTokens(tokens: string[]): string[] {
	const special: string[] = []
	const plain: string[] = []

	for (const token of tokens)
		if (hasStructuredQuery(parseQueueQuery(token))) special.push(token)
		else plain.push(token)

	special.sort((left, right) => left.toLowerCase().localeCompare(right.toLowerCase()))

	return [...special, ...plain]
}

// setQueueFilterToken adds one filter, replacing any existing filter with the same key.
// Activating the same value again removes it.
export function setQueueFilterToken(input: string, key: string, value: string): string {
	const token = `${key}:${value}`
	const prefix = `${key.toLowerCase()}:`
	const tokens = input.trim() === "" ? [] : input.trim().split(/\s+/)
	const existing = tokens.filter(item => item.toLowerCase().startsWith(prefix))
	const next = tokens.filter(item => !item.toLowerCase().startsWith(prefix))

	if (existing.length !== 1 || existing[0]?.toLowerCase() !== token.toLowerCase()) next.push(token)

	return sortQueueFilterTokens(next).join(" ")
}

// hasStructuredQuery reports whether the reader used one of the key:value filters.
export function hasStructuredQuery(query: QueueQuery): boolean {
	return (
		query.age !== undefined
		|| query.check !== undefined
		|| query.checks !== undefined
		|| query.journals !== undefined
		|| query.time !== undefined
		|| query.status !== undefined
		|| query.type !== undefined
		|| query.version !== undefined
		|| query.week !== undefined
	)
}

// matchesQueueQuery applies every structured filter and plain term to one ship.
export function matchesQueueQuery(item: QueueItem, query: QueueQuery): boolean {
	if (query.age !== undefined && !matchesAge(item, query.age)) return false
	if (query.time !== undefined && !matchesNumber(item.hoursClaimedSeconds * 1000, query.time)) return false
	if (query.version !== undefined && !matchesNumber(item.version, query.version)) return false
	if (query.week !== undefined && (item.week === undefined || !matchesNumber(item.week, query.week))) return false
	if (query.type !== undefined && (item.theme ?? "").trim().toLowerCase() !== query.type) return false
	if (query.status !== undefined && normalizeStatus(item.status) !== query.status) return false
	if (query.check !== undefined) {
		const { key, status } = query.check

		if (!item.checks.some(check => check.key.toLowerCase() === key && check.status.toLowerCase() === status))
			return false
	}
	if (query.checks !== undefined && !matchesChecks(item, query.checks)) return false
	if (query.journals !== undefined && !matchesNumber(item.journalCount, query.journals)) return false
	if (query.text.length === 0) return true

	const haystack = [item.title, item.makerName, humanStatus(item.status), item.theme ?? ""].join(" ").toLowerCase()
	return query.text.every(term => haystack.includes(term))
}

function matchesAge(item: QueueItem, filter: NumericFilter): boolean {
	const ingested = Date.parse(item.ingestedAt)
	if (Number.isNaN(ingested)) return false

	return matchesNumber(Date.now() - ingested, filter)
}

function matchesNumber(actual: number, filter: NumericFilter): boolean {
	switch (filter.op) {
		case "=":
			return actual === filter.value
		case ">":
			return actual > filter.value
		case ">=":
			return actual >= filter.value
		case "<":
			return actual < filter.value
		case "<=":
			return actual <= filter.value
		case "!=":
			return actual !== filter.value
		default:
			return false
	}
}

function matchesChecks(item: QueueItem, filter: ChecksFilter): boolean {
	if (filter.kind === "overallPass") return item.checkSummary.overall === "pass"

	const { error, fail, pass, running, warn } = item.checkSummary
	let count: number

	switch (filter.count) {
		case "total":
			count = error + fail + pass + running + warn
			break
		case "running":
			count = running + (item.status === "checking" && running === 0 ? 1 : 0)
			break
		case "error":
			count = error
			break
		case "fail":
			count = fail
			break
		case "pass":
			count = pass
			break
		case "warn":
			count = warn
			break
		default:
			return false
	}

	return matchesNumber(count, { op: filter.op, value: filter.value })
}

// isServerFilteredQuery reports whether the queue query uses only filters the API can apply server-side.
// All structured queue filters are applied server-side, so the client never needs to scan every page for a match.
export function isServerFilteredQuery(_query: QueueQuery): boolean {
	return true
}
