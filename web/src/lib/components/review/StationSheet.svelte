<script lang="ts">
import { maxNote } from "../../contract"
import {
	answerFor,
	initialStationKey,
	nextQuestionKey,
	type StationAnswer,
	type StationDefinition,
	type StationQuestion,
	type StationState,
	type StationVerdict,
	stationStatusLabel,
	stationTone,
	verdictLabel,
	verdictTone,
} from "../../stations"

interface Props {
	answers: StationAnswer[]
	currentStation?: string
	editable: boolean
	focusStation?: string
	onAnswer(stationKey: string, questionKey: string, answer: StationVerdict, evidence: string): Promise<void>
	states: StationState[]
	stations: StationDefinition[]
	version: number
}

let { answers, currentStation, editable, focusStation, onAnswer, states, stations, version }: Props = $props()

const orderedStations = $derived(stations.toSorted((a, b) => a.sort - b.sort))
const statesByKey = $derived(new Map(states.map(state => [state.key, state])))

let selected = $state(initialStationKey(orderedStations, states, focusStation, currentStation))
let manualSelection = $state(false)
let seenFocus = focusStation ?? ""
let seenCurrent = currentStation ?? ""
let seenVersion = version
let drafts = $state<{ [_: string]: string }>({})
let errors = $state<{ [_: string]: string }>({})
let savingKey = $state("")

const selectedStation = $derived(orderedStations.find(item => item.key === selected) ?? orderedStations[0])
const selectedState = $derived(selectedStation ? statesByKey.get(selectedStation.key) : undefined)
const questions = $derived(selectedStation ? selectedStation.questions.toSorted((a, b) => a.sort - b.sort) : [])
const nextKey = $derived(selectedStation ? nextQuestionKey(selectedStation, answers) : undefined)
const allDone = $derived(states.length > 0 && states.every(state => state.status === "done"))
const busy = $derived(savingKey !== "")

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
	if (current === "" || manualSelection) return

	selected = current
})

// A resubmission changes the bundle version; typed evidence belongs to the previous version.
$effect((): void => {
	if (version === seenVersion) return

	seenVersion = version
	drafts = {}
	errors = {}
	savingKey = ""
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

	return answerFor(answers, stationKey, questionKey)?.evidence ?? ""
}

function setEvidence(stationKey: string, questionKey: string, value: string): void {
	drafts[answerKey(stationKey, questionKey)] = value
}

// submitAnswer records one verdict with the evidence typed for that question.
async function submitAnswer(question: StationQuestion, verdict: StationVerdict): Promise<void> {
	if (!editable || savingKey !== "" || selectedStation === undefined) return

	const stationKey = selectedStation.key
	const key = answerKey(stationKey, question.key)
	delete errors[key]
	savingKey = key

	try {
		await onAnswer(stationKey, question.key, verdict, evidenceValue(stationKey, question.key).trim())
	} catch (err) {
		errors[key] = err instanceof Error ? err.message : "Could not save this answer."
	} finally {
		savingKey = ""
	}
}
</script>

<section aria-labelledby="station-sheet-title" class="ui-card padded station-sheet">
	<div class="sheet-head">
		<h2 id="station-sheet-title">Stations</h2>
		{#if selectedState}
			<span class="ui-badge" data-tone={stationTone(selectedState.status)}
				>{stationStatusLabel(selectedState.status)}</span
			>
		{/if}
	</div>

	{#if orderedStations.length === 0}
		<p class="hint">No station workflow is configured for this programme.</p>
	{:else}
		<ol aria-label="Station progress" class="progress">
			{#each orderedStations as station (station.key)}
				{const state = statesByKey.get(station.key)}
				<li>
					<button
						aria-pressed={station.key === selected}
						class="progress-item"
						data-status={state?.status ?? "todo"}
						onclick={(): void => selectStation(station.key)}
						type="button"
					>
						<span aria-hidden="true" class="progress-dot"></span>
						<span class="progress-label">{station.label}</span>
						{#if state}
							<span class="progress-count">{state.answered}/{state.required}</span>
						{/if}
					</button>
				</li>
			{/each}
		</ol>

		{#if allDone}
			<p class="ui-notice" data-tone="good">All stations are done. The decision is ready.</p>
		{/if}

		{#if !editable}
			<p class="ui-notice read-only-note">Read-only. Claim this ship to answer the station questions.</p>
		{/if}

		{#if selectedStation}
			<div class="station-body">
				<div class="station-head">
					<h3>{selectedStation.label}</h3>
					{#if selectedState}
						<small>{selectedState.answered} of {selectedState.required} required answered</small>
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
							{const answer = answerFor(answers, selectedStation.key, question.key)}
							{const key = answerKey(selectedStation.key, question.key)}
							<li class:next={editable && answer === undefined && nextKey === question.key}>
								<div class="question-head">
									<p class="prompt">
										{question.prompt}
										{#if question.required === 1}
											<span class="required">Required</span>
										{/if}
									</p>
									{#if answer}
										<span class="ui-badge" data-tone={verdictTone(answer.answer)}
											>{verdictLabel(answer.answer)}</span
										>
									{/if}
								</div>

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
									<div aria-label={`Answer: ${question.prompt}`} class="verdict-buttons" role="group">
										<button
											class="ui-button sm ok"
											disabled={busy}
											onclick={(): Promise<void> => submitAnswer(question, "pass")}
											type="button"
											class:chosen={answer?.answer === "pass"}
										>
											Pass
										</button>
										<button
											class="ui-button sm danger"
											disabled={busy}
											onclick={(): Promise<void> => submitAnswer(question, "fail")}
											type="button"
											class:chosen={answer?.answer === "fail"}
										>
											Fail
										</button>
										<button
											class="ui-button sm ghost"
											disabled={busy}
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
									<p class="fail-hint">Add a finding for this below.</p>
								{/if}

								{#if savingKey === key}
									<p class="saving" role="status">Saving…</p>
								{/if}

								{#if errors[key]}
									<p class="ui-notice" data-tone="bad" role="alert">{errors[key]}</p>
								{/if}
							</li>
						{/each}
					</ol>
				{/if}
			</div>
		{/if}
	{/if}
</section>

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

.progress {
	display: flex;
	gap: var(--space-1);
	padding: 0;
	margin: 0;
	overflow-x: auto;
	list-style: none;
}

.progress li {
	flex: 0 0 auto;
}

.progress-item {
	display: inline-flex;
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

.progress-item[aria-pressed="true"] {
	color: var(--text);
	background: color-mix(in srgb, var(--primary) 10%, var(--surface));
	border-color: var(--primary);
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

.progress-dot {
	flex: none;
	width: 0.5rem;
	height: 0.5rem;
	background: var(--station-status, var(--text-3));
	border-radius: 50%;
}

.progress-count {
	font-weight: 400;
	font-variant-numeric: tabular-nums;
	color: var(--text-3);
}

.station-body {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding-top: var(--space-2);
	border-top: 1px solid var(--border);
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
	gap: var(--space-2);
	padding: 0;
	margin: 0;
	list-style: none;
}

.questions li {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: var(--space-3);
	background: var(--surface-2);
	border-left: 3px solid transparent;
	border-radius: var(--radius-sm);
}

.questions li.next {
	border-left-color: var(--primary);
}

.question-head {
	display: flex;
	gap: var(--space-2);
	align-items: flex-start;
	justify-content: space-between;
}

.prompt {
	font-size: var(--text-sm);
	font-weight: 600;
	color: var(--text);
	overflow-wrap: anywhere;
}

.required {
	padding: 0 5px;
	margin-left: var(--space-1);
	font-size: 0.6rem;
	font-weight: 700;
	vertical-align: 1px;
	color: var(--text-3);
	text-transform: uppercase;
	letter-spacing: 0.04em;
	background: var(--surface-3);
	border-radius: 99px;
}

.evidence-field .ui-input {
	width: 100%;
	font-size: var(--text-sm);
}

.verdict-buttons {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1);
}

.verdict-buttons .chosen {
	outline: 2px solid var(--text);
	outline-offset: 1px;
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
