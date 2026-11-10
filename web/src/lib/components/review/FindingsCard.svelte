<script lang="ts">
import { tick } from "svelte"
import { type InferOutput } from "valibot"
import { type Bundle } from "../../api"
import { type findingCreateRequestSchema, type findingUpdateRequestSchema, maxNote } from "../../contract"
import { findingSeverities, findingStations, isActive, severityTone, statusLabel } from "../../findings"
import { formatDateTime } from "../../format"

type Finding = Bundle["findings"][number]
type FindingCreate = InferOutput<typeof findingCreateRequestSchema>
type FindingUpdate = InferOutput<typeof findingUpdateRequestSchema>

interface Props {
	editable: boolean
	findings: Finding[]
	onCreate(request: FindingCreate): Promise<void>
	onUpdate(id: string, request: FindingUpdate): Promise<void>
	version: number
}

let { editable, findings, onCreate, onUpdate, version }: Props = $props()

const ordered = $derived.by((): Finding[] => {
	const active = findings
		.filter(finding => isActive(finding))
		.toSorted((a, b) => b.createdAt.localeCompare(a.createdAt))
	const resolved = findings
		.filter(finding => !isActive(finding))
		.toSorted((a, b) => b.createdAt.localeCompare(a.createdAt))
	return [...active, ...resolved]
})

let severity = $state<Finding["severity"]>("fixable")
let station = $state<string>("triage")
let summary = $state("")
let makerAction = $state("")
let detail = $state("")
let evidence = $state("")
let creating = $state(false)
let formError = $state("")
let message = $state("")
let messageTone = $state<"bad" | "good">("good")
let savingId = $state("")
let resolvingId = $state("")
let resolution = $state("")
let resolutionElement = $state<HTMLInputElement | undefined>(undefined)

const busy = $derived(savingId !== "")

function resetForm(): void {
	severity = "fixable"
	station = "triage"
	summary = ""
	makerAction = ""
	detail = ""
	evidence = ""
}

async function beginResolve(findingId: string): Promise<void> {
	resolvingId = findingId
	resolution = ""
	message = ""
	await tick()
	resolutionElement?.focus()
}

function cancelResolve(): void {
	resolvingId = ""
	resolution = ""
}

// submit records a finding that follows the project across resubmissions.
async function submit(): Promise<void> {
	if (creating || busy) return

	formError = ""
	message = ""

	const text = summary.trim()
	if (text === "") {
		formError = "Summarise the finding first."
		return
	}

	const action = makerAction.trim()
	if ((severity === "block" || severity === "fixable") && action === "") {
		formError = "Say what the maker must change for a blocking or fixable finding."
		return
	}

	creating = true

	try {
		await onCreate({
			detail: detail.trim() || undefined,
			evidence: evidence.trim() || undefined,
			makerAction: action || undefined,
			severity,
			station,
			summary: text,
		})
		resetForm()
		message = "Finding added."
		messageTone = "good"
	} catch (err) {
		formError = err instanceof Error ? err.message : "Could not save this finding."
	} finally {
		creating = false
	}
}

// resolve moves a finding to its next state, requiring a resolution only for fixed.
async function resolve(finding: Finding, status: Finding["status"]): Promise<void> {
	if (busy) return

	const text = status === "fixed" ? resolution.trim() : ""
	if (status === "fixed" && text === "") {
		message = "Describe what changed before marking this fixed."
		messageTone = "bad"
		return
	}

	savingId = finding.id
	message = ""

	try {
		await onUpdate(finding.id, { resolution: text || undefined, status })
		cancelResolve()
		message = `Finding ${status}.`
		messageTone = "good"
	} catch (err) {
		message = err instanceof Error ? err.message : "Could not update this finding."
		messageTone = "bad"
	} finally {
		savingId = ""
	}
}
</script>

<div class="findings">
	{#if ordered.length === 0}
		<p class="hint">No findings yet. Record a problem here so it follows the project across resubmissions.</p>
	{:else}
		<ul class="finding-list">
			{#each ordered as finding (finding.id)}
				<li class:resolved={!isActive(finding)}>
					<div class="finding-head">
						<span class="ui-badge" data-tone={severityTone(finding.severity)}>{finding.severity}</span>
						<span class="finding-status">{statusLabel(finding.status)}</span>
						<span class="finding-station">{finding.station}</span>
						{#if finding.openedVersion < version}
							<span class="finding-version">from v{finding.openedVersion}</span>
						{/if}
						<small class="finding-when">{formatDateTime(finding.createdAt)}</small>
					</div>

					<p class="finding-summary">{finding.summary}</p>

					{#if finding.question}
						<p class="finding-line"><span>Question</span>{finding.question}</p>
					{/if}
					{#if finding.makerAction}
						<p class="finding-line"><span>Maker action</span>{finding.makerAction}</p>
					{/if}
					{#if finding.detail}
						<p class="finding-line internal">
							<span>Internal detail <span class="internal-tag">Internal</span></span>{finding.detail}
						</p>
					{/if}
					{#if finding.evidence && finding.evidence !== "{}"}
						<p class="finding-line"><span>Evidence</span>{finding.evidence}</p>
					{/if}
					{#if finding.resolution}
						<p class="finding-line resolution">
							<span>Resolution</span>{finding.resolution}
						</p>
					{/if}

					{#if editable && isActive(finding)}
						<div class="finding-actions">
							{#if resolvingId === finding.id}
								<form
									class="resolution-form"
									onsubmit={(event): void => {
										event.preventDefault()
										void resolve(finding, "fixed")
									}}
								>
									<input
										aria-label={`Resolution for ${finding.summary}`}
										class="ui-input"
										maxlength={maxNote}
										onkeydown={(event): void => {
											if (event.key === "Escape") cancelResolve()
										}}
										placeholder="What changed?"
										bind:this={resolutionElement}
										bind:value={resolution}
									>
									<button class="ui-button primary sm" disabled={busy} type="submit">
										{savingId === finding.id ? "Saving…" : "Save fixed"}
									</button>
									<button class="ui-button ghost sm" onclick={cancelResolve} type="button">
										Cancel
									</button>
								</form>
							{:else}
								<button
									class="ui-button sm"
									disabled={busy}
									onclick={(): void => void beginResolve(finding.id)}
									type="button"
								>
									Mark fixed
								</button>
							{/if}
							<button
								class="ui-button soft sm"
								disabled={busy}
								onclick={(): Promise<void> => resolve(finding, "disputed")}
								type="button"
							>
								Dispute
							</button>
							{#if finding.severity === "note"}
								<button
									class="ui-button ghost sm"
									disabled={busy}
									onclick={(): Promise<void> => resolve(finding, "accepted")}
									type="button"
								>
									Accept
								</button>
							{/if}
							<button
								class="ui-button ghost sm"
								disabled={busy}
								onclick={(): Promise<void> => resolve(finding, "withdrawn")}
								type="button"
							>
								Withdraw
							</button>
						</div>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}

	{#if message}
		<p class="ui-notice" data-tone={messageTone} role="status">{message}</p>
	{/if}

	{#if editable}
		<form
			class="finding-form"
			onsubmit={(event): void => {
				event.preventDefault()
				void submit()
			}}
		>
			<div class="form-row">
				<label class="field">
					<span>Severity</span>
					<select aria-label="Severity" class="ui-input" bind:value={severity}>
						{#each findingSeverities as option (option)}
							<option value={option}>{option}</option>
						{/each}
					</select>
				</label>
				<label class="field">
					<span>Station</span>
					<select aria-label="Station" class="ui-input" bind:value={station}>
						{#each findingStations as option (option)}
							<option value={option}>{option}</option>
						{/each}
					</select>
				</label>
			</div>

			<label class="field">
				<span class="field-head">
					<span>Summary</span>
					<span aria-hidden="true" class="field-count">{summary.length} / 500</span>
				</span>
				<input
					aria-label="Finding summary"
					class="ui-input"
					maxlength="500"
					placeholder="What is wrong? One sentence the next reviewer can scan."
					bind:value={summary}
				>
			</label>

			<label class="field">
				<span class="field-head">
					<span
						>Maker action
						{#if severity === "block" || severity === "fixable"}
							<span aria-hidden="true" class="required" title="Required">*</span>
							<span class="visually-hidden">Required</span>
						{/if}</span
					>
					<span aria-hidden="true" class="field-count">{makerAction.length} / {maxNote}</span>
				</span>
				<input
					aria-label="Maker action"
					class="ui-input"
					maxlength={maxNote}
					placeholder="What the maker must change before this can ship."
					bind:value={makerAction}
				>
			</label>

			<label class="field">
				<span class="field-head">
					<span>
						Internal detail
						<span class="internal-tag">Internal</span>
					</span>
					<span aria-hidden="true" class="field-count">{detail.length} / {maxNote}</span>
				</span>
				<textarea
					aria-label="Internal detail"
					class="ui-input"
					maxlength={maxNote}
					placeholder="Only the team sees this."
					rows="2"
					bind:value={detail}
				></textarea>
			</label>

			<label class="field">
				<span class="field-head">
					<span>Evidence</span>
					<span aria-hidden="true" class="field-count">{evidence.length} / {maxNote}</span>
				</span>
				<input
					aria-label="Evidence"
					class="ui-input"
					maxlength={maxNote}
					placeholder="Link or note that backs this finding."
					bind:value={evidence}
				>
			</label>

			{#if formError}
				<p class="ui-notice" data-tone="bad" role="alert">{formError}</p>
			{/if}

			<button class="ui-button primary" disabled={creating || busy} type="submit">
				{creating ? "Saving…" : "Add finding"}
			</button>
		</form>
	{:else}
		<p class="hint">Claim this ship to add or resolve findings.</p>
	{/if}
</div>

<style>
.findings {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
}

.hint {
	font-size: var(--text-sm);
	color: var(--text-3);
}

.finding-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: 0;
	margin: 0;
	list-style: none;
}

.finding-list li {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: var(--space-3);
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.finding-list li.resolved {
	opacity: 0.72;
}

.finding-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
	font-size: var(--text-xs);
	color: var(--text-3);
}

.finding-status {
	font-weight: 700;
	color: var(--text-2);
}

.finding-station {
	padding: 0 5px;
	background: var(--surface-3);
	border-radius: 99px;
}

.finding-version {
	font-weight: 700;
	color: var(--status-changes);
}

.finding-when {
	margin-left: auto;
	font-size: var(--text-xs);
	color: var(--text-3);
}

.finding-summary {
	font-size: var(--text-sm);
	font-weight: 600;
	color: var(--text);
	overflow-wrap: anywhere;
}

.finding-line {
	padding-left: var(--space-2);
	font-size: var(--text-sm);
	color: var(--text-2);
	overflow-wrap: anywhere;
	border-left: 2px solid var(--border);
}

.finding-line > span {
	display: block;
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-3);
}

.finding-line.resolution {
	border-left-color: var(--status-approved);
}

.finding-line.internal {
	border-left-color: var(--status-pending);
}

.finding-actions {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1);
	padding-top: var(--space-1);
}

.resolution-form {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1);
	align-items: center;
	width: 100%;
}

.resolution-form .ui-input {
	flex: 1 1 12rem;
	width: auto;
	min-width: 0;
}

.finding-form {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding-top: var(--space-2);
	border-top: 1px solid var(--border);
}

.form-row {
	display: grid;
	grid-template-columns: repeat(2, minmax(0, 1fr));
	gap: var(--space-2);
}

.field {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.field > span {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-2);
}

.field-head {
	display: flex;
	gap: var(--space-2);
	align-items: baseline;
	justify-content: space-between;
}

.field-count {
	flex: none;
	font-size: var(--text-sm);
	font-weight: 400;
	font-variant-numeric: tabular-nums;
	color: var(--text-2);
	white-space: nowrap;
}

.required {
	margin-left: 2px;
	font-size: var(--text-sm);
	font-weight: 700;
	color: var(--accent-red);
}

.finding-form > .ui-notice {
	font-size: var(--text-xs);
}
</style>
