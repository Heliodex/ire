<script lang="ts">
import { type Bundle } from "../../api"
import { formatHours } from "../../format"
import {
	type MainField,
	suggestInternalNote,
	suggestTechnicalFeatures,
} from "../../station-fields"

interface Props {
	bundle: Bundle
	deflationReason: string
	editable: boolean
	internalNote: string
	note: string
	onChange(field: MainField, value: string): void
	stationKey: string
	technicalFeatures: string
}

let { bundle, deflationReason, editable, internalNote, note, onChange, stationKey, technicalFeatures }: Props = $props()

const capturedSeconds = $derived(
	Math.max(
		bundle.submission.hoursClaimedSeconds,
		bundle.submission.hoursJournalSeconds + bundle.submission.hoursHackatimeSeconds,
	),
)
const cuts = $derived(
	bundle.journals.filter(entry => entry.approvedSeconds !== undefined && entry.approvedSeconds < entry.seconds)
		.length,
)

// focusHref returns the URL for the same ship at another station.
function focusHref(key: string): string {
	const stage = bundle.stations.find(item => item.key === key)?.stage ?? 1
	return `/station/${encodeURIComponent(key)}?id=${encodeURIComponent(bundle.submission.id)}&stage=${stage}`
}

// stationLabel names another station, falling back when the workflow renames it.
function stationLabel(key: string, fallback: string): string {
	return bundle.stations.find(item => item.key === key)?.label ?? fallback
}
</script>

{#if stationKey === "narrative"}
	<section class="ui-card padded fields-card">
		<div class="fields-head">
			<h2>Specific technical features</h2>
			<span class="internal-tag">Internal</span>
		</div>
		<p class="hint">
			The work that accounts for these hours. Suggested from the description, BOM and file kinds; edit it.
		</p>
		<textarea
			aria-label="Specific technical features"
			class="ui-input"
			maxlength="4000"
			oninput={(event): void => onChange("technicalFeatures", event.currentTarget.value)}
			placeholder="Firmware, board, enclosure, and what each one does."
			readonly={!editable}
			rows="5"
			value={technicalFeatures}
		></textarea>
		<div class="field-actions">
			<button
				class="ui-button ghost sm"
				onclick={(): void => onChange("technicalFeatures", suggestTechnicalFeatures(bundle))}
				type="button"
			>
				Suggest from the repo
			</button>
			<span class="muted">{bundle.bom.length} BOM lines · {bundle.evidence?.files.length ?? 0} files</span>
		</div>
	</section>
{:else if stationKey === "synthesis" || stationKey === "verify"}
	<section class="ui-card padded fields-card">
		<div class="fields-head">
			<h2>Specific technical features</h2>
			<span class="internal-tag">Internal</span>
		</div>
		<p class="hint">The work that accounts for these hours. Edit the suggestion before deciding.</p>
		<textarea
			aria-label="Specific technical features"
			class="ui-input"
			maxlength="4000"
			oninput={(event): void => onChange("technicalFeatures", event.currentTarget.value)}
			placeholder="Firmware, board, enclosure, and what each one does."
			readonly={!editable}
			rows="4"
			value={technicalFeatures}
		></textarea>
		<div class="field-actions">
			<button
				class="ui-button ghost sm"
				onclick={(): void => onChange("technicalFeatures", suggestTechnicalFeatures(bundle))}
				type="button"
			>
				Suggest from the repo
			</button>
			<span class="muted">{bundle.bom.length} BOM lines · {bundle.evidence?.files.length ?? 0} files</span>
		</div>

		<div class="fields-head">
			<h2>Deflation reason</h2>
			<span class="internal-tag">Internal</span>
		</div>
		<p class="hint">
			{#if cuts > 0}
				Built from {cuts} journal cut{cuts === 1 ? "" : "s"} on the time station; edit the cuts there.
			{:else}
				Only needed when the approved time is below the captured time; cut journal entries on the time
				station.
			{/if}
		</p>
		{#if deflationReason !== ""}
			<p class="readonly-text">{deflationReason}</p>
		{:else}
			<p class="readonly-empty">Cut journal entries on the time station to explain a lower approval.</p>
		{/if}
		<div class="field-actions">
			<a class="ui-button ghost sm" href={focusHref("time")}>{stationLabel("time", "Journals and time")} ↗</a>
			<a class="ui-button ghost sm" href={focusHref("narrative")}>
				{stationLabel("narrative", "README and claims")} ↗
			</a>
			<span class="muted"
				>{formatHours(capturedSeconds)}
				captured · {formatHours(bundle.submission.hoursApprovedSeconds ?? capturedSeconds)} approved</span
			>
		</div>

		<div class="fields-head">
			<h2>Notes</h2>
		</div>
		<p class="hint">
			The maker note is public and built from the findings chosen for return. The internal note is for the
			team and the audit record.
		</p>
		<span class="field-head"><span>Note to maker</span></span>
		{#if note !== ""}
			<p class="readonly-text">{note}</p>
		{:else}
			<p class="readonly-empty">No findings chosen for return – the maker note is empty.</p>
		{/if}
		<label class="field">
			<span class="field-head"
				><span>Internal note<span class="internal-tag">Internal</span></span
				><span class="muted">{internalNote.length} / 4000</span></span
			>
			<textarea
				aria-label="Internal note"
				class="ui-input"
				maxlength="4000"
				oninput={(event): void => onChange("internalNote", event.currentTarget.value)}
				placeholder="Only the team sees this."
				readonly={!editable}
				rows="3"
				value={internalNote}
			></textarea>
		</label>
		<div class="field-actions">
			<button
				class="ui-button ghost sm"
				onclick={(): void => onChange("internalNote", suggestInternalNote(bundle))}
				type="button"
			>
				Use station notes
			</button>
		</div>
	</section>
{/if}

<style>
.fields-card {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.fields-head {
	display: flex;
	gap: var(--space-2);
	align-items: center;
	justify-content: space-between;
}

.fields-head h2 {
	font-size: var(--text-md);
	font-weight: 700;
}

.hint,
.muted {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.field {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
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

.field-actions {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
}

.readonly-text {
	padding: var(--space-2) var(--space-3);
	font-size: var(--text-sm);
	color: var(--text-2);
	white-space: pre-wrap;
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.readonly-empty {
	font-size: var(--text-xs);
	color: var(--text-3);
}
</style>
