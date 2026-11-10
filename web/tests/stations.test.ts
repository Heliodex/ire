import { describe, expect, test } from "bun:test"
import {
	answerFor,
	initialStationKey,
	nextQuestionKey,
	type StationAnswer,
	type StationDefinition,
	type StationState,
	stationStatusLabel,
	stationTone,
	upsertStationAnswer,
	verdictLabel,
	verdictTone,
} from "../src/lib/stations"

function question(key: string, sort: number, required = 1): StationDefinition["questions"][number] {
	return { evidenceKind: "text", key, prompt: `Prompt ${key}`, required, sort }
}

function station(key: string, sort: number, overrides: Partial<StationDefinition> = {}): StationDefinition {
	return {
		active: 1,
		description: `${key} description`,
		key,
		label: key,
		questions: [question("q1", 0)],
		sort,
		stage: 1,
		themes: [],
		...overrides,
	}
}

function state(key: string, status: string, overrides: Partial<StationState> = {}): StationState {
	return {
		answered: 0,
		key,
		label: key,
		openFindings: 0,
		required: 1,
		sort: 0,
		status,
		...overrides,
	}
}

function answer(stationKey: string, questionKey: string, overrides: Partial<StationAnswer> = {}): StationAnswer {
	return {
		answer: "pass",
		evidence: "",
		id: `${stationKey}-${questionKey}`,
		questionKey,
		stationKey,
		submissionId: "ship-1",
		updatedAt: "2026-01-01T00:00:00Z",
		...overrides,
	}
}

describe("stationTone", () => {
	test("maps every workflow status to the badge palette", () => {
		expect(stationTone("done")).toBe("approved")
		expect(stationTone("current")).toBe("pending")
		expect(stationTone("blocked")).toBe("changes")
		expect(stationTone("todo")).toBe("neutral")
		expect(stationTone("unknown")).toBe("neutral")
	})
})

describe("labels", () => {
	test("names station states and verdicts", () => {
		expect(stationStatusLabel("done")).toBe("Done")
		expect(stationStatusLabel("current")).toBe("Current")
		expect(stationStatusLabel("blocked")).toBe("Blocked")
		expect(stationStatusLabel("todo")).toBe("To do")
		expect(verdictTone("pass")).toBe("approved")
		expect(verdictTone("fail")).toBe("rejected")
		expect(verdictTone("na")).toBe("neutral")
		expect(verdictLabel("pass")).toBe("Pass")
		expect(verdictLabel("fail")).toBe("Fail")
		expect(verdictLabel("na")).toBe("N/A")
		expect(verdictLabel("other")).toBe("other")
	})
})

describe("initialStationKey", () => {
	const stations = [station("triage", 0), station("time", 1), station("hardware", 2)]
	const states = [state("triage", "done"), state("time", "current"), state("hardware", "todo")]

	test("prefers the focus link, then the current station, then the first unfinished one", () => {
		expect(initialStationKey(stations, states, "hardware", "time")).toBe("hardware")
		expect(initialStationKey(stations, states, "missing", "time")).toBe("time")
		expect(initialStationKey(stations, states, undefined, "missing")).toBe("time")
		expect(initialStationKey(stations, states)).toBe("time")
	})

	test("falls back to the first station when every state is done", () => {
		const done = states.map(item => state(item.key, "done"))

		expect(initialStationKey(stations, done)).toBe("triage")
	})

	test("returns an empty key when no stations exist", () => {
		expect(initialStationKey([], [])).toBe("")
	})
})

describe("nextQuestionKey", () => {
	const item = station("triage", 0, {
		questions: [question("first", 0), question("second", 1)],
	})

	test("finds the first unanswered question in sort order", () => {
		expect(nextQuestionKey(item, [])).toBe("first")
		expect(nextQuestionKey(item, [answer("triage", "first")])).toBe("second")
	})

	test("returns undefined when every question is answered", () => {
		const answered = [answer("triage", "first"), answer("triage", "second")]

		expect(nextQuestionKey(item, answered)).toBeUndefined()
	})
})

describe("answerFor and upsertStationAnswer", () => {
	const first = answer("triage", "cover", { evidence: "old" })
	const second = answer("triage", "cover", { evidence: "new" })

	test("finds an answer by station and question", () => {
		expect(answerFor([first], "triage", "cover")).toBe(first)
		expect(answerFor([first], "time", "cover")).toBeUndefined()
	})

	test("appends a new answer and replaces an existing one without mutating the input", () => {
		const appended = upsertStationAnswer([], first)
		expect(appended).toHaveLength(1)
		expect(appended[0]).toBe(first)

		const replaced = upsertStationAnswer([first], second)
		expect(replaced).toHaveLength(1)
		expect(replaced[0]?.evidence).toBe("new")
		expect(first.evidence).toBe("old")
	})
})
