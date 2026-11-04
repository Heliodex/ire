<script lang="ts">
import type { Check } from "../../api"
import { groupChecks, isBlocking, isWarning } from "../../checks"
import { statusTone } from "../../format"

let {
	checks,
	acknowledgements,
	onToggle,
	onAcknowledgeAll,
}: {
	checks: Check[]
	acknowledgements: string[]
	onToggle(key: string): void
	onAcknowledgeAll(): void
} = $props()

const warnings = $derived(checks.filter(isWarning))
const blocking = $derived(checks.filter(isBlocking))
const missingAcks = $derived(warnings.filter(check => !acknowledgements.includes(check.key)))
const groups = $derived(groupChecks(checks))
</script>

{#if blocking.length > 0}
	<div class="ui-notice" data-tone="bad" role="alert">
		<strong>Approval is blocked</strong>
		{blocking[0].summary}. Ask the maker for changes or reject with a note.
	</div>
{/if}

{#if warnings.length > 0}
	<div class="ui-notice warn-box" data-tone="warn">
		<div>
			<strong>{warnings.length} warning{warnings.length === 1 ? "" : "s"}</strong>
			Acknowledge each one before approving. Warnings are held for the second pass.
		</div>
		{#if missingAcks.length > 0}
			<button class="ui-button sm" onclick={onAcknowledgeAll} type="button">Acknowledge all</button>
		{/if}
	</div>
{/if}

<div class="ui-card checks">
	{#each groups as group (group.group)}
		<div class="group">
			<h2>{group.group}</h2>
			{#each group.items as check (check.key)}
				<div class="check">
					<span class="ui-badge check-status" data-tone={statusTone(check.status)}>{check.status}</span>
					<span class="check-label">{check.label}</span>
					<code class="check-key ui-mono" title={check.key}>{check.key}</code>
					{#if isWarning(check)}
						<label class="ack" title="Acknowledge this warning">
							<input
								aria-label={`Acknowledge ${check.label}`}
								checked={acknowledgements.includes(check.key)}
								onchange={(): void => onToggle(check.key)}
								type="checkbox"
							>
							<span>Ack</span>
						</label>
					{:else}
						<span class="ack-empty"></span>
					{/if}
					<span class="check-detail">
						<span class="check-summary">{check.summary}</span>
						{#if check.detail}
							<span class="check-note">{check.detail}</span>
						{/if}
					</span>
				</div>
			{/each}
		</div>
	{/each}
</div>

<style>
.warn-box {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-3);
	align-items: center;
	justify-content: space-between;
}

.checks {
	overflow: hidden;
}

.group + .group {
	border-top: 1px solid var(--border);
}

.group h2 {
	padding: var(--space-2) var(--space-4);
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-3);
	background: var(--surface-2);
}

.check {
	display: grid;
	grid-template-columns: 58px 190px minmax(0, 1fr) 64px;
	row-gap: var(--space-1);
	column-gap: var(--space-3);
	align-items: start;
	padding: var(--space-2) var(--space-4);
	border-top: 1px solid var(--border);
}

.group h2 + .check {
	border-top: 0;
}

.check-status {
	justify-self: start;
}

.check-label {
	font-size: var(--text-sm);
	font-weight: 700;
}

.check-key {
	padding-top: 2px;
	overflow: hidden;
	text-overflow: ellipsis;
	font-size: var(--text-xs);
	color: var(--text-3);
	white-space: nowrap;
}

.check-detail {
	display: flex;
	flex-direction: column;
	grid-column: 1 / -1;
	gap: 1px;
}

.check-summary {
	font-size: var(--text-sm);
	color: var(--text-2);
}

.check-note {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.ack {
	display: flex;
	gap: var(--space-1);
	align-items: center;
	justify-self: end;
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--status-pending);
	cursor: pointer;
}

.ack input {
	accent-color: var(--primary);
}

@media (max-width: 760px) {
	.check {
		grid-template-areas:
			"status label key"
			"detail detail ack";
		grid-template-columns: 54px minmax(0, 1fr) minmax(90px, 150px);
		row-gap: var(--space-1);
	}

	.check-status {
		grid-area: status;
	}

	.check-label {
		grid-area: label;
	}

	.check-key {
		grid-area: key;
	}

	.check-detail {
		grid-area: detail;
	}

	.ack,
	.ack-empty {
		grid-area: ack;
		justify-self: end;
	}
}

@media (max-width: 500px) {
	.check {
		grid-template-areas:
			"status label ack"
			"key key key"
			"detail detail detail";
		grid-template-columns: 54px minmax(0, 1fr) 56px;
	}
}

@media (max-width: 640px) {
	.check,
	.group h2 {
		padding-inline: var(--space-3);
	}
}
</style>
