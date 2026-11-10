<script lang="ts">
import { type Journal, type JournalTimelapse, reviewTimelapse } from "../../api"
import { ApiError } from "../../api-error"
import { reasonLabel, reasonLabels } from "../../deflation"
import { formatHours } from "../../format"

interface Props {
	clip: JournalTimelapse
	clipIndex: number
	editable: boolean
	entry: Journal
	onSaved(totalSeconds: number): void
	shipId: string
	version: number
}

let { clip, clipIndex, editable, entry, shipId, version, onSaved }: Props = $props()

// hoursValue shows seconds as a compact hours input without trailing zeros.
function hoursValue(seconds: number): string {
	return String(Math.round((seconds / 3600) * 100) / 100)
}

// svelte-ignore state_referenced_locally
let approved = $state(hoursValue(clip.approvedSeconds ?? clip.coveredSeconds))
// svelte-ignore state_referenced_locally
let reason = $state(clip.reason ?? "")
// svelte-ignore state_referenced_locally
let note = $state(clip.note ?? "")
// svelte-ignore state_referenced_locally
let savedApproved = $state(clip.approvedSeconds ?? clip.coveredSeconds)
// svelte-ignore state_referenced_locally
let savedReason = $state(clip.reason ?? "")
// svelte-ignore state_referenced_locally
let savedNote = $state(clip.note ?? "")
let status = $state<"idle" | "saving" | "saved" | "error">("idle")
let error = $state("")

const approvedSeconds = $derived(Math.max(0, Math.round((Number(approved) || 0) * 3600)))
const changed = $derived(approvedSeconds !== savedApproved || reason !== savedReason || note !== savedNote)
const cut = $derived(clip.coveredSeconds - approvedSeconds)

// save writes the clip's approved time, reason and note, keeping the rail total in step.
async function save(): Promise<void> {
	if (!changed || status === "saving") {
		status = "idle"
		return
	}
	if (approvedSeconds < clip.coveredSeconds && reason.trim() === "") {
		status = "error"
		error = "Choose why this clip counts for less."
		return
	}

	status = "saving"
	error = ""

	try {
		const result = await reviewTimelapse(shipId, entry.id, String(clipIndex), {
			approvedSeconds,
			deflationReason: reason,
			reviewerNote: note,
			version,
		})
		savedApproved = approvedSeconds
		savedReason = reason
		savedNote = note
		const clips = entry.timelapses ?? []
		clips[clipIndex] = result.journal.timelapses?.[clipIndex] ?? clip
		entry.timelapses = clips
		onSaved(result.hoursApprovedSeconds)
		status = "saved"
	} catch (err) {
		status = "error"
		error = err instanceof ApiError ? err.message : "Could not save this clip."
	}
}
</script>

<div class="clip">
	{#if clip.url}
		<a class="clip-thumb" href={clip.url} rel="noreferrer" target="_blank" title="Open timelapse">
			{#if clip.thumbnailUrl}
				<img
					alt={clip.externalId ?? "Timelapse clip"}
					loading="lazy"
					referrerpolicy="no-referrer"
					src={clip.thumbnailUrl}
				>
			{:else}
				<span class="clip-fallback">{formatHours(clip.coveredSeconds)}</span>
			{/if}
		</a>
	{:else if clip.thumbnailUrl}
		<span class="clip-thumb">
			<img
				alt={clip.externalId ?? "Timelapse clip"}
				loading="lazy"
				referrerpolicy="no-referrer"
				src={clip.thumbnailUrl}
			>
		</span>
	{:else}
		<span class="clip-thumb"><span class="clip-fallback">{formatHours(clip.coveredSeconds)}</span></span>
	{/if}
	<span class="clip-meta">
		<span>{formatHours(clip.coveredSeconds)} covered</span>
		{#if cut > 0}
			<span class="ui-badge" data-tone="changes">Counts as {formatHours(approvedSeconds)}</span>
		{/if}
	</span>
	{#if editable}
		<label class="field">
			<span class="field-head">Counts as (hours)</span>
			<input
				aria-label="Approved hours for this clip"
				class="ui-input"
				min="0"
				onchange={(): Promise<void> => save()}
				step="0.25"
				type="number"
				bind:value={approved}
			>
		</label>
		{#if approvedSeconds < clip.coveredSeconds || reason !== ""}
			<label class="field">
				<span class="field-head"><span>Why less?</span><span class="hint">staff only</span></span>
				<select
					aria-label="Deflation reason for this clip"
					class="ui-input"
					onchange={(): Promise<void> => save()}
					bind:value={reason}
				>
					<option value="">Choose a reason</option>
					{#each Object.entries(reasonLabels) as [value, label] (value)}
						<option {value}>{label}</option>
					{/each}
				</select>
			</label>
			<label class="field">
				<span class="field-head"><span>Deflation note</span><span class="hint">staff only</span></span>
				<input
					aria-label="Deflation note for this clip"
					class="ui-input"
					onchange={(): Promise<void> => save()}
					placeholder="Why this clip counts for less"
					bind:value={note}
				>
			</label>
		{/if}
		<p aria-live="polite" class="clip-status">
			{#if status === "saving"}
				Saving…
			{:else if status === "saved"}
				Saved
			{:else if status === "error"}
				<span class="error">{error}</span>
			{/if}
		</p>
	{:else if clip.reason}
		<p class="clip-counted">· {reasonLabel(clip.reason)}</p>
	{/if}
</div>

<style>
.clip {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	min-width: 0;
}

.clip-thumb {
	display: block;
	overflow: hidden;
	border: 1px solid var(--border);
	border-radius: var(--radius-md);
}

.clip-thumb img {
	display: block;
	width: 100%;
	aspect-ratio: 16 / 9;
	object-fit: cover;
	background: var(--surface-2);
}

.clip-fallback {
	display: flex;
	align-items: center;
	justify-content: center;
	min-height: 4rem;
	font-size: var(--text-sm);
	font-weight: 700;
	color: var(--text-2);
	background: var(--surface-2);
}

.clip-meta {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1);
	align-items: center;
	font-size: var(--text-xs);
	color: var(--text-3);
}

.field {
	display: flex;
	flex-direction: column;
	gap: 2px;
}

.field-head {
	display: flex;
	gap: var(--space-2);
	align-items: baseline;
	justify-content: space-between;
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-2);
}

.field-head .hint {
	font-weight: 400;
	color: var(--text-3);
}

.clip-status {
	min-height: 1.2em;
	font-size: var(--text-xs);
	color: var(--text-3);
}

.clip-status .error {
	color: var(--accent-red);
}

.clip-counted {
	font-size: var(--text-xs);
	color: var(--text-3);
}
</style>
