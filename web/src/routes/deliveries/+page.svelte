<script lang="ts">
import { onMount } from "svelte"
import { goto } from "$app/navigation"
import { type FakeDelivery, fetchMe, fetchOutbox, fetchReceived, type OutboxRow } from "../../lib/api"
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
		void goto("/login")
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
	<title>Deliveries · Half Life Review</title>
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
	<div class="counts">
		<span><strong>{outbox.filter(row => row.status === "delivered").length}</strong> delivered</span>
		<span><strong>{outbox.filter(row => row.status !== "delivered").length}</strong> pending</span>
	</div>
</section>

{#if error}
	<div class="ui-notice" data-tone="bad" role="alert">{error}</div>
{/if}

{#if loading}
	<div class="loading">
		<div class="ui-card"></div>
		<div class="ui-card"></div>
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
					<span>Approve a clean fixture ship to write the first delivery.</span>
				</div>
			{:else}
				{#each outbox as row (row.id)}
					<article class="row">
						<button class="row-head" onclick={(): void => toggle(row.id)} type="button">
							<div class="row-title">
								<strong>{row.event}</strong>
								<span class="ui-badge" data-tone={statusTone(row.status)}>{row.status}</span>
								{#if row.attempts > 0}
									<span class="muted">attempt {row.attempts}</span>
								{/if}
							</div>
							<small>{formatDateTime(row.createdAt)}</small>
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
					<span>The fake receiver stores the body and signature it checked.</span>
				</div>
			{:else}
				{#each received as delivery (delivery.id)}
					<article class="row">
						<button class="row-head" onclick={(): void => toggle(delivery.id)} type="button">
							<div class="row-title">
								<strong>{eventOf(delivery.body)}</strong>
								<span class="ui-badge" data-tone={delivery.verified === 1 ? "approved" : "rejected"}>
									{delivery.verified === 1 ? "verified" : "rejected"}
								</span>
							</div>
							<small>{formatDateTime(delivery.receivedAt)}</small>
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
	max-width: 680px;
	padding-top: var(--space-1);
	font-size: var(--text-sm);
	color: var(--text-2);
}

.counts {
	display: flex;
	gap: var(--space-4);
	font-size: var(--text-sm);
	color: var(--text-3);
}

.counts strong {
	font-variant-numeric: tabular-nums;
	color: var(--text);
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

.muted {
	font-size: var(--text-xs);
	color: var(--text-3);
}

pre {
	max-height: 340px;
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
	grid-template-columns: 80px 1fr;
	gap: var(--space-2);
}

dt {
	color: var(--text-3);
}

dd {
	margin: 0;
	overflow-wrap: anywhere;
}

.loading {
	display: grid;
	gap: var(--space-4);
}

.loading div {
	height: 180px;
	animation: pulse 1.4s ease-in-out infinite;
}

@keyframes pulse {
	50% {
		opacity: 0.55;
	}
}

@media (max-width: 900px) {
	.grid {
		grid-template-columns: 1fr;
	}
}
</style>
