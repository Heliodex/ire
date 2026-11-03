<script lang="ts">
import { goto } from "$app/navigation"
import { onMount } from "svelte"
import { fetchMe, fetchOutbox, fetchReceived, type FakeDelivery, type OutboxRow } from "../../lib/api"
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
	<title>Deliveries · Half Life Review</title>
</svelte:head>

<section class="head">
	<div>
		<p class="eyebrow">Outbound · signed</p>
		<h1>Deliveries</h1>
		<p class="lede">
			Every final decision is written to the outbox before it is sent. The receiver verifies the same
			<code>timestamp.delivery_id.body</code> signature Half Life's Ari handler checks.
		</p>
	</div>
	<div class="hint">
		<span>{outbox.filter((row) => row.status === "delivered").length} delivered</span>
		<span>{outbox.filter((row) => row.status !== "delivered").length} pending</span>
	</div>
</section>

{#if error}
	<div class="error" role="alert">{error}</div>
{/if}

{#if loading}
	<div class="loading"><div></div><div></div></div>
{:else}
	<div class="grid">
		<section class="card">
			<div class="card-head">
				<h2>Outbox</h2>
				<span>{outbox.length} rows</span>
			</div>
			{#if outbox.length === 0}
				<div class="empty">
					<p>Nothing sent yet.</p>
					<span>Approve a clean fixture ship to write the first delivery.</span>
				</div>
			{:else}
				<div class="rows">
					{#each outbox as row (row.id)}
						<article class="row">
							<button class="row-head" onclick={(): void => toggle(row.id)} type="button">
								<div>
									<strong>{row.event}</strong>
									<span class="status {statusTone(row.status)}">{row.status}</span>
									{#if row.attempts > 0}<span class="muted">attempt {row.attempts}</span>{/if}
								</div>
								<small>{formatDateTime(row.createdAt)}</small>
							</button>
							{#if open === row.id && row.payload}
								<pre>{row.payload}</pre>
							{/if}
						</article>
					{/each}
				</div>
			{/if}
		</section>

		<section class="card">
			<div class="card-head">
				<h2>Receiver</h2>
				<span>{received.length} verified</span>
			</div>
			{#if received.length === 0}
				<div class="empty">
					<p>No deliveries received.</p>
					<span>The fake receiver stores the body and signature it checked.</span>
				</div>
			{:else}
				<div class="rows">
					{#each received as delivery (delivery.id)}
						<article class="row">
							<button class="row-head" onclick={(): void => toggle(delivery.id)} type="button">
								<div>
									<strong>{eventOf(delivery.body)}</strong>
									<span class="status {delivery.verified === 1 ? "good" : "bad"}">
										{delivery.verified === 1 ? "verified" : "rejected"}
									</span>
								</div>
								<small>{formatDateTime(delivery.receivedAt)}</small>
							</button>
							{#if open === delivery.id}
								<dl>
									<div><dt>Delivery</dt><dd>{delivery.deliveryId}</dd></div>
									<div><dt>Timestamp</dt><dd>{delivery.timestamp}</dd></div>
									<div><dt>Signature</dt><dd class="mono">{delivery.signature}</dd></div>
								</dl>
								<pre>{delivery.body}</pre>
							{/if}
						</article>
					{/each}
				</div>
			{/if}
		</section>
	</div>
{/if}

<style>
.head {
	display: flex;
	flex-wrap: wrap;
	gap: 24px;
	align-items: flex-end;
	justify-content: space-between;
	padding-bottom: 22px;
}

.eyebrow {
	padding-bottom: 9px;
	font-size: 0.68rem;
	font-weight: 750;
	color: var(--accent);
	text-transform: uppercase;
	letter-spacing: 0.13em;
}

h1 {
	font-size: clamp(2rem, 4.3vw, 3rem);
}

.lede {
	max-width: 720px;
	padding-top: 10px;
	font-size: 0.9rem;
	color: var(--muted);
}

code {
	font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
	font-size: 0.85em;
}

.hint {
	display: flex;
	gap: 14px;
	font-size: 0.74rem;
	color: var(--muted);
}

.grid {
	display: grid;
	grid-template-columns: 1fr 1fr;
	gap: 16px;
	align-items: start;
}

.card {
	padding: 18px;
	background: var(--card);
	border: 1px solid var(--line);
	border-radius: var(--radius);
}

.card-head {
	display: flex;
	gap: 10px;
	align-items: baseline;
	justify-content: space-between;
	padding-bottom: 12px;
}

.card-head h2 {
	font-size: 1.05rem;
}

.card-head span {
	font-size: 0.72rem;
	color: var(--muted);
}

.rows {
	display: grid;
	gap: 8px;
}

.row {
	background: var(--paper);
	border: 1px solid var(--line);
	border-radius: 10px;
}

.row-head {
	display: flex;
	width: 100%;
	gap: 10px;
	align-items: baseline;
	justify-content: space-between;
	padding: 10px 12px;
	text-align: left;
	background: transparent;
	border: 0;
}

.row-head > div {
	display: flex;
	flex-wrap: wrap;
	gap: 7px;
	align-items: baseline;
}

.row-head strong {
	font-size: 0.8rem;
}

.row-head small {
	font-size: 0.68rem;
	color: var(--muted);
}

.status {
	padding: 2px 7px;
	font-size: 0.6rem;
	font-weight: 750;
	text-transform: uppercase;
	letter-spacing: 0.06em;
	border-radius: 999px;
}

.status.good {
	color: var(--teal);
	background: var(--teal-soft);
}

.status.warn {
	color: var(--accent-deep);
	background: var(--accent-soft);
}

.status.bad {
	color: #7d2a12;
	background: #fbe5df;
}

.muted {
	font-size: 0.68rem;
	color: var(--muted);
}

pre {
	max-height: 340px;
	padding: 12px;
	margin: 0 8px 10px;
	font-size: 0.7rem;
	overflow: auto;
	color: #364043;
	background: #fff;
	border: 1px solid var(--line);
	border-radius: 8px;
}

dl {
	display: grid;
	gap: 4px;
	padding: 0 12px 8px;
	font-size: 0.72rem;
}

dl div {
	display: grid;
	grid-template-columns: 80px 1fr;
	gap: 8px;
}

dt {
	color: var(--muted);
}

dd {
	overflow-wrap: anywhere;
}

.mono {
	font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

.empty {
	padding: 24px;
	text-align: center;
	color: var(--muted);
	border: 1px dashed var(--line);
	border-radius: 10px;
}

.empty p {
	font-weight: 700;
	color: var(--ink);
}

.empty span {
	display: block;
	padding-top: 4px;
	font-size: 0.76rem;
}

.error {
	padding: 10px 13px;
	margin-bottom: 14px;
	font-size: 0.84rem;
	color: #7d2a12;
	background: #fbe5df;
	border: 1px solid #e9b9aa;
	border-radius: 10px;
}

.loading {
	display: grid;
	gap: 12px;
}

.loading div {
	height: 120px;
	background: linear-gradient(90deg, var(--card), var(--paper-deep), var(--card));
	background-size: 200% 100%;
	border: 1px solid var(--line);
	border-radius: var(--radius);
	animation: shimmer 1.4s infinite;
}

@keyframes shimmer {
	to {
		background-position: -200% 0;
	}
}

@media (max-width: 900px) {
	.grid {
		grid-template-columns: 1fr;
	}
}
</style>
