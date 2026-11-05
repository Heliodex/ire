<script lang="ts">
import { SvelteSet } from "svelte/reactivity"
import type { Check } from "../../api"
import { checkGroupLabel, groupChecks, isBlocking, isWarning } from "../../checks"
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
const collapsed = new SvelteSet<string>()

function toggleGroup(group: string): void {
	if (collapsed.has(group)) collapsed.delete(group)
	else collapsed.add(group)
}
</script>

<div class="checks-panel">
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
		{#if checks.length === 0}
			<div class="ui-empty">
				<strong>No checks yet</strong>
				<span>Repository and evidence checks will appear here when they run.</span>
			</div>
		{:else}
			{#each groups as group (group.group)}
				<div class="group">
					<h2 class="group-heading">
						<button
							aria-expanded={!collapsed.has(group.group)}
							class="group-toggle"
							onclick={(): void => toggleGroup(group.group)}
							type="button"
						>
							<span class="group-name">{checkGroupLabel(group.group)}</span>
							<span class="group-count"
								>{group.items.length} {group.items.length === 1 ? "check" : "checks"}</span
							>
							<svg
								aria-hidden="true"
								class="chevron"
								fill="none"
								height="14"
								viewBox="0 0 24 24"
								width="14"
								class:collapsed={collapsed.has(group.group)}
							>
								<path
									d="m6 9 6 6 6-6"
									stroke="currentColor"
									stroke-linecap="round"
									stroke-linejoin="round"
									stroke-width="2"
								/>
							</svg>
						</button>
					</h2>
					{#if !collapsed.has(group.group)}
						{#each group.items as check (check.key)}
							<div class="check">
								<span class="ui-badge check-status" data-tone={statusTone(check.status)}
									>{check.status}</span
								>
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
					{/if}
				</div>
			{/each}
		{/if}
	</div>
</div>

<style>
.checks-panel {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
}

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

.group-heading {
	margin: 0;
}

.group-toggle {
	display: flex;
	gap: var(--space-2);
	align-items: center;
	width: 100%;
	padding: var(--space-2) var(--space-4);
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-2);
	text-align: left;
	background: var(--surface-2);
	border: 0;
}

.group-toggle:hover {
	color: var(--text);
	background: var(--surface-3);
}

.group-name {
	flex: 1;
	text-transform: uppercase;
	letter-spacing: 0.04em;
}

.group-count {
	font-weight: 600;
	color: var(--text-3);
}

.chevron {
	flex: none;
	color: var(--text-3);
	transition: transform 0.15s;
}

.chevron.collapsed {
	transform: rotate(-90deg);
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
	padding: 2px 8px 2px 5px;
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--status-pending);
	cursor: pointer;
	background: color-mix(in srgb, var(--status-pending) 10%, var(--surface));
	border: 1px solid color-mix(in srgb, var(--status-pending) 35%, transparent);
	border-radius: var(--radius-full);
}

.ack:has(input:checked) {
	color: var(--status-approved);
	background: color-mix(in srgb, var(--status-approved) 12%, var(--surface));
	border-color: color-mix(in srgb, var(--status-approved) 35%, transparent);
}

.ack input {
	margin: 0;
	accent-color: var(--primary);
}

@media (max-width: 760px) {
	.check {
		grid-template-areas:
			"status label key ack"
			"detail detail detail detail";
		grid-template-columns: 54px minmax(0, 1fr) minmax(90px, 150px) 56px;
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
	.group-toggle {
		padding-inline: var(--space-3);
	}
}
</style>
