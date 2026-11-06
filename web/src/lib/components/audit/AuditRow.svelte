<script lang="ts">
import type { AuditEntry } from "../../api"
import {
	auditChanges,
	auditDetailRows,
	auditLook,
	auditReason,
	auditSummary,
	auditTargetHref,
	auditTargetLabel,
} from "../../audit"
import { formatDateTime, timeAgo } from "../../format"
import { reviewerHref } from "../../reviewer"
import Avatar from "../Avatar.svelte"
import Icon from "../Icon.svelte"

let { entry }: { entry: AuditEntry } = $props()

let expanded = $state(false)

const look = $derived(auditLook(entry))
const summary = $derived(auditSummary(entry))
const changes = $derived(auditChanges(entry))
const reason = $derived(auditReason(entry))
const details = $derived(auditDetailRows(entry))
const targetLabel = $derived(auditTargetLabel(entry))
const targetHref = $derived(auditTargetHref(entry))
const actor = $derived(entry.actorName || entry.actorEmail || "System")
const actorHref = $derived(reviewerHref(entry.actorEmail))
const detailID = $derived(`audit-${entry.id}-detail`)
</script>

<li class="entry" data-tone={look.tone}>
	<span aria-hidden="true" class="bubble">
		<Icon name={look.icon} size={17} />
	</span>

	<div class="body">
		<div class="headline">
			<Avatar email={entry.actorEmail} name={entry.actorName} size="xs" url={entry.actorAvatarUrl} />
			<p class="text">
				{#if actorHref}
					<a class="actor" data-sveltekit-reload href={actorHref} title={entry.actorEmail ?? ""}>{actor}</a>
				{:else}
					<b>{actor}</b>
				{/if}
				<span class="what">{summary}</span>
			</p>
		</div>

		<div class="sub">
			<span class="kind">{look.label}</span>
			{#if targetLabel}
				<span aria-hidden="true" class="sep">·</span>
				{#if targetHref && entry.targetType === "user"}
					<a class="target" data-sveltekit-reload href={targetHref} title="Open {targetLabel}">
						{targetLabel}
						<Icon name="arrowR" size={12} />
					</a>
				{:else if targetHref}
					<a class="target" href={targetHref} title="Open {targetLabel}">
						{targetLabel}
						<Icon name="arrowR" size={12} />
					</a>
				{:else}
					<span class="target">{targetLabel}</span>
				{/if}
			{/if}
		</div>

		{#if expanded}
			<div class="detail" id={detailID}>
				<dl class="kv">
					{#each details as detail (detail.label)}
						<div>
							<dt>{detail.label}</dt>
							<dd class:mono={detail.mono}>{detail.value}</dd>
						</div>
					{/each}
				</dl>

				{#if reason}
					<div class="reason">
						<span class="section-label">Reason</span>
						<p>{reason}</p>
					</div>
				{/if}

				{#if changes.length > 0}
					<div class="changes">
						<span class="section-label">Changes</span>
						{#each changes as change (change.key)}
							<div class="change">
								<span class="change-label">{change.label}</span>
								<span class="change-values">
									<span class="from">{change.from}</span>
									<span aria-hidden="true" class="arrow">→</span>
									<span class="to">{change.to}</span>
								</span>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{/if}
	</div>

	<div class="side">
		<time datetime={entry.createdAt} title={formatDateTime(entry.createdAt)}>{timeAgo(entry.createdAt)}</time>
		<button
			aria-controls={expanded ? detailID : undefined}
			aria-expanded={expanded}
			aria-label={expanded ? "Hide event details" : "Show event details"}
			class="ui-icon-button toggle"
			onclick={(): void => {
				expanded = !expanded
			}}
			type="button"
		>
			<Icon name="chevD" size={16} />
		</button>
	</div>
</li>

<style>
.entry {
	--tone: var(--text-2);
	display: flex;
	gap: var(--space-3);
	align-items: flex-start;
	padding: var(--space-3) var(--space-4);
	border-bottom: 1px solid var(--border);
}

.entry:last-child {
	border-bottom: 0;
}

.entry[data-tone="approved"] {
	--tone: var(--status-approved);
}

.entry[data-tone="changes"] {
	--tone: var(--status-changes);
}

.entry[data-tone="rejected"] {
	--tone: var(--status-rejected);
}

.entry[data-tone="secondpass"] {
	--tone: var(--status-secondpass);
}

.entry[data-tone="ready"] {
	--tone: var(--accent-blue);
}

.entry[data-tone="pending"] {
	--tone: var(--status-pending);
}

.entry[data-tone="purple"] {
	--tone: var(--accent-purple);
}

.bubble {
	display: grid;
	flex: none;
	place-items: center;
	width: var(--control-md);
	height: var(--control-md);
	color: var(--tone);
	background: color-mix(in srgb, var(--tone) 15%, var(--surface));
	border-radius: var(--radius-md);
}

.body {
	display: flex;
	flex: 1;
	flex-direction: column;
	gap: var(--space-1);
	min-width: 0;
	padding-top: 0.1rem;
}

.headline {
	display: flex;
	gap: var(--space-2);
	align-items: center;
	min-width: 0;
}

.text {
	min-width: 0;
	margin: 0;
	font-size: var(--text-md);
	line-height: 1.45;
	overflow-wrap: anywhere;
}

.text b {
	font-weight: 700;
}

.actor {
	font-weight: 700;
	color: var(--text);
	text-decoration: none;
}

.actor:hover {
	text-decoration: underline;
}

.what {
	color: var(--text-2);
}

.sub {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1) var(--space-2);
	align-items: center;
	font-size: var(--text-xs);
	color: var(--text-3);
}

.kind {
	font-weight: 700;
	color: var(--tone);
}

.sep {
	color: var(--border-2);
}

.target {
	display: inline-flex;
	gap: 0.25rem;
	align-items: center;
	min-width: 0;
	color: var(--text-2);
	overflow-wrap: anywhere;
	text-decoration: none;
}

.target :global(svg) {
	flex: none;
}

a.target:hover {
	color: var(--text);
	text-decoration: underline;
}

.detail {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	padding: var(--space-3);
	margin-top: var(--space-2);
	background: var(--surface-2);
	border: 1px solid var(--border);
	border-left: 2px solid color-mix(in srgb, var(--tone) 45%, var(--border));
	border-radius: var(--radius-md);
}

.kv {
	display: grid;
	gap: var(--space-1);
	margin: 0;
}

.kv div {
	display: grid;
	grid-template-columns: 6.5rem minmax(0, 1fr);
	gap: var(--space-2);
	align-items: baseline;
}

.kv dt {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-3);
}

.kv dd {
	margin: 0;
	font-size: var(--text-xs);
	color: var(--text-2);
	overflow-wrap: anywhere;
}

.kv dd.mono {
	font-family: var(--font-mono);
	font-size: 0.7rem;
}

.reason,
.changes {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding-top: var(--space-3);
	border-top: 1px solid var(--border);
}

.section-label {
	font-size: var(--text-xs);
	font-weight: 800;
	color: var(--text-3);
	text-transform: uppercase;
	letter-spacing: 0.05em;
}

.reason p {
	font-size: var(--text-sm);
	line-height: 1.55;
	color: var(--text-2);
	overflow-wrap: anywhere;
	white-space: pre-wrap;
}

.change {
	display: grid;
	grid-template-columns: 7.5rem minmax(0, 1fr);
	gap: var(--space-2);
	align-items: baseline;
}

.change-label {
	font-size: var(--text-xs);
	font-weight: 600;
	color: var(--text-3);
}

.change-values {
	display: inline-flex;
	flex-wrap: wrap;
	gap: var(--space-1);
	align-items: baseline;
	min-width: 0;
	font-size: var(--text-xs);
	font-variant-numeric: tabular-nums;
}

.from {
	color: var(--text-3);
}

.arrow {
	color: var(--text-3);
}

.to {
	font-weight: 700;
	color: var(--text);
	overflow-wrap: anywhere;
}

.side {
	display: flex;
	flex: none;
	gap: var(--space-1);
	align-items: center;
	padding-top: 0.15rem;
}

time {
	font-size: var(--text-xs);
	color: var(--text-3);
	white-space: nowrap;
}

.toggle {
	flex: none;
	width: var(--control-sm);
	height: var(--control-sm);
}

.toggle :global(svg) {
	transition: transform 0.15s;
}

.toggle[aria-expanded="true"] :global(svg) {
	transform: rotate(180deg);
}

@media (max-width: 40rem) {
	.entry {
		gap: var(--space-2);
		padding: var(--space-3);
	}

	.bubble {
		width: var(--control-sm);
		height: var(--control-sm);
	}

	.kv div,
	.change {
		grid-template-columns: 5.5rem minmax(0, 1fr);
	}
}
</style>
