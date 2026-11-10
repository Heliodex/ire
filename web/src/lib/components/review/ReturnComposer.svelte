<script lang="ts">
import { type Bundle } from "../../api"
import { isReturnable, severityTone } from "../../findings"

type Finding = Bundle["findings"][number]

interface Props {
	findings: Finding[]
	selected?: string[]
}

let { findings, selected = $bindable([]) }: Props = $props()

const returnable = $derived(findings.filter(finding => isReturnable(finding)))
const selectedCount = $derived(returnable.filter(finding => selected.includes(finding.id)).length)
const missingCount = $derived(returnable.length - selectedCount)
const missingMessage = $derived(
	`${missingCount} open finding${missingCount === 1 ? "" : "s"} ${missingCount === 1 ? "is" : "are"} not in this return.`,
)
const idsKey = $derived(returnable.map(finding => finding.id).join("|"))

// The selection resets to every returnable finding whenever the set of findings changes.
$effect(() => {
	void idsKey
	selected = returnable.map(finding => finding.id)
})

function toggle(findingId: string, checked: boolean): void {
	selected = checked ? [...selected, findingId] : selected.filter(id => id !== findingId)
}

function selectAll(): void {
	selected = returnable.map(finding => finding.id)
}
</script>

<div class="composer">
	<div class="composer-head">
		<span class="field-label">Findings to return</span>
		{#if returnable.length > 0}
			<span class="composer-count">{selectedCount} of {returnable.length} selected</span>
		{/if}
	</div>

	{#if returnable.length === 0}
		<p class="hint">No open blocking or fixable findings.</p>
	{:else}
		<ul class="return-list">
			{#each returnable as finding (finding.id)}
				<li>
					<label class="return-item">
						<input
							checked={selected.includes(finding.id)}
							onchange={(event): void => toggle(finding.id, event.currentTarget.checked)}
							type="checkbox"
						>
						<span class="return-text">
							<span class="return-summary">{finding.summary}</span>
							{#if finding.makerAction}
								<small>{finding.makerAction}</small>
							{/if}
						</span>
						<span class="ui-badge" data-tone={severityTone(finding.severity)}>{finding.severity}</span>
					</label>
				</li>
			{/each}
		</ul>

		{#if missingCount > 0}
			<p class="ui-notice" data-tone="warn">{missingMessage}</p>
		{/if}

		{#if missingCount > 0}
			<div class="composer-actions">
				<button class="ui-button ghost sm" onclick={selectAll} type="button">Select all</button>
			</div>
		{/if}
	{/if}
</div>

<style>
.composer {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: var(--space-2) 0;
}

.composer-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: baseline;
	justify-content: space-between;
}

.field-label {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-2);
}

.composer-count {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.hint {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.return-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	padding: 0;
	margin: 0;
	list-style: none;
}

.return-item {
	display: flex;
	gap: var(--space-2);
	align-items: flex-start;
	padding: var(--space-2);
	cursor: pointer;
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.return-item input {
	flex: none;
	margin-top: 2px;
}

.return-text {
	display: flex;
	flex-direction: column;
	gap: 1px;
	min-width: 0;
	font-size: var(--text-sm);
	color: var(--text-2);
}

.return-summary {
	font-weight: 600;
	color: var(--text);
	overflow-wrap: anywhere;
}

.return-text small {
	font-size: var(--text-xs);
	color: var(--text-3);
	overflow-wrap: anywhere;
}

.return-item .ui-badge {
	flex: none;
	margin-left: auto;
}

.composer-actions {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1);
	align-items: center;
}

.composer .ui-notice {
	font-size: var(--text-xs);
}
</style>
