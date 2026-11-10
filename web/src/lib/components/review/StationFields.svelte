<script lang="ts">
import { type Bundle } from "../../api"
import { returnNote } from "../../findings"
import { formatHours } from "../../format"
import {
	type MainField,
	suggestDeflationReason,
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
const returnable = $derived(
	bundle.findings.filter(
		finding => finding.status === "open" && (finding.severity === "block" || finding.severity === "fixable"),
	),
)
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
				Built from {cuts} journal cut{cuts === 1 ? "" : "s"}; compare with an intermediate maker.
			{:else}
				Only needed when the approved time is below the captured time.
			{/if}
		</p>
		<textarea
			aria-label="Deflation reason"
			class="ui-input"
			maxlength="4000"
			oninput={(event): void => onChange("deflationReason", event.currentTarget.value)}
			placeholder="Why is this amount reasonable for this maker?"
			readonly={!editable}
			rows="3"
			value={deflationReason}
		></textarea>
		<div class="field-actions">
			<button
				class="ui-button ghost sm"
				onclick={(): void => onChange("deflationReason", suggestDeflationReason(bundle, capturedSeconds))}
				type="button"
			>
				Use journal cuts
			</button>
			<span class="muted"
				>{formatHours(capturedSeconds)}
				captured · {formatHours(bundle.submission.hoursApprovedSeconds ?? capturedSeconds)} approved</span
			>
		</div>

		<div class="fields-head">
			<h2>Notes</h2>
		</div>
		<p class="hint">The maker note is public. The internal note is for the team and the audit record.</p>
		<label class="field">
			<span class="field-head"><span>Note to maker</span><span class="muted">{note.length} / 4000</span></span>
			<textarea
				aria-label="Note to maker"
				class="ui-input"
				maxlength="4000"
				oninput={(event): void => onChange("note", event.currentTarget.value)}
				placeholder="What should they change, or why is this good?"
				readonly={!editable}
				rows="4"
				value={note}
			></textarea>
		</label>
		<div class="field-actions">
			<button
				class="ui-button ghost sm"
				disabled={returnable.length === 0}
				onclick={(): void => onChange("note", returnNote(returnable))}
				type="button"
			>
				Draft note from {returnable.length} finding{returnable.length === 1 ? "" : "s"}
			</button>
		</div>
		<label class="field">
			<span class="field-head"
				><span>Internal note <span class="internal-tag">Internal</span></span
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
</style>
