<script lang="ts">
import { onMount } from "svelte"
import { goto } from "$app/navigation"
import { fetchMe, fetchOutbox, fetchReceived } from '../../lib/api';
import type { FakeDelivery, OutboxRow } from '../../lib/api';
import LoadingState from "../../lib/components/LoadingState.svelte"
import { formatDateTime, statusTone } from "../../lib/format"
import { session } from "../../lib/session.svelte.js"

let outbox = $state<OutboxRow[]>([])
let received = $state<FakeDelivery[]>([])
let loading = $state(true)
let error = $state("")
let open = $state("")

onMount((): void => {
	void boot()
})

async function boot(): Promise<void> {
	try {
		session.me ??= await fetchMe()
	} catch {
		void goto("/")
		return
	}

	try {
		const [rows, deliveries] = await Promise.all([fetchOutbox(), fetchReceived()])
		outbox = rows
		received = deliveries
	} catch (err) {
		error = err instanceof Error ? err.message : "Could not load deliveries."
	} finally {
		loading = false
	}
}

function toggle(id: string): void {
	open = open === id ? "" : id
}

function eventOf(body: string): string {
	try {
		const parsed = JSON.parse(body) as { event?: string }
		return parsed.event ?? "unknown"
	} catch {
		return "unknown"
	}
}
</script>

<svelte:head>
	<title>Deliveries · Ire</title>
</svelte:head>

<section class="head">
	<div>
		<h1>Deliveries</h1>
		<p>
			Every final decision is written to the outbox before it is sent. The receiver verifies the same
			<code class="ui-mono">timestamp.delivery_id.body</code>
			signature Half Life's Ari handler checks.
		</p>
	</div>
	<div class="stats ui-card">
		<div><strong>{outbox.filter(row => row.status === "delivered").length}</strong><span>Delivered</span></div>
		<div><strong>{outbox.filter(row => row.status !== "delivered").length}</strong><span>Pending</span></div>
		<div><strong>{received.length}</strong><span>Verified</span></div>
	</div>
</section>

{#if error}
	<div class="ui-notice" data-tone="bad" role="alert">{error}</div>
{/if}

{#if loading}
	<div class="grid">
		<section class="ui-card">
			<div class="card-head">
				<h2>Outbox</h2>
			</div>
			<LoadingState message="Loading outbox…" />
		</section>
		<section class="ui-card">
			<div class="card-head">
				<h2>Receiver</h2>
			</div>
			<LoadingState message="Loading receiver…" />
		</section>
	</div>
{:else}
	<div class="grid">
		<section class="ui-card">
			<div class="card-head">
				<h2>Outbox</h2>
				<span>{outbox.length} {outbox.length === 1 ? "row" : "rows"}</span>
			</div>
			{#if outbox.length === 0}
				<div class="ui-empty">
					<strong>Nothing sent yet</strong>
					<span>A final decision will write the first delivery here.</span>
				</div>
			{:else}
				{#each outbox as row (row.id)}
					<article class="row">
						<button
							aria-expanded={open === row.id}
							class="row-head"
							onclick={(): void => toggle(row.id)}
							type="button"
						>
							<div class="row-title">
								<strong>{row.event}</strong>
								<span class="ui-badge" data-tone={statusTone(row.status)}>{row.status}</span>
								{#if row.attempts > 0}
									<span class="muted">attempt {row.attempts}</span>
								{/if}
							</div>
							<span class="row-meta">
								<small>{formatDateTime(row.createdAt)}</small>
								<svg
									aria-hidden="true"
									class="chevron"
									fill="none"
									height="16"
									viewBox="0 0 24 24"
									width="16"
									class:open={open === row.id}
								>
									<path
										d="m6 9 6 6 6-6"
										stroke="currentColor"
										stroke-linecap="round"
										stroke-linejoin="round"
										stroke-width="2"
									/>
								</svg>
							</span>
						</button>
						{#if open === row.id && row.payload}
							<pre>{row.payload}</pre>
						{/if}
					</article>
				{/each}
			{/if}
		</section>

		<section class="ui-card">
			<div class="card-head">
				<h2>Receiver</h2>
				<span>{received.length} verified</span>
			</div>
			{#if received.length === 0}
				<div class="ui-empty">
					<strong>No deliveries received</strong>
					<span>The local receiver stores the verified body and signature.</span>
				</div>
			{:else}
				{#each received as delivery (delivery.id)}
					<article class="row">
						<button
							aria-expanded={open === delivery.id}
							class="row-head"
							onclick={(): void => toggle(delivery.id)}
							type="button"
						>
							<div class="row-title">
								<strong>{eventOf(delivery.body)}</strong>
								<span class="ui-badge" data-tone={delivery.verified === 1 ? "approved" : "rejected"}>
									{delivery.verified === 1 ? "verified" : "rejected"}
								</span>
							</div>
							<span class="row-meta">
								<small>{formatDateTime(delivery.receivedAt)}</small>
								<svg
									aria-hidden="true"
									class="chevron"
									fill="none"
									height="16"
									viewBox="0 0 24 24"
									width="16"
									class:open={open === delivery.id}
								>
									<path
										d="m6 9 6 6 6-6"
										stroke="currentColor"
										stroke-linecap="round"
										stroke-linejoin="round"
										stroke-width="2"
									/>
								</svg>
							</span>
						</button>
						{#if open === delivery.id}
							<dl>
								<div>
									<dt>Delivery</dt>
									<dd class="ui-mono">{delivery.deliveryId}</dd>
								</div>
								<div>
									<dt>Timestamp</dt>
									<dd class="ui-mono">{delivery.timestamp}</dd>
								</div>
								<div>
									<dt>Signature</dt>
									<dd class="ui-mono">{delivery.signature}</dd>
								</div>
							</dl>
							<pre>{delivery.body}</pre>
						{/if}
					</article>
				{/each}
			{/if}
		</section>
	</div>
{/if}

<style>
.head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-4);
	align-items: flex-end;
	justify-content: space-between;
	padding-bottom: var(--space-5);
}

h1 {
	font-size: var(--text-2xl);
}

.head p {
	max-width: 42.5rem;
	padding-top: var(--space-1);
	font-size: var(--text-sm);
	color: var(--text-2);
}

.stats {
	display: flex;
	overflow: hidden;
}

:where(.stats) div {
	min-width: 5.75rem;
	padding: var(--space-2) var(--space-3);
	border-left: 1px solid var(--border);
}

:where(.stats) div:first-child {
	border-left: 0;
}

:where(.stats) strong {
	display: block;
	font-size: var(--text-lg);
	font-weight: 800;
	font-variant-numeric: tabular-nums;
}

:where(.stats) span {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.grid {
	display: grid;
	grid-template-columns: 1fr 1fr;
	gap: var(--space-4);
	align-items: start;
}

.grid > section {
	min-width: 0;
}

.card-head {
	display: flex;
	gap: var(--space-2);
	align-items: baseline;
	justify-content: space-between;
	padding: var(--space-3) var(--space-4);
	border-bottom: 1px solid var(--border);
}

.card-head h2 {
	font-size: var(--text-md);
	font-weight: 700;
}

.card-head span {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.row {
	border-bottom: 1px solid var(--border);
}

.row:last-child {
	border-bottom: 0;
}

.row-head {
	display: flex;
	gap: var(--space-3);
	align-items: center;
	justify-content: space-between;
	width: 100%;
	padding: var(--space-3) var(--space-4);
	text-align: left;
	background: transparent;
	border: 0;
}

.row-head:hover {
	background: var(--surface-2);
}

.row-title {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
}

.row-title strong {
	font-size: var(--text-sm);
}

.row-head small {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.row-meta {
	display: flex;
	flex: none;
	gap: var(--space-2);
	align-items: center;
	color: var(--text-3);
}

.chevron {
	flex: none;
	color: var(--text-3);
	transition: transform 0.15s;
}

.chevron.open {
	transform: rotate(180deg);
}

.muted {
	font-size: var(--text-xs);
	color: var(--text-3);
}

pre {
	max-height: 21.25rem;
	padding: var(--space-3);
	margin: 0 var(--space-3) var(--space-3);
	overflow: auto;
	font-size: var(--text-xs);
	color: var(--text-2);
	overflow-wrap: anywhere;
	white-space: pre-wrap;
	background: var(--surface-2);
	border: 1px solid var(--border);
	border-radius: var(--radius-sm);
}

dl {
	display: grid;
	gap: var(--space-1);
	padding: 0 var(--space-4) var(--space-3);
	font-size: var(--text-xs);
}

dl div {
	display: grid;
	grid-template-columns: 5rem 1fr;
	gap: var(--space-2);
}

dt {
	color: var(--text-3);
}

dd {
	margin: 0;
	overflow-wrap: anywhere;
}

@media (max-width: 56.25rem) {
	.grid {
		grid-template-columns: 1fr;
	}
}

@media (max-width: 47.5rem) {
	.stats {
		width: 100%;
	}

	.stats div {
		flex: 1;
		min-width: 0;
	}
}
</style>
