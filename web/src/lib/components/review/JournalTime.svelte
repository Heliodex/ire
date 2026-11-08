<script lang="ts">
import { onDestroy } from "svelte"
import { reviewJournal } from '../../api';
import type { Journal } from '../../api';
import { ApiError } from "../../api-error"
import { formatHours } from "../../format"

interface Props {
	editable: boolean
	entry: Journal
	onSaved(seconds: number): void
	shipId: string
	version: number
}

let { editable, entry, onSaved, shipId, version }: Props = $props()

// svelte-ignore state_referenced_locally
let approved = $state(hoursValue(entry.approvedSeconds ?? entry.seconds))
// svelte-ignore state_referenced_locally
let note = $state(entry.reviewerNote ?? "")
// svelte-ignore state_referenced_locally
let savedApproved = $state(entry.approvedSeconds ?? entry.seconds)
// svelte-ignore state_referenced_locally
let savedNote = $state(entry.reviewerNote ?? "")
let status = $state<"idle" | "saving" | "saved" | "error">("idle")
let error = $state("")
let timer: ReturnType<typeof setTimeout> | null

const approvedSeconds = $derived(Math.max(0, Math.round((Number(approved) || 0) * 3600)))
const changed = $derived(approvedSeconds !== savedApproved || note !== savedNote)

onDestroy(() => {
	if (timer) clearTimeout(timer)
	if (changed) void save(true)
})

// hoursValue shows seconds as a compact hours input without trailing zeros.
function hoursValue(seconds: number): string {
	return String(Math.round((seconds / 3600) * 100) / 100)
}

// schedule saves once typing pauses, so a deflation does not need a button.
function schedule(): void {
	if (!editable) return
	if (timer) clearTimeout(timer)
	status = "saving"
	timer = setTimeout(() => {
		timer = undefined
		void save()
	}, 600)
}

// flush saves immediately when the field loses focus.
function flush(): void {
	if (!editable) return
	if (timer) {
		clearTimeout(timer)
		timer = undefined
	}
	void save()
}

// save writes the entry's approved time and note, keeping the rail total in step.
async function save(keepalive = false): Promise<void> {
	if (timer) {
		clearTimeout(timer)
		timer = undefined
	}
	if (!changed) {
		status = "idle"
		return
	}

	status = "saving"
	error = ""

	try {
		const result = await reviewJournal(
			shipId,
			entry.id,
			{ approvedSeconds, reviewerNote: note, version },
			keepalive,
		)
		savedApproved = approvedSeconds
		savedNote = note
		onSaved(result.hoursApprovedSeconds)
		status = "saved"
	} catch (err) {
		status = "error"
		error = err instanceof ApiError ? err.message : "Could not save this entry."
	}
}
</script>

{#if editable}
	<div class="journal-time">
		<label class="field">
			<span>Count as (hours)</span>
			<span class="journal-time-row">
				<input
					aria-label={`Approved hours for ${entry.title ?? "this entry"}`}
					class="ui-input"
					min="0"
					onblur={flush}
					oninput={schedule}
					step="0.25"
					type="number"
					bind:value={approved}
				>
				<span class="hint">{formatHours(entry.seconds)} claimed</span>
			</span>
		</label>
		<label class="field">
			<span>Deflation note <span class="hint">staff only</span></span>
			<input
				aria-label={`Deflation note for ${entry.title ?? "this entry"}`}
				class="ui-input"
				onblur={flush}
				oninput={schedule}
				placeholder="Why this entry counts for less"
				bind:value={note}
			>
		</label>
		<p aria-live="polite" class="journal-time-status">
			{#if status === "saving"}
				Saving…
			{:else if status === "saved"}
				Saved
			{:else if status === "error"}
				<span class="error">{error}</span>
			{/if}
		</p>
	</div>
{:else if typeof entry.approvedSeconds === "number"}
	<p class="journal-counted">
		Counted {formatHours(entry.approvedSeconds)} of {formatHours(entry.seconds)}
		{#if entry.reviewerNote}
			· {entry.reviewerNote}
		{/if}
	</p>
{/if}

<style>
.journal-time {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: end;
	padding-top: var(--space-2);
	border-top: 1px solid var(--border);
}

.journal-time .field {
	flex: 1 1 14rem;
}

.journal-time-row {
	display: flex;
	gap: var(--space-2);
	align-items: center;
}

.journal-time-row .ui-input {
	width: 7rem;
}

.journal-time .hint {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.journal-time-status {
	flex-basis: 100%;
	min-height: 1.2em;
	font-size: var(--text-xs);
	color: var(--text-3);
}

.journal-time-status .error {
	color: var(--accent-red);
}

.journal-counted {
	padding-top: var(--space-2);
	font-size: var(--text-sm);
	color: var(--text-3);
}
</style>
