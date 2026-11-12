import type { Bundle } from "./api"
import type { IconName } from "./components/icon-paths"
import type { Tone } from "./format"

export type StationDefinition = Bundle["stations"][number]
export type StationQuestion = StationDefinition["questions"][number]
export type StationState = Bundle["stationStates"][number]
export type StationAnswer = Bundle["stationAnswers"][number]
export type StationStatus = "todo" | "current" | "done" | "blocked"
export type StationVerdict = "pass" | "fail" | "na"

// stationTone maps one station state to the badge palette used across the app.
export function stationTone(status: string): Tone {
	switch (status) {
		case "done":
			return "approved"
		case "current":
			return "pending"
		case "blocked":
			return "changes"
		default:
			return "neutral"
	}
}

// stationStatusLabel turns a station state into the short label shown beside its name.
export function stationStatusLabel(status: string): string {
	switch (status) {
		case "done":
			return "Done"
		case "current":
			return "Current"
		case "blocked":
			return "Blocked"
		default:
			return "To do"
	}
}

// stationIcon maps one station state to the progress glyph, so status reads as more than a colour.
export function stationIcon(status: string): IconName {
	switch (status) {
		case "done":
			return "checkCircle"
		case "flagged":
			return "info"
		case "current":
			return "clock"
		case "blocked":
			return "info"
		default:
			return "circle"
	}
}

// verdictTone maps one answer to its badge tone.
export function verdictTone(answer: string): Tone {
	switch (answer) {
		case "pass":
			return "approved"
		case "fail":
			return "rejected"
		default:
			return "neutral"
	}
}

// verdictLabel turns a stored answer into the label shown in the question row.
export function verdictLabel(answer: string): string {
	switch (answer) {
		case "pass":
			return "Pass"
		case "fail":
			return "Fail"
		case "na":
			return "N/A"
		default:
			return answer
	}
}

// answersFor lists every reviewer's answer to one question.
export function answersFor(answers: StationAnswer[], stationKey: string, questionKey: string): StationAnswer[] {
	return answers.filter(answer => answer.stationKey === stationKey && answer.questionKey === questionKey)
}

// answerFor finds one saved answer in the bundle. It returns the current reviewer's
// answer when reviewerID is set, and any answer otherwise.
export function answerFor(
	answers: StationAnswer[],
	stationKey: string,
	questionKey: string,
	reviewerID?: string,
): StationAnswer | undefined {
	const matches = answersFor(answers, stationKey, questionKey)
	if (reviewerID === undefined || reviewerID === "") return matches[0]

	return matches.find(answer => answer.reviewerId === reviewerID)
}

// questionConflict reports whether reviewers disagree about one question.
export function questionConflict(answers: StationAnswer[], stationKey: string, questionKey: string): boolean {
	return new Set(answersFor(answers, stationKey, questionKey).map(answer => answer.answer)).size > 1
}

// initialStationKey picks the station the sheet opens on: the focus link first, then the
// current station, then the first unfinished one, then the first station in the workflow.
export function initialStationKey(
	stations: StationDefinition[],
	states: StationState[],
	focusStation?: string,
	currentStation?: string,
): string {
	const ordered = stations.toSorted((a, b) => a.sort - b.sort)
	const byKey = new Map(states.map(state => [state.key, state]))

	if (focusStation !== undefined && focusStation !== "" && ordered.some(item => item.key === focusStation))
		return focusStation

	if (currentStation !== undefined && currentStation !== "" && ordered.some(item => item.key === currentStation))
		return currentStation

	return ordered.find(item => byKey.get(item.key)?.status !== "done")?.key ?? ordered[0]?.key ?? ""
}

// nextQuestionKey finds the first question in a station that has no answer yet.
export function nextQuestionKey(
	station: StationDefinition,
	answers: StationAnswer[],
	reviewerID?: string,
): string | undefined {
	const ordered = station.questions.toSorted((a, b) => a.sort - b.sort)

	return ordered.find(question => answerFor(answers, station.key, question.key, reviewerID) === undefined)?.key
}

// upsertStationAnswer replaces one station question's answer or appends a new one.
export function upsertStationAnswer(answers: StationAnswer[], next: StationAnswer): StationAnswer[] {
	const index = answers.findIndex(
		answer =>
			answer.stationKey === next.stationKey
			&& answer.questionKey === next.questionKey
			&& answer.reviewerId === next.reviewerId,
	)
	if (index === -1) return [...answers, next]

	return answers.map((answer, position) => (position === index ? next : answer))
}
