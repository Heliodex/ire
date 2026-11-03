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
let refreshing = $state(false)
let opening = $state("")
let error = $state("")
let selected = $state(0)
let search = $state("")
let filterTabs = $state<HTMLDivElement | null>(null)

// cache keeps each filter's last response for this visit, so switching back is instant while a fresh copy loads behind it.
const cache = new Map<Filter, Queue>()

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
		} else if (event.key === "ArrowRight") {
			moveFilter(1)
			event.preventDefault()
		} else if (event.key === "ArrowLeft") {
			moveFilter(-1)
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

// checkTone maps a check result to a dot tone. A failure with warning severity stays orange, matching the review screen.
function checkTone(check: QueueItem["checks"][number]): string {
	if (check.status === "pass") return "approved"
	if (check.status === "warn") return "pending"
	if (check.status === "fail" && check.severity === "warn") return "changes"
	if (check.status === "fail" || check.status === "error") return "rejected"
	return "neutral"
}

async function load(target = filter): Promise<void> {
	try {
		const next = await fetchQueue(target)
		if (target !== filter) return

		cache.set(target, next)
		queue = next
		selected = Math.min(selected, Math.max(queue.items.length - 1, 0))
		error = ""
	} catch (err) {
		if (err instanceof ApiError && err.code === "unauthorized") {
			void goto("/")
			return
		}
		if (target === filter) error = err instanceof Error ? err.message : "Could not load the queue."
	} finally {
		if (target === filter) {
			loading = false
			refreshing = false
		}
	}
}

async function choose(next: Filter): Promise<void> {
	if (next === filter) {
		void load(next)
		return
	}

	filter = next
	selected = 0
	error = ""

	const cached = cache.get(next)
	if (cached) {
		// Show the last copy immediately and let load() replace it without a skeleton.
		queue = cached
		refreshing = false
		void load(next)
		return
	}

	// Keep the current rows visible while the first fetch for this filter runs.
	refreshing = true
	await load(next)
}

// moveFilter selects the next filter and keeps keyboard focus on it, wrapping at the ends.
function moveFilter(step: number): void {
	const index = filters.findIndex((option) => option.key === filter)
	const next = (index + step + filters.length) % filters.length

	filterTabs?.querySelectorAll<HTMLButtonElement>("button")[next]?.focus()
	void choose(filters[next].key)
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
	<div aria-label="Filters" bind:this={filterTabs} class="ui-seg" role="tablist">
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

<section aria-busy={loading || refreshing} aria-labelledby="list-title">
	<div class="list-head">
		<h2 id="list-title">Open ships</h2>
		<span>
			{visible.length} {visible.length === 1 ? "ship" : "ships"} ·
			<kbd class="ui-kbd">J</kbd><kbd class="ui-kbd">K</kbd> to move ·
			<kbd class="ui-kbd">←</kbd><kbd class="ui-kbd">→</kbd> filters ·
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
		<div class="ui-card list" class:refreshing>
			{#each visible as item, index (item.id)}
				<button
					aria-current={index === selected}
					class="row"
					class:selected={index === selected}
					disabled={opening !== ""}
					onclick={(): Promise<void> => open(item)}
					type="button"
				>
					<span class="ship-title" title={item.title}>{item.title}</span>
					<span
						class="ui-badge status"
						data-tone={statusTone(item.status)}
						title={item.claimedBy ? `Claimed by ${item.claimedBy}` : humanStatus(item.status)}
					>
						{item.claimedBy === session.me?.user.email ? "Yours" : humanStatus(item.status)}
					</span>
					<span class="theme">{item.theme ?? ""}</span>
					<div class="facts">
						<span class="maker">{item.makerName}</span>
						<span class="version">v{item.version}</span>
						<span class="hours">{formatHours(item.hoursClaimedSeconds)} claimed</span>
						<span class="age">{timeAgo(item.ingestedAt)}</span>
						<span class="checks">
							<span class="counts">
								{#if item.checkSummary.pass > 0}<span class="pass">{item.checkSummary.pass} pass</span>{/if}
								{#if item.checkSummary.warn > 0}<span class="warn">{item.checkSummary.warn} warn</span>{/if}
								{#if item.checkSummary.fail > 0}<span class="fail">{item.checkSummary.fail} fail</span>{/if}
								{#if item.checkSummary.error > 0}<span class="fail">{item.checkSummary.error} error</span>{/if}
								{#if item.checkSummary.overall === "pass"}<span class="fast">Fast lane</span>{/if}
							</span>
							{#if item.checks.length > 0}
								<span
									aria-label={item.checks.map((check) => `${check.label}: ${check.status}`).join(", ")}
									class="dots"
									role="img"
								>
									{#each item.checks as check (check.key)}
										<span class="dot" data-tone={checkTone(check)} title={`${check.label}: ${check.status}`}></span>
									{/each}
								</span>
							{/if}
						</span>
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
	transition: opacity 120ms ease;
}

.list.refreshing {
	opacity: 0.55;
}

.row {
	display: grid;
	grid-template-columns:
		minmax(0, 1fr) 96px 64px minmax(110px, 160px) 36px 96px 64px minmax(120px, 160px) 56px;
	gap: var(--space-3);
	align-items: center;
	width: 100%;
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

.ship-title {
	overflow: hidden;
	font-size: var(--text-md);
	font-weight: 700;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.status {
	justify-self: start;
}

.theme {
	overflow: hidden;
	font-size: var(--text-xs);
	color: var(--text-3);
	text-overflow: ellipsis;
	white-space: nowrap;
}

.facts {
	display: contents;
}

.maker,
.hours {
	overflow: hidden;
	font-size: var(--text-sm);
	color: var(--text-2);
	text-overflow: ellipsis;
	white-space: nowrap;
}

.version,
.age {
	font-size: var(--text-xs);
	color: var(--text-3);
	font-variant-numeric: tabular-nums;
}

.checks {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	min-width: 0;
	font-size: var(--text-xs);
	font-weight: 700;
}

.counts {
	display: flex;
	gap: var(--space-2);
	overflow: hidden;
	white-space: nowrap;
}

.checks .pass {
	color: var(--status-approved);
}

.checks .warn {
	color: var(--status-pending);
}

.checks .fail {
	color: var(--status-rejected);
}

.checks .fast {
	color: var(--text-3);
	font-weight: 600;
}

.dots {
	display: flex;
	flex-wrap: wrap;
	gap: 4px;
}

.dot {
	width: 7px;
	height: 7px;
	background: var(--border-2);
	border-radius: 50%;
}

.dot[data-tone="approved"] {
	background: var(--status-approved);
}

.dot[data-tone="pending"] {
	background: var(--status-pending);
}

.dot[data-tone="changes"] {
	background: var(--status-changes);
}

.dot[data-tone="rejected"] {
	background: var(--status-rejected);
}

.row-action {
	display: inline-flex;
	justify-self: end;
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

@media (max-width: 1024px) {
	.row {
		grid-template-columns: minmax(0, 1fr) 96px 64px auto;
		grid-template-areas:
			"title status theme action"
			"facts facts facts facts";
		row-gap: var(--space-1);
	}

	.ship-title {
		grid-area: title;
	}

	.status {
		grid-area: status;
	}

	.theme {
		grid-area: theme;
	}

	.row-action {
		grid-area: action;
	}

	.facts {
		display: grid;
		grid-template-columns: minmax(110px, 1fr) 36px 96px 64px minmax(120px, 160px);
		gap: var(--space-3);
		grid-area: facts;
		align-items: baseline;
	}
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
		grid-template-columns: auto 1fr auto;
		grid-template-areas:
			"title title action"
			"status theme theme"
			"facts facts facts";
	}

	.facts {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-3);
	}
}
</style>
