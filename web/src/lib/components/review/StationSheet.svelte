<script lang="ts">
import { tick } from "svelte"
import { type InferOutput } from "valibot"
import { type findingUpdateRequestSchema, maxNote } from "../../contract"
import { type Finding, isActive } from "../../findings"
import {
	answerFor,
	answersFor,
	initialStationKey,
	nextQuestionKey,
	questionConflict,
	type StationAnswer,
	type StationDefinition,
	type StationQuestion,
	type StationState,
	type StationVerdict,
	stationIcon,
	stationStatusLabel,
	stationTone,
	verdictLabel,
	verdictTone,
} from "../../stations"
import Icon from "../Icon.svelte"

interface Props {
	answers: StationAnswer[]
	canResolve?: boolean
	closed?: boolean
	currentStation?: string
	editable: boolean
	findings?: Finding[]
	focusStation?: string
	onAnswer(stationKey: string, questionKey: string, answer: StationVerdict, evidence: string): Promise<void>
	onCreateFinding?(request: FindingRequest): Promise<void>
	onResolve?(stationKey: string, questionKey: string, answer: StationVerdict): Promise<void>
	onUpdateFinding?(findingId: string, request: FindingUpdate): Promise<void>
	/** Pins the sheet to focusStation: other stations show status only. */
	pinned?: boolean
	reviewerId?: string
	states: StationState[]
	stations: StationDefinition[]
	version: number
}

let {
	answers,
	currentStation,
	editable,
	findings = [],
	focusStation,
	onAnswer,
	canResolve = false,
	closed = false,
	onCreateFinding,
	onResolve,
	onUpdateFinding,
	pinned = false,
	reviewerId = "",
	states,
	stations,
	version,
}: Props = $props()

const statesByKey = $derived(new Map(states.map(state => [state.key, state])))
const orderedStations = $derived(
	stations.filter(station => statesByKey.has(station.key)).toSorted((a, b) => a.sort - b.sort),
)

let selected = $state(initialStationKey(orderedStations, states, focusStation, currentStation))
let manualSelection = $state(false)
let seenFocus = focusStation ?? ""
let seenCurrent = currentStation ?? ""
let seenVersion = version
let drafts = $state<{ [_: string]: string }>({})
let errors = $state<{ [_: string]: string }>({})
let queue = $state<PendingAnswer[]>([])
let active = $state<PendingAnswer | undefined>(undefined)
let running = $state(false)
let findingKey = $state("")
let findingEdit = $state<{ id: string; status: Finding["status"] } | undefined>(undefined)
let findingSummary = $state("")
let findingAction = $state("")
let findingSeverity = $state<"block" | "fixable">("fixable")
let findingSaving = $state(false)
let findingError = $state("")

interface FindingRequest {
	detail?: string
	evidence?: string
	makerAction?: string
	question?: string
	severity: "block" | "fixable"
	station: string
	summary: string
}

type FindingUpdate = InferOutput<typeof findingUpdateRequestSchema>

interface PendingAnswer {
	evidence: string
	key: string
	questionKey: string
	stationKey: string
	verdict: StationVerdict
}

const selectedStation = $derived(orderedStations.find(item => item.key === selected) ?? orderedStations[0])
const selectedState = $derived(selectedStation ? statesByKey.get(selectedStation.key) : undefined)
const questions = $derived(selectedStation ? selectedStation.questions.toSorted((a, b) => a.sort - b.sort) : [])
const nextKey = $derived(selectedStation ? nextQuestionKey(selectedStation, answers, reviewerId) : undefined)
const allDone = $derived(states.length > 0 && states.every(state => state.status === "done"))

// A focus link from the station queue wins over the workflow's current station.
$effect((): void => {
	const focus = focusStation ?? ""
	if (focus === seenFocus) return

	seenFocus = focus
	if (focus === "" || !orderedStations.some(item => item.key === focus)) return

	selected = focus
	manualSelection = false
})

// When a save advances the current station, follow it unless the reviewer picked another station.
$effect((): void => {
	const current = currentStation ?? ""
	if (current === seenCurrent) return

	seenCurrent = current
	if (current === "" || manualSelection || (focusStation ?? "") !== "") return

	selected = current
})

// A resubmission changes the bundle version; typed evidence belongs to the previous version.
$effect((): void => {
	if (version === seenVersion) return

	seenVersion = version
	drafts = {}
	errors = {}
	queue = []
	active = undefined
	running = false
})

// If the workflow changes underneath the sheet, fall back to a station that still exists.
$effect((): void => {
	if (orderedStations.length === 0) return
	if (orderedStations.some(item => item.key === selected)) return

	selected = initialStationKey(orderedStations, states, focusStation, currentStation)
	manualSelection = false
})

function selectStation(key: string): void {
	selected = key
	manualSelection = key !== (currentStation ?? "")
}

function answerKey(stationKey: string, questionKey: string): string {
	return `${stationKey}:${questionKey}`
}

function evidenceValue(stationKey: string, questionKey: string): string {
	const key = answerKey(stationKey, questionKey)
	if (key in drafts) return drafts[key]

	return answerFor(answers, stationKey, questionKey, reviewerId)?.evidence ?? ""
}

function setEvidence(stationKey: string, questionKey: string, value: string): void {
	drafts[answerKey(stationKey, questionKey)] = value
}

// submitAnswer records one verdict with the evidence typed for that question.
// Answers are queued so a fast second click is applied after the first response
// instead of racing it or being silently dropped.
function submitAnswer(question: StationQuestion, verdict: StationVerdict): void {
	if (!editable || selectedStation === undefined) return

	const stationKey = selectedStation.key
	const key = answerKey(stationKey, question.key)
	if (active?.key === key) return

	const task: PendingAnswer = {
		evidence: evidenceValue(stationKey, question.key).trim(),
		key,
		questionKey: question.key,
		stationKey,
		verdict,
	}
	delete errors[key]
	queue = [...queue.filter(item => item.key !== key), task]
	void drain()
}

// drain applies queued answers one at a time, so each response carries the state
// written by the previous answer.
async function drain(): Promise<void> {
	if (running || queue.length === 0) return

	running = true
	const [task] = queue
	if (!task) {
		running = false
		return
	}

	queue = queue.slice(1)
	active = task

	try {
		await onAnswer(task.stationKey, task.questionKey, task.verdict, task.evidence)
		if (task.stationKey === selectedStation?.key) void scrollToNext()
	} catch (err) {
		errors[task.key] = err instanceof Error ? err.message : "Could not save this answer."
	} finally {
		active = undefined
		running = false
		void drain()
	}
}

// questionFinding returns the newest finding recorded for this question, if any.
function questionFinding(stationKey: string, prompt: string): Finding | undefined {
	return findings.find(item => isActive(item) && item.station === stationKey && item.question === prompt)
}

// openFinding starts an inline finding for a failed question, or edits the one it already has.
function openFinding(stationKey: string, question: StationQuestion): void {
	const existing = questionFinding(stationKey, question.prompt)
	findingKey = answerKey(stationKey, question.key)
	findingEdit = existing === undefined ? undefined : { id: existing.id, status: existing.status }
	findingSummary = existing?.summary ?? ""
	findingAction = existing?.makerAction ?? ""
	findingSeverity = existing?.severity === "block" ? "block" : "fixable"
	findingError = ""
}

// saveFinding sends the inline finding, creating it once and updating it after that.
async function saveFinding(stationKey: string, question: StationQuestion): Promise<void> {
	if (findingSummary.trim() === "") return

	findingSaving = true
	findingError = ""

	try {
		if (findingEdit !== undefined) {
			if (onUpdateFinding === undefined) return
			await onUpdateFinding(findingEdit.id, {
				makerAction: findingAction.trim(),
				severity: findingSeverity,
				status: findingEdit.status,
				summary: findingSummary.trim(),
			})
		} else {
			if (onCreateFinding === undefined) return
			await onCreateFinding({
				makerAction: findingAction.trim(),
				question: question.prompt,
				severity: findingSeverity,
				station: stationKey,
				summary: findingSummary.trim(),
			})
		}
		findingKey = ""
	} catch (err) {
		findingError = err instanceof Error ? err.message : "Could not save the finding."
	} finally {
		findingSaving = false
	}
}

// questionHasFinding reports whether a finding already follows this station answer.
function questionHasFinding(stationKey: string, prompt: string): boolean {
	return questionFinding(stationKey, prompt) !== undefined
}

// isFailWithoutFinding reports whether a question was failed but still needs its finding.
function isFailWithoutFinding(stationKey: string, question: StationQuestion): boolean {
	const existing = answerFor(answers, stationKey, question.key, reviewerId)
	return existing?.answer === "fail" && !questionHasFinding(stationKey, question.prompt)
}

function isQueued(key: string): boolean {
	return queue.some(item => item.key === key)
}

// scrollToNext keeps the next unanswered question in view after a verdict.
async function scrollToNext(): Promise<void> {
	await tick()
	if (nextKey === undefined) return

	const node = document.querySelector(`[data-question="${nextKey}"]`)
	if (node instanceof HTMLElement) node.scrollIntoView({ behavior: "smooth", block: "nearest" })
}

// answerNext lets a reviewer answer the next unanswered question with 1, 2 or 3.
function onKeydown(event: KeyboardEvent): void {
	if (!editable || event.metaKey || event.ctrlKey || event.altKey) return

	const { target } = event
	if (target instanceof HTMLElement && target.closest("input, textarea, select, [contenteditable='true']") !== null)
		return

	const verdicts: { [_: string]: StationVerdict } = { "1": "pass", "2": "fail", "3": "na" }
	const verdict = verdicts[event.key]
	if (verdict === undefined || nextKey === undefined) return

	const question = questions.find(item => item.key === nextKey)
	if (question === undefined) return

	event.preventDefault()
	submitAnswer(question, verdict)
}
</script>

<svelte:window onkeydown={onKeydown} />

<section aria-labelledby="station-sheet-title" class="ui-card padded station-sheet">
	<div class="sheet-head">
		<h2 id="station-sheet-title">Stations</h2>
		{#if editable}
			<p class="key-hint">
				<kbd class="ui-kbd">1</kbd>
				pass <kbd class="ui-kbd">2</kbd> fail <kbd class="ui-kbd">3</kbd> N/A
			</p>
		{/if}
		{#if selectedState}
			{#if selectedState.conflicts > 0}
				<span class="ui-badge" data-tone="changes"
					>{selectedState.conflicts}
					conflict{selectedState.conflicts === 1 ? "" : "s"}</span
				>
			{:else}
				<span class="ui-badge" data-tone={stationTone(selectedState.status)}
					>{stationStatusLabel(selectedState.status)}</span
				>
			{/if}
		{/if}
	</div>

	{#if orderedStations.length === 0}
		<p class="hint">No station workflow is configured for this programme.</p>
	{:else}
		<!-- Per-row values use $derived: a plain const inside a runes-mode each block is captured once. -->
		<ol aria-label="Station progress" class="progress">
			{#each orderedStations as station (station.key)}
				{const state = $derived(statesByKey.get(station.key))}
				<li>
					{#if pinned}
						<div
							aria-current={station.key === selected ? "true" : undefined}
							class="progress-item static"
							data-status={state?.status ?? "todo"}
						>
							<span aria-hidden="true" class="progress-mark"
								><Icon name={stationIcon(state?.status ?? "todo")} size={14} /></span
							>
							<span class="progress-label">{station.label}</span>
							{#if state}
								<span class="progress-count">{state.answered}/{state.required}</span>
							{/if}
						</div>
					{:else}
						<button
							aria-pressed={station.key === selected}
							class="progress-item"
							data-status={state?.status ?? "todo"}
							onclick={(): void => selectStation(station.key)}
							type="button"
						>
							<span aria-hidden="true" class="progress-mark"
								><Icon name={stationIcon(state?.status ?? "todo")} size={14} /></span
							>
							<span class="progress-label">{station.label}</span>
							{#if state}
								<span class="progress-count">{state.answered}/{state.required}</span>
							{/if}
						</button>
					{/if}
				</li>
			{/each}
		</ol>

		{#if allDone}
			<p class="ui-notice" data-tone="good">All stations are done. The decision is ready.</p>
		{/if}

		{#if !editable}
			<p class="ui-notice read-only-note">
				{closed
					? "Decided ship. Station answers are read-only."
					: "Read-only. Claim this ship to answer the station questions."}
			</p>
		{/if}

	{/if}
</section>

{#if selectedStation}
	<section aria-label={`${selectedStation.label} questions`} class="station-sheet">
		{const failNeedsFinding = $derived(questions.some(question => isFailWithoutFinding(selectedStation.key, question)))}
		<div class="station-body">
				<div class="station-head">
					<h3>{selectedStation.label}</h3>
					{#if selectedState}
						<small>
							{selectedState.answered}
							of {selectedState.required} required answered{selectedState.conflicts > 0
								? ` · ${selectedState.conflicts} conflict${selectedState.conflicts === 1 ? "" : "s"}`
								: ""}
						</small>
					{/if}
				</div>

				{#if selectedStation.description}
					<p class="station-description">{selectedStation.description}</p>
				{/if}

				{#if questions.length === 0}
					<p class="hint">This station has no questions.</p>
				{:else}
					<ol class="questions">
						{#each questions as question (question.key)}
							{const answer = $derived(answerFor(answers, selectedStation.key, question.key, reviewerId))}
							{const others = $derived(
								answersFor(answers, selectedStation.key, question.key).filter(
									item => item.reviewerId !== reviewerId,
								),
							)}
							{const conflict = $derived(questionConflict(answers, selectedStation.key, question.key))}
							{const key = $derived(answerKey(selectedStation.key, question.key))}
							<li
								data-question={question.key}
								class:needs-finding={answer?.answer === "fail"
									&& !questionHasFinding(selectedStation.key, question.prompt)}
								class:next={editable && answer === undefined && nextKey === question.key}
							>
								<details
									class="question-details"
									open={conflict
										|| (answer?.answer === "fail"
											&& !questionHasFinding(selectedStation.key, question.prompt))
										|| (question.key === nextKey && !failNeedsFinding)}
								>
									<summary class="question-head">
										<p class="prompt">
											{question.prompt}
											{#if question.required === 1}
												<span aria-hidden="true" class="required" title="Required">*</span>
												<span class="visually-hidden">Required</span>
											{/if}
										</p>
										<span class="question-meta">
											{#if conflict}
												<span class="ui-badge" data-tone="changes">Conflict</span>
											{:else if answer}
												<span class="ui-badge" data-tone={verdictTone(answer.answer)}
													>{verdictLabel(answer.answer)}</span
												>
											{/if}
											<span class="question-toggle">
												<Icon name="chevD" size={14} />
											</span>
										</span>
									</summary>

									{#if conflict && canResolve && onResolve !== undefined && editable}
										<div class="conflict-resolve">
											<span>Resolve every answer as</span>
											<button
												class="ui-button sm pass"
												disabled={active?.key === key}
												onclick={(): Promise<void> =>
													onResolve(selectedStation.key, question.key, "pass")}
												type="button"
											>
												Pass
											</button>
											<button
												class="ui-button sm danger"
												disabled={active?.key === key}
												onclick={(): Promise<void> =>
													onResolve(selectedStation.key, question.key, "fail")}
												type="button"
											>
												Fail
											</button>
											<button
												class="ui-button sm ghost"
												disabled={active?.key === key}
												onclick={(): Promise<void> =>
													onResolve(selectedStation.key, question.key, "na")}
												type="button"
											>
												N/A
											</button>
										</div>
									{/if}

									{#if others.length > 0}
										<div class="other-answers">
											<span class="other-label">
												{conflict ? "Reviewers disagree:" : "Also answered by:"}
											</span>
											{#each others as other (other.id)}
												<button
													class="other-answer"
													disabled={!editable || active?.key === key}
													onclick={(): Promise<void> =>
														submitAnswer(question, other.answer as StationVerdict)}
													title={editable ? "Use this answer as yours" : undefined}
													type="button"
												>
													<span>{other.reviewerName || other.reviewerId || "Reviewer"}</span>
													<span class="ui-badge" data-tone={verdictTone(other.answer)}
														>{verdictLabel(other.answer)}</span
													>
												</button>
											{/each}
										</div>
									{/if}

									{#if editable}
										<label class="evidence-field">
											<span class="visually-hidden">Evidence for {question.prompt}</span>
											<input
												class="ui-input"
												maxlength={maxNote}
												oninput={(event): void =>
													setEvidence(
														selectedStation.key,
														question.key,
														(event.currentTarget as HTMLInputElement).value,
													)}
												placeholder="Evidence or note for this answer"
												value={evidenceValue(selectedStation.key, question.key)}
											>
										</label>

										<!-- biome-ignore lint/a11y/useSemanticElements: the verdict buttons are a segmented control -->
										<div
											aria-label={`Answer: ${question.prompt}`}
											class="verdict-buttons"
											role="group"
										>
											<button
												class="ui-button sm pass"
												disabled={active?.key === key}
												onclick={(): Promise<void> => submitAnswer(question, "pass")}
												type="button"
												class:chosen={answer?.answer === "pass"}
											>
												Pass
											</button>
											<button
												class="ui-button sm danger"
												disabled={active?.key === key}
												onclick={(): Promise<void> => submitAnswer(question, "fail")}
												type="button"
												class:chosen={answer?.answer === "fail"}
											>
												Fail
											</button>
											<button
												class="ui-button sm ghost"
												disabled={active?.key === key}
												onclick={(): Promise<void> => submitAnswer(question, "na")}
												type="button"
												class:chosen={answer?.answer === "na"}
											>
												N/A
											</button>
										</div>
									{:else if answer?.evidence}
										<p class="evidence-text">{answer.evidence}</p>
									{/if}

									{#if answer?.answer === "fail"}
										{#if onCreateFinding !== undefined && editable}
											<details
												class="finding-details"
												ontoggle={(event): void => {
													if ((event.currentTarget as HTMLDetailsElement).open)
														openFinding(selectedStation.key, question)
													else if (findingKey === key) findingKey = ""
												}}
												open={findingKey === key}
											>
												<summary class="fail-row">
													<span class="fail-hint">A failed answer needs a finding.</span>
													<span class="ui-button sm changes">Record finding</span>
												</summary>
												<div class="finding-form">
													<label class="finding-field">
														<span>Summary</span>
														<input
															autocomplete="off"
															class="ui-input"
															maxlength="500"
															bind:value={findingSummary}
														>
													</label>
													<label class="finding-field">
														<span>Maker action</span>
														<input
															autocomplete="off"
															class="ui-input"
															maxlength="4000"
															placeholder="What should the maker change?"
															bind:value={findingAction}
														>
													</label>
													<label class="finding-field">
														<span>Severity</span>
														<select class="ui-input" bind:value={findingSeverity}>
															<option value="fixable">Fixable</option>
															<option value="block">Block</option>
														</select>
													</label>
													{#if findingError}
														<p class="fail-hint">{findingError}</p>
													{/if}
													<div class="finding-actions">
														<button
															class="ui-button sm ok"
															disabled={findingSaving || findingSummary.trim() === ""}
															onclick={(): Promise<void> =>
																saveFinding(selectedStation.key, question)}
															type="button"
														>
															{findingSaving
																? "Saving…"
																: findingEdit === undefined
																	? "Add finding"
																	: "Save finding"}
														</button>
														<button
															class="ui-button sm ghost"
															onclick={(): void => {
																findingKey = ""
															}}
															type="button"
														>
															Cancel
														</button>
													</div>
												</div>
											</details>
										{:else}
											<div class="fail-row">
												<p class="fail-hint">A failed answer needs a finding.</p>
											</div>
										{/if}
									{/if}

									{#if active?.key === key}
										<p class="saving" role="status">Saving…</p>
									{:else if isQueued(key)}
										<p class="saving" role="status">Queued…</p>
									{/if}

									{#if errors[key]}
										<p class="ui-notice" data-tone="bad" role="alert">{errors[key]}</p>
									{/if}
								</details>
							</li>
						{/each}
					</ol>
				{/if}
			</div>
	</section>
{/if}

<style>
.station-sheet {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
}

.sheet-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
	justify-content: space-between;
}

.sheet-head h2 {
	font-size: var(--text-md);
	font-weight: 700;
}

.key-hint {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.key-hint kbd {
	margin: 0 2px;
}

.progress {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	padding: 0;
	margin: 0;
	list-style: none;
}

.progress li {
	display: flex;
}

.progress-item {
	display: inline-flex;
	flex: 1 1 auto;
	gap: var(--space-2);
	align-items: center;
	height: var(--control-sm);
	padding: 0 var(--space-2);
	font: inherit;
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-2);
	cursor: pointer;
	background: var(--surface-2);
	border: 1px solid var(--border);
	border-radius: var(--radius-full);
}

.progress-item:hover {
	color: var(--text);
	background: var(--surface-3);
}

.progress-item.static {
	cursor: default;
}

.progress-item.static:hover {
	color: var(--text-2);
	background: var(--surface-2);
}

.progress-item[aria-pressed="true"],
.progress-item[aria-current="true"] {
	color: var(--text);
	background: color-mix(in srgb, var(--primary) 10%, var(--surface));
	border-color: var(--primary);
}

.progress-item.static[aria-current="true"]:hover {
	color: var(--text);
	background: color-mix(in srgb, var(--primary) 10%, var(--surface));
}

.progress-item[data-status="done"] {
	--station-status: var(--status-approved);
}

.progress-item[data-status="current"] {
	--station-status: var(--status-pending);
}

.progress-item[data-status="blocked"] {
	--station-status: var(--status-changes);
}

.progress-item[data-status="todo"] {
	--station-status: var(--status-neutral);
}

.progress-mark {
	display: inline-flex;
	flex: none;
	color: var(--station-status, var(--text-3));
}

.progress-count {
	margin-left: auto;
	font-weight: 400;
	font-variant-numeric: tabular-nums;
	color: var(--text-3);
}

.station-body {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.station-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: baseline;
	justify-content: space-between;
}

.station-head h3 {
	font-size: var(--text-sm);
	font-weight: 700;
}

.station-head small {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.station-description {
	max-width: 80ch;
	font-size: var(--text-sm);
	color: var(--text-2);
}

.questions {
	display: flex;
	flex-direction: column;
	padding: 0;
	margin: 0;
	overflow: hidden;
	list-style: none;
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.questions li {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: 0;
	border-left: 3px solid transparent;
}

.questions li + li {
	border-top: 1px solid var(--border);
}

.questions li.next {
	border-left-color: var(--primary);
}

.questions li.needs-finding {
	background: color-mix(in srgb, var(--status-changes) 7%, transparent);
	border-left-color: var(--status-changes);
}

.question-head {
	display: flex;
	gap: var(--space-2);
	align-items: flex-start;
	justify-content: space-between;
	padding: var(--space-2) var(--space-3);
	margin: 0 calc(-1 * var(--space-3));
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-2);
	cursor: pointer;
	list-style: none;
}

.question-head:hover {
	color: var(--text);
	background: var(--surface-3);
}

.question-head::-webkit-details-marker {
	display: none;
}

.question-meta {
	display: inline-flex;
	flex: none;
	gap: var(--space-2);
	align-items: center;
}

.question-toggle {
	display: inline-flex;
	flex: none;
	color: var(--text-3);
	transition: transform 0.15s;
}

.question-details[open] > .question-head .question-toggle {
	transform: rotate(180deg);
}

.question-details {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: 0 var(--space-3);
}

.question-details[open] {
	padding-bottom: var(--space-3);
}

.prompt {
	font-size: var(--text-xs);
	font-weight: 700;
	color: inherit;
	overflow-wrap: anywhere;
}

.required {
	margin-left: 2px;
	font-size: var(--text-sm);
	font-weight: 700;
	color: var(--accent-red);
}

.evidence-field .ui-input {
	width: 100%;
	font-size: var(--text-sm);
}

.verdict-buttons {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1);
	padding-top: var(--space-1);
}

.verdict-buttons .ui-button {
	flex: 1 1 0;
}

.verdict-buttons .chosen {
	outline: 2px solid var(--text);
	outline-offset: 1px;
}

.conflict-resolve {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1);
	align-items: center;
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--status-changes);
}

.other-answers {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1);
	align-items: center;
	font-size: var(--text-xs);
	color: var(--text-3);
}

.other-label {
	font-weight: 700;
}

.other-answer {
	display: inline-flex;
	gap: var(--space-1);
	align-items: center;
	padding: 0;
	font: inherit;
	color: var(--text-2);
	cursor: pointer;
	background: none;
	border: 0;
}

.other-answer:hover:not(:disabled) {
	color: var(--text);
}

.other-answer:disabled {
	cursor: default;
}

.fail-row {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
	justify-content: space-between;
	padding-top: var(--space-1);
}

.finding-details {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	padding-top: var(--space-1);
}

.finding-details > summary {
	cursor: pointer;
	list-style: none;
}

.finding-details > summary::-webkit-details-marker {
	display: none;
}

.finding-form {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	padding: var(--space-2);
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.finding-field {
	display: flex;
	flex-direction: column;
	gap: 2px;
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-2);
}

.finding-actions {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1);
}

.fail-hint,
.saving {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--status-changes);
}

.saving {
	font-weight: 400;
	color: var(--text-3);
}

.evidence-text {
	font-size: var(--text-sm);
	color: var(--text-2);
	overflow-wrap: anywhere;
}

.read-only-note {
	font-size: var(--text-xs);
}

.hint {
	font-size: var(--text-sm);
	color: var(--text-3);
}
</style>
