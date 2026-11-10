<script lang="ts">
import { goto } from "$app/navigation"
import { page } from "$app/state"
import { fetchMe } from "../../../lib/api"
import { fetchStationQueue } from "../../../lib/api.gen"
import { ApiError } from "../../../lib/api-error"
import LoadingState from "../../../lib/components/LoadingState.svelte"
import { formatHours, humanStatus, statusTone, timeAgo } from "../../../lib/format"
import { dropSignIn, session } from "../../../lib/session.svelte.js"

type StationQueue = Awaited<ReturnType<typeof fetchStationQueue>>

let key = $state("")
let data = $state<StationQueue | undefined>(undefined)
let loading = $state(true)
let error = $state("")

const station = $derived(data?.station)

$effect((): void => {
	const target = page.params.key ?? ""
	if (target === "" || target === key) return

	key = target
	void load(target)
})

async function load(target: string): Promise<void> {
	loading = true
	error = ""
	data = undefined

	try {
		session.me ??= await fetchMe()
	} catch {
		void goto("/")
		return
	}

	try {
		const next = await fetchStationQueue(target)
		if (target !== key) return

		data = next
	} catch (err) {
		if (err instanceof ApiError && err.code === "unauthorized") {
			dropSignIn()
			void goto("/")
			return
		}

		if (target !== key) return

		error = err instanceof Error ? err.message : "Could not load this station."
	} finally {
		if (target === key) loading = false
	}
}
</script>

<svelte:head>
	<title>{station ? `${station.label} · Ire` : "Station · Ire"}</title>
</svelte:head>

<section class="page-head">
	<div>
		<p class="crumbs"><a href="/stations">Stations</a></p>
		<h1>{station?.label ?? "Station"}</h1>
		{#if station}
			<p>{station.description || "Open a ship to answer this station's questions."}</p>
		{/if}
	</div>

	{#if data}
		<div class="stats ui-card">
			<div><strong>{data.items.length}</strong><span>waiting</span></div>
		</div>
	{/if}
</section>

{#if error}
	<div class="ui-notice" data-tone="bad" role="alert">{error}</div>
{/if}

{#if loading}
	<div class="ui-card">
		<LoadingState message="Loading station queue…" />
	</div>
{:else if data && data.items.length === 0}
	<div class="ui-card ui-empty">
		<strong>Nothing waiting here</strong>
		<span>Ships arrive when the previous station is done.</span>
	</div>
{:else if data}
	<div class="ui-card list">
		{#each data.items as item (item.id)}
			<a class="row" href={`/review/${encodeURIComponent(item.id)}?station=${encodeURIComponent(key)}`}>
				<span class="row-main">
					<span class="row-title">{item.title}</span>
					<span class="row-meta">
						<span>{item.makerName}</span>
						{#if item.theme}
							<span class="theme">{item.theme}</span>
						{/if}
						<span>{formatHours(item.hoursClaimedSeconds)} claimed</span>
						<span>{timeAgo(item.submittedAt)}</span>
					</span>
				</span>
				<span class="row-side">
					<span class="ui-badge" data-tone={statusTone(item.status)}>{humanStatus(item.status)}</span>
					<span class="open">Open</span>
				</span>
			</a>
		{/each}
	</div>
	{#if data.hasMore}
		<p class="capped">The list is capped; open the queue to see the rest.</p>
	{/if}
{/if}

<style>
.page-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-4);
	align-items: flex-end;
	justify-content: space-between;
	padding-bottom: var(--space-4);
}

.crumbs {
	padding-bottom: var(--space-1);
	font-size: var(--text-sm);
	color: var(--text-3);
}

.crumbs a {
	color: var(--text-2);
}

.crumbs a:hover {
	color: var(--primary);
}

h1 {
	font-size: var(--text-2xl);
}

.page-head p {
	padding-top: var(--space-1);
	font-size: var(--text-sm);
	color: var(--text-2);
}

.stats {
	display: flex;
	overflow: hidden;
}

.stats div {
	min-width: 5.25rem;
	padding: var(--space-2) var(--space-3);
}

.stats strong {
	display: block;
	font-size: var(--text-lg);
	font-weight: 800;
	font-variant-numeric: tabular-nums;
}

.stats span {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.list {
	overflow: hidden;
}

.row {
	display: flex;
	gap: var(--space-3);
	align-items: center;
	justify-content: space-between;
	padding: var(--space-3) var(--space-4);
	color: inherit;
	text-decoration: none;
	border-top: 1px solid var(--border);
}

.row:first-child {
	border-top: 0;
}

.row:hover {
	background: var(--surface-2);
}

.row-main {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	min-width: 0;
}

.row-title {
	font-size: var(--text-sm);
	font-weight: 700;
	overflow-wrap: anywhere;
}

.row-meta {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	font-size: var(--text-xs);
	color: var(--text-3);
}

.theme {
	padding: 0 5px;
	background: var(--surface-3);
	border-radius: 99px;
}

.row-side {
	display: flex;
	flex: none;
	gap: var(--space-2);
	align-items: center;
}

.open {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--primary);
}

.capped {
	padding: var(--space-2) var(--space-4) var(--space-3);
	font-size: var(--text-xs);
	color: var(--text-3);
}
</style>
