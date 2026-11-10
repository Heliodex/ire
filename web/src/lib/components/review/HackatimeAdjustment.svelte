<script lang="ts">
import { reviewHackatime, type TimeAdjustment } from "../../api"
import { ApiError } from "../../api-error"
import { reasonLabel, reasonLabels } from "../../deflation"
import { formatHours } from "../../format"

interface Props {
	adjustment: TimeAdjustment | undefined
	claimedSeconds: number
	editable: boolean
	onSaved(totalSeconds: number, adjustment: TimeAdjustment | undefined): void
	shipId: string
	version: number
}

let { adjustment, claimedSeconds, editable, shipId, version, onSaved }: Props = $props()

// hoursValue shows seconds as a compact hours input without trailing zeros.
function hoursValue(seconds: number): string {
	return String(Math.round((seconds / 3600) * 100) / 100)
}

const approvedDefault = $derived(claimedSeconds - (adjustment?.seconds ?? 0))

// svelte-ignore state_referenced_locally
let approved = $state(hoursValue(approvedDefault))
// svelte-ignore state_referenced_locally
let reason = $state(adjustment?.reason ?? "")
// svelte-ignore state_referenced_locally
let note = $state(adjustment?.note ?? "")
let status = $state<"idle" | "saving" | "saved" | "error">("idle")
let error = $state("")

const approvedSeconds = $derived(Math.max(0, Math.round((Number(approved) || 0) * 3600)))

// save writes the Hackatime approved time, reason and note, keeping the rail total in step.
async function save(): Promise<void> {
	if (status === "saving") return
	if (approvedSeconds < claimedSeconds && reason.trim() === "") {
		status = "error"
		error = "Choose why this Hackatime counts for less."
		return
	}

	status = "saving"
	error = ""

	try {
		const result = await reviewHackatime(shipId, {
			approvedSeconds,
			deflationReason: reason,
			reviewerNote: note,
			version,
		})
		onSaved(result.hoursApprovedSeconds, result.adjustment)
		status = "saved"
	} catch (err) {
		status = "error"
		error = err instanceof ApiError ? err.message : "Could not save the Hackatime time."
	}
}
</script>

<div class="hackatime-cut">
	<span class="hackatime-summary">
		Hackatime counts as <strong>{formatHours(approvedSeconds)}</strong> of {formatHours(claimedSeconds)}
		{#if adjustment}
			· {reasonLabel(adjustment.reason)}
		{/if}
	</span>
	{#if editable}
		<label class="field">
			<span class="field-head">Counts as (hours)</span>
			<input
				aria-label="Approved Hackatime hours"
				class="ui-input"
				min="0"
				onchange={(): Promise<void> => save()}
				step="0.25"
				type="number"
				bind:value={approved}
			>
		</label>
		{#if approvedSeconds < claimedSeconds || reason !== ""}
			<label class="field">
				<span class="field-head"><span>Why less?</span><span class="hint">staff only</span></span>
				<select
					aria-label="Deflation reason for Hackatime"
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
					aria-label="Deflation note for Hackatime"
					class="ui-input"
					onchange={(): Promise<void> => save()}
					placeholder="Why this Hackatime counts for less"
					bind:value={note}
				>
			</label>
		{/if}
		<p aria-live="polite" class="hackatime-status">
			{#if status === "saving"}
				Saving…
			{:else if status === "saved"}
				Saved
			{:else if status === "error"}
				<span class="error">{error}</span>
			{/if}
		</p>
	{/if}
</div>

<style>
.hackatime-cut {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	padding-top: var(--space-2);
}

.hackatime-summary {
	font-size: var(--text-sm);
	color: var(--text-2);
}

.hackatime-summary strong {
	color: var(--text);
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

.hackatime-status {
	min-height: 1.2em;
	font-size: var(--text-xs);
	color: var(--text-3);
}

.hackatime-status .error {
	color: var(--accent-red);
}
</style>
