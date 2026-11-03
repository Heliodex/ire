<script lang="ts">
import { goto } from "$app/navigation"
import { onMount } from "svelte"
import { claimShip, fetchMe, fetchQueue, type Queue, type QueueItem } from "../../lib/api"
import { ApiError } from "../../lib/api-error"
import { formatHours, humanStatus, statusTone, timeAgo } from "../../lib/format"
import { session } from "../../lib/session.svelte.js"

type Filter = "all" | "fast" | "attention" | "mine"

let queue = $state<Queue>({ counts: { ready: 0, claimed: 0, needsAttention: 0, stage2Hold: 0 }, items: [] })
let filter = $state<Filter>("all")
let loading = $state(true)
let opening = $state("")
let error = $state("")
let selected = $state(0)
let search = $state("")

const filters: { key: Filter; label: string }[] = [
	{ key: "all", label: "All open" },
	{ key: "fast", label: "Fast lane" },
	{ key: "attention", label: "Needs a look" },
	{ key: "mine", label: "Mine" },
]

const visible = $derived(
	queue.items.filter((item) => {
		if (search.trim() === "") return true
		const needle = search.trim().toLowerCase()
		return item.title.toLowerCase().includes(needle) || item.makerName.toLowerCase().includes(needle)
	}),
)

onMount((): (() => void) => {
	void boot()
	const timer = setInterval((): void => void load(), 10000)

	function onKeydown(event: KeyboardEvent): void {
		if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return

		if (event.key === "j") {
			selected = Math.min(selected + 1, visible.length - 1)
			event.preventDefault()
		} else if (event.key === "k") {
			selected = Math.max(selected - 1, 0)
			event.preventDefault()
		} else if (event.key === "Enter" && visible[selected]) {
			void open(visible[selected])
			event.preventDefault()
		}
	}

	window.addEventListener("keydown", onKeydown)

	return (): void => {
		clearInterval(timer)
		window.removeEventListener("keydown", onKeydown)
	}
})

async function boot(): Promise<void> {
	try {
		session.me ??= await fetchMe()
	} catch {
		void goto("/")
		return
	}
	await load()
}

async function load(): Promise<void> {
	try {
		queue = await fetchQueue(filter)
		selected = Math.min(selected, Math.max(queue.items.length - 1, 0))
		error = ""
	} catch (err) {
		if (err instanceof ApiError && err.code === "unauthorized") {
			void goto("/")
			return
		}
		error = err instanceof Error ? err.message : "Could not load the queue."
	} finally {
		loading = false
	}
}

async function choose(next: Filter): Promise<void> {
	filter = next
	loading = true
	selected = 0
	await load()
}

async function open(item: QueueItem): Promise<void> {
	opening = item.id
	error = ""
	try {
		if (!item.claimedBy || item.claimedBy !== session.me?.user.email) await claimShip(item.id)
		await goto(`/review?id=${encodeURIComponent(item.id)}`)
	} catch (err) {
		error = err instanceof Error ? err.message : "Could not claim this ship."
		await load()
	} finally {
		opening = ""
	}
}
</script>

<svelte:head>
	<title>Queue · Half Life Review</title>
</svelte:head>

<section class="page-head">
	<div>
		<h1>Queue</h1>
		<p>Stage 1 · first pass</p>
	</div>

	<div class="stats ui-card">
		<div><strong>{queue.counts.ready}</strong><span>Ready</span></div>
		<div><strong>{queue.counts.claimed}</strong><span>In review</span></div>
		<div><strong>{queue.counts.needsAttention}</strong><span>Needs a look</span></div>
		<div><strong>{queue.counts.stage2Hold}</strong><span>Second pass</span></div>
	</div>
</section>

<div class="toolbar">
	<div aria-label="Filters" class="ui-seg" role="tablist">
		{#each filters as option (option.key)}
			<button
				aria-selected={filter === option.key}
				class:active={filter === option.key}
				onclick={(): Promise<void> => choose(option.key)}
				role="tab"
				type="button"
			>
				{option.label}
			</button>
		{/each}
	</div>
	<label class="search">
		<span class="visually-hidden">Search the queue</span>
		<input bind:value={search} class="ui-input" placeholder="Search project or maker…" type="search">
	</label>
</div>

{#if error}
	<div class="ui-notice" data-tone="bad" role="alert">{error}</div>
{/if}

<section aria-busy={loading} aria-labelledby="list-title">
	<div class="list-head">
		<h2 id="list-title">Open ships</h2>
		<span>
			{visible.length} {visible.length === 1 ? "ship" : "ships"} ·
			<kbd class="ui-kbd">J</kbd><kbd class="ui-kbd">K</kbd> to move ·
			<kbd class="ui-kbd">Enter</kbd> to open
		</span>
	</div>

	{#if loading}
		<div class="skeleton ui-card">
			<div></div>
			<div></div>
			<div></div>
		</div>
	{:else if visible.length === 0}
		<div class="ui-card ui-empty">
			<strong>{search ? "No ships match that search." : "Nothing in this filter."}</strong>
			<span>{filter === "attention" ? "The checks found nothing blocking." : "Check back after the next ingest."}</span>
		</div>
	{:else}
		<div class="ui-card list">
			{#each visible as item, index (item.id)}
				<button
					aria-current={index === selected}
					class="row"
					class:selected={index === selected}
					disabled={opening !== ""}
					onclick={(): Promise<void> => open(item)}
					type="button"
				>
					<div class="row-main">
						<div class="row-title">
							<h3>{item.title}</h3>
							<span class="ui-badge" data-tone={statusTone(item.status)}>{humanStatus(item.status)}</span>
							{#if item.theme}<span class="theme">{item.theme}</span>{/if}
						</div>
						<div class="row-meta">
							<span>{item.makerName}</span>
							<span>v{item.version}</span>
							<span>{formatHours(item.hoursClaimedSeconds)} claimed</span>
							<span>{timeAgo(item.ingestedAt)}</span>
							{#if item.claimedBy}
								<span class="claim">
									{item.claimedBy === session.me?.user.email ? "Yours" : `Claimed by ${item.claimedBy}`}
								</span>
							{/if}
						</div>
						<div class="row-checks">
							{#if item.checkSummary.pass > 0}<span class="pass">{item.checkSummary.pass} pass</span>{/if}
							{#if item.checkSummary.warn > 0}<span class="warn">{item.checkSummary.warn} warn</span>{/if}
							{#if item.checkSummary.fail > 0}<span class="fail">{item.checkSummary.fail} fail</span>{/if}
							{#if item.checkSummary.error > 0}<span class="fail">{item.checkSummary.error} error</span>{/if}
							{#if item.checkSummary.overall === "pass"}<span class="fast">Fast lane</span>{/if}
						</div>
					</div>
					<span class="row-action">
						{opening === item.id ? "Claiming…" : "Open"}
						<svg aria-hidden="true" fill="none" height="14" viewBox="0 0 16 16" width="14">
							<path d="M3 8h9M8.5 3.5 13 8l-4.5 4.5" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.7"/>
						</svg>
					</span>
				</button>
			{/each}
		</div>
	{/if}
</section>

<style>
.page-head {
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
	min-width: 84px;
	padding: var(--space-2) var(--space-3);
	border-left: 1px solid var(--border);
}

.stats div:first-child {
	border-left: 0;
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

.toolbar {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-3);
	align-items: center;
	justify-content: space-between;
	padding-bottom: var(--space-4);
}

.search {
	flex: 0 1 260px;
}

.list-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: baseline;
	justify-content: space-between;
	padding-bottom: var(--space-2);
}

.list-head h2 {
	font-size: var(--text-md);
	font-weight: 700;
}

.list-head span {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.list {
	overflow: hidden;
}

.row {
	display: flex;
	width: 100%;
	gap: var(--space-4);
	align-items: center;
	justify-content: space-between;
	padding: var(--space-3) var(--space-4);
	text-align: left;
	background: transparent;
	border: 0;
	border-bottom: 1px solid var(--border);
}

.row:last-child {
	border-bottom: 0;
}

.row:hover:not(:disabled) {
	background: var(--surface-2);
}

.row.selected {
	background: var(--surface-2);
	box-shadow: inset 3px 0 0 var(--primary);
}

.row:disabled {
	cursor: wait;
}

.row-main {
	display: flex;
	flex: 1;
	flex-direction: column;
	gap: var(--space-1);
	min-width: 0;
}

.row-title {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
}

.row-title h3 {
	font-size: var(--text-md);
	font-weight: 700;
}

.theme {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.row-meta {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-3);
	font-size: var(--text-sm);
	color: var(--text-2);
}

.claim {
	color: var(--primary);
}

.row-checks {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-3);
	padding-top: var(--space-1);
	font-size: var(--text-xs);
	font-weight: 700;
}

.row-checks .pass {
	color: var(--status-approved);
}

.row-checks .warn {
	color: var(--status-pending);
}

.row-checks .fail {
	color: var(--status-rejected);
}

.row-checks .fast {
	color: var(--text-3);
	font-weight: 600;
}

.row-action {
	display: inline-flex;
	flex: none;
	gap: var(--space-1);
	align-items: center;
	font-size: var(--text-sm);
	font-weight: 700;
	color: var(--text-3);
}

.row:hover .row-action {
	color: var(--primary);
}

.skeleton {
	display: grid;
	gap: 1px;
	overflow: hidden;
	background: var(--border);
}

.skeleton div {
	height: 84px;
	background: var(--surface);
	animation: pulse 1.4s ease-in-out infinite;
}

@keyframes pulse {
	50% {
		opacity: 0.55;
	}
}

.visually-hidden {
	position: absolute;
	width: 1px;
	height: 1px;
	overflow: hidden;
	clip: rect(0 0 0 0);
	white-space: nowrap;
}

@media (max-width: 640px) {
	.stats {
		width: 100%;
	}

	.stats div {
		flex: 1;
		min-width: 0;
	}

	.search {
		flex: 1 1 100%;
	}

	.row {
		align-items: flex-start;
	}

	.row-action {
		padding-top: var(--space-1);
	}
}
</style>
