<script lang="ts">
import { browser } from "$app/env"
import { goto } from "$app/navigation"
import { page } from "$app/state"
import { fetchMe } from "../../../lib/api"
import { fetchStationQueue } from "../../../lib/api.gen"
import { ApiError } from "../../../lib/api-error"
import LoadingState from "../../../lib/components/LoadingState.svelte"
import StationFocus from "../../../lib/components/review/StationFocus.svelte"
import { formatHours, humanStatus, statusTone, timeAgo } from "../../../lib/format"
import { dropSignIn, session } from "../../../lib/session.svelte.js"

type StationQueue = Awaited<ReturnType<typeof fetchStationQueue>>

let key = $state("")
let data = $state<StationQueue | undefined>(undefined)
let loading = $state(true)
let error = $state("")
let loadedKey = ""
let query = $state("")
let sort = $state("")
let dir = $state<1 | -1>(1)
let seededKey = ""
let requestId = 0

const sortOptions = [
	{ label: "Default order", value: "" },
	{ label: "Ship", value: "ship" },
	{ label: "Maker", value: "maker" },
	{ label: "Status", value: "status" },
	{ label: "Type", value: "type" },
	{ label: "Version", value: "version" },
	{ label: "Claimed", value: "claimed" },
	{ label: "Age", value: "age" },
	{ label: "Checks", value: "checks" },
]

const station = $derived(data?.station)
// Search params are only available after hydration; prerendering must not read them.
let focusId = $state("")
let stage = $state(1)
const stationKey = $derived(page.params.key ?? key)

$effect((): void => {
	focusId = browser ? (page.url.searchParams.get("id") ?? "") : ""
	stage = browser && page.url.searchParams.get("stage") === "2" ? 2 : 1
})

$effect((): void => {
	const target = page.params.key ?? ""
	if (target === "") return

	if (target !== key) key = target
	const keyed = `${stage}:${target}:${query.trim()}:${sort}:${dir}`
	if (focusId !== "" || keyed === loadedKey) return

	loadedKey = keyed
	void load(target)
})

// The filter bar seeds from the URL when the station changes, then owns its state.
$effect((): void => {
	if (!browser) return
	const target = page.params.key ?? ""
	const scoped = `${stage}:${target}`
	if (scoped === seededKey) return

	seededKey = scoped
	query = page.url.searchParams.get("q") ?? ""
	sort = page.url.searchParams.get("sort") ?? ""
	dir = page.url.searchParams.get("dir") === "-1" ? -1 : 1
})

// refresh syncs the filter bar to the URL so the queue reloads and links share.
function refresh(): void {
	if (!browser) return
	const params = new URLSearchParams()
	params.set("stage", String(stage))
	if (query.trim() !== "") params.set("q", query.trim())
	if (sort !== "") {
		params.set("sort", sort)
		params.set("dir", String(dir))
	}
	void goto(`${page.url.pathname}?${params}`, { replaceState: true })
}

async function load(target: string): Promise<void> {
	// Reloads keep the current list on screen; only the first load shows a skeleton.
	const first = data === undefined
	if (first) loading = true
	error = ""
	const mine = ++requestId

	try {
		session.me ??= await fetchMe()
	} catch {
		void goto("/")
		return
	}

	try {
		const next = await fetchStationQueue(target, stage, sort, dir, query.trim())
		if (mine !== requestId || target !== key) return

		data = next
	} catch (err) {
		if (err instanceof ApiError && err.code === "unauthorized") {
			dropSignIn()
			void goto("/")
			return
		}

		if (mine !== requestId || target !== key) return

		error = err instanceof Error ? err.message : "Could not load this station."
	} finally {
		if (first && target === key) loading = false
	}
}
</script>

<svelte:head>
	<title>{focusId !== "" ? "Station focus · Ire" : station ? `${station.label} · Ire` : "Station · Ire"}</title>
</svelte:head>

{#if focusId !== ""}
	<StationFocus shipId={focusId} {stationKey} />
{:else}
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
				<div>
					<strong>{data.filteredTotal}</strong>
					<span>{data.filteredTotal === data.total ? "waiting" : `of ${data.total} waiting`}</span>
				</div>
			</div>
		{/if}
	</section>

	{#if error}
		<div class="ui-notice" data-tone="bad" role="alert">{error}</div>
	{/if}

	<div class="filter-bar">
		<input
			aria-label="Filter this station queue"
			class="ui-input"
			oninput={(event): void => {
				query = event.currentTarget.value
				refresh()
			}}
			placeholder="Filter ships — try status:ready, checks:fail>0, journals:>=2"
			type="search"
			value={query}
		>
		<select
			aria-label="Sort this station queue"
			class="ui-input"
			onchange={refresh}
			bind:value={sort}
		>
			{#each sortOptions as option (option.value)}
				<option value={option.value}>{option.label}</option>
			{/each}
		</select>
		<button
			aria-label={dir === 1 ? "Sort ascending" : "Sort descending"}
			class="ui-button ghost sm"
			disabled={sort === ""}
			onclick={(): void => {
				dir = dir === 1 ? -1 : 1
				refresh()
			}}
			title={dir === 1 ? "Ascending" : "Descending"}
			type="button"
		>
			{dir === 1 ? "↑" : "↓"}
		</button>
	</div>

	{#if loading}
		<div class="ui-card">
			<LoadingState message="Loading station queue…" />
		</div>
	{:else if data && data.items.length === 0}
		<div class="ui-card ui-empty">
			<strong>{query.trim() === "" ? "Nothing waiting here" : "No ships match"}</strong>
			<span
				>{query.trim() === ""
					? "Ships arrive when the previous station is done."
					: "Try a different filter for this station."}</span
			>
		</div>
	{:else if data}
		<div class="ui-card list">
			{#each data.items as item (item.id)}
				<a
					class="row"
					href={`/station/${encodeURIComponent(key)}?id=${encodeURIComponent(item.id)}&stage=${stage}`}
				>
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
					</span>
				</a>
			{/each}
		</div>
		{#if data.hasMore}
			<p class="capped">Showing the first {data.items.length} of {data.total}; open the queue to see the rest.</p>
		{/if}
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
	flex-basis: 100%;
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

.filter-bar {
	display: flex;
	gap: var(--space-2);
	padding-bottom: var(--space-3);
}

.filter-bar input {
	flex: 1 1 auto;
	min-width: 0;
}

.filter-bar select {
	flex: 0 1 11rem;
	min-width: 0;
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

.capped {
	padding: var(--space-2) var(--space-4) var(--space-3);
	font-size: var(--text-xs);
	color: var(--text-3);
}
</style>
