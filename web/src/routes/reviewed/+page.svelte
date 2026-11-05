<script lang="ts">
import { onMount } from "svelte"
import { goto } from "$app/navigation"
import { fetchReviewed, type ReviewedItem } from "../../lib/api"
import { ApiError } from "../../lib/api-error"
import { decisionLabel, formatDate, formatDateTime, formatHours, statusTone } from "../../lib/format"

type DecisionFilter = "all" | "approved" | "changes" | "rejected"

const filters: { key: DecisionFilter; label: string }[] = [
	{ key: "all", label: "All decisions" },
	{ key: "approved", label: "Approved" },
	{ key: "changes", label: "Changes" },
	{ key: "rejected", label: "Rejected" },
]

let items = $state<ReviewedItem[]>([])
let filter = $state<DecisionFilter>("all")
let search = $state("")
let loading = $state(true)
let loadingMore = $state(false)
let hasMore = $state(false)
let error = $state("")
let filterTabs = $state<HTMLDivElement | null>(null)

const visible = $derived(
	items.filter(item => {
		if (search.trim() === "") return true

		const needle = search.trim().toLowerCase()

		return (
			item.title.toLowerCase().includes(needle)
			|| item.makerName.toLowerCase().includes(needle)
			|| item.externalId.toLowerCase().includes(needle)
		)
	}),
)

// load fetches one page, or appends the next page when keepRows is set.
async function load(target = filter, keepRows = false): Promise<void> {
	if (!keepRows) loading = true

	error = ""

	try {
		const { hasMore: more, items: fetched } = await fetchReviewed(target, keepRows ? items.length : 0)

		if (target !== filter) return

		if (keepRows) {
			const seen = new Set(items.map(item => item.id))
			items = [...items, ...fetched.filter(item => !seen.has(item.id))]
		} else items = fetched

		hasMore = more
	} catch (err) {
		if (err instanceof ApiError && err.code === "unauthorized") {
			void goto("/")
			return
		}

		if (target === filter) error = err instanceof Error ? err.message : "Could not load reviewed projects."
	} finally {
		if (target === filter) {
			loading = false
			loadingMore = false
		}
	}
}

function choose(next: DecisionFilter): void {
	if (next === filter) return

	filter = next
	items = []
	hasMore = false
	void load(next)
}

async function loadMore(): Promise<void> {
	if (loadingMore || !hasMore) return

	loadingMore = true
	await load(filter, true)
}

function stageLabel(stage: number): string {
	return stage === 2 ? "Second pass" : "First pass"
}

// isTyping keeps arrows inside a text field instead of switching filters.
function isTyping(target: EventTarget | null): boolean {
	return (
		target instanceof HTMLInputElement
		|| target instanceof HTMLTextAreaElement
		|| target instanceof HTMLSelectElement
		|| (target instanceof HTMLElement && target.isContentEditable)
	)
}

// moveFilter selects the next decision filter and keeps focus on its tab.
function moveFilter(step: number): void {
	const current = filters.findIndex(option => option.key === filter)
	const next = (current + step + filters.length) % filters.length

	filterTabs?.querySelectorAll<HTMLButtonElement>("button")[next]?.focus()
	choose(filters[next].key)
}

// onKeydown switches decision filters with the left and right arrows.
function onKeydown(event: KeyboardEvent): void {
	if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return
	if (event.ctrlKey || event.metaKey || event.altKey) return
	if (isTyping(event.target)) return

	event.preventDefault()
	moveFilter(event.key === "ArrowRight" ? 1 : -1)
}

onMount((): (() => void) => {
	void load()
	window.addEventListener("keydown", onKeydown)

	return (): void => window.removeEventListener("keydown", onKeydown)
})
</script>

<svelte:head>
	<title>Reviewed · Half Life Review</title>
</svelte:head>

<section class="page-head">
	<div>
		<h1>Reviewed</h1>
		<p>Final decisions across both passes</p>
	</div>
</section>

<div class="toolbar">
	<div aria-label="Decision filters" class="ui-seg filters" role="tablist" bind:this={filterTabs}>
		{#each filters as option (option.key)}
			<button
				aria-selected={filter === option.key}
				onclick={(): void => choose(option.key)}
				role="tab"
				type="button"
				class:active={filter === option.key}
			>
				{option.label}
			</button>
		{/each}
	</div>
	<label class="search">
		<span class="visually-hidden">Filter reviewed ships</span>
		<input class="ui-input" placeholder="Filter reviewed ships…" type="search" bind:value={search}>
	</label>
</div>

{#if error}
	<div class="ui-notice error-notice" data-tone="bad" role="alert">{error}</div>
{/if}

<section aria-busy={loading} aria-labelledby="reviewed-title">
	<div class="list-head">
		<h2 id="reviewed-title">Decided ships</h2>
		<span>
			{visible.length} {visible.length === 1 ? "ship" : "ships"}
			<span class="keys">
				· <kbd class="ui-kbd">←</kbd><kbd class="ui-kbd">→</kbd>
				filters
			</span>
		</span>
	</div>

	{#if loading}
		<div class="skeleton ui-card">
			<div></div>
			<div></div>
			<div></div>
		</div>
	{:else}
		{#if visible.length === 0}
			<div class="ui-card ui-empty">
				<strong
					>{search
						? "No reviewed projects match that search."
						: filter === "all"
							? "No reviewed projects yet."
							: "No projects with that decision."}</strong
				>
				<span
					>{search
						? "Try a different title, maker or external id."
						: filter === "all"
							? "Finished first and second passes will appear here."
							: "Choose another decision or wait for more reviews."}</span
				>
			</div>
		{:else}
			<div class="ui-card list">
				{#each visible as item (item.id)}
					<a class="row" data-sveltekit-reload href={`/review/${encodeURIComponent(item.id)}?from=reviewed`}>
						<span class="thumb">
							{#if item.thumbnailUrl}
								<img alt="" decoding="async" loading="lazy" src={item.thumbnailUrl}>
							{:else}
								<svg aria-hidden="true" fill="none" height="16" viewBox="0 0 24 24" width="16">
									<rect
										height="16"
										rx="2"
										stroke="currentColor"
										stroke-width="1.6"
										width="18"
										x="3"
										y="4"
									/>
									<circle cx="8.5" cy="9.5" fill="currentColor" r="1.5" />
									<path
										d="m4 17 4.5-4.5 3 3L15 12l5 5"
										stroke="currentColor"
										stroke-linecap="round"
										stroke-linejoin="round"
										stroke-width="1.6"
									/>
								</svg>
							{/if}
						</span>
						<span class="copy">
							<span class="title" title={item.title}>{item.title}</span>
							<span class="meta"
								>{item.makerName}
								· {item.externalId}{item.theme ? ` · ${item.theme}` : ""}</span
							>
						</span>
						<span class="outcome">
							<span class="ui-badge" data-tone={statusTone(item.decision)}
								>{decisionLabel(item.decision)}</span
							>
							<span class="stage">{stageLabel(item.stage)}</span>
						</span>
						<span class="hours">
							<strong
								>{formatHours(
									item.decision === "approved" ? item.hoursApprovedSeconds : item.hoursClaimedSeconds,
								)}</strong
							>
							<span>{item.decision === "approved" ? "approved" : "claimed"}</span>
						</span>
						<span class="date" title={formatDateTime(item.decidedAt)}>{formatDate(item.decidedAt)}</span>
					</a>
				{/each}
			</div>
		{/if}
		{#if hasMore}
			<div class="more">
				<button
					class="ui-button"
					data-variant="outline"
					disabled={loadingMore}
					onclick={loadMore}
					type="button"
				>
					{loadingMore ? "Loading…" : "Load more projects"}
				</button>
			</div>
		{/if}
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

.toolbar {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-3);
	align-items: center;
	justify-content: space-between;
	padding-bottom: var(--space-4);
}

.filters {
	flex-wrap: wrap;
	border-radius: var(--radius-lg);
}

.search {
	flex: 0 1 290px;
}

.error-notice {
	margin-bottom: var(--space-3);
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
	flex-wrap: wrap;
	gap: var(--space-3);
	align-items: center;
	padding: var(--space-3) var(--space-4);
	text-decoration: none;
	border-bottom: 1px solid var(--border);
}

.row:last-child {
	border-bottom: 0;
}

.row:hover {
	background: var(--surface-2);
}

.thumb {
	display: grid;
	flex: none;
	place-items: center;
	width: 56px;
	height: 42px;
	overflow: hidden;
	color: var(--text-3);
	background: var(--surface-2);
	border: 1px solid var(--border);
	border-radius: var(--radius-sm);
}

.thumb img {
	display: block;
	width: 100%;
	height: 100%;
	object-fit: cover;
}

.copy {
	display: flex;
	flex: 1 1 220px;
	flex-direction: column;
	min-width: 0;
}

.title,
.meta {
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.title {
	font-size: var(--text-md);
	font-weight: 700;
}

.meta {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.outcome {
	display: flex;
	flex: none;
	flex-direction: column;
	gap: 3px;
	align-items: flex-start;
}

.stage {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.hours {
	display: flex;
	flex: none;
	flex-direction: column;
	align-items: flex-end;
	min-width: 74px;
	font-size: var(--text-xs);
	color: var(--text-3);
}

.hours strong {
	font-size: var(--text-sm);
	font-variant-numeric: tabular-nums;
	color: var(--text);
}

.date {
	flex: none;
	min-width: 92px;
	font-size: var(--text-xs);
	color: var(--text-3);
	text-align: right;
}

.more {
	display: flex;
	justify-content: center;
	padding-top: var(--space-2);
}

.skeleton {
	display: grid;
	gap: 1px;
	overflow: hidden;
	background: var(--border);
}

.skeleton div {
	height: 68px;
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
	white-space: nowrap;
	clip: rect(0 0 0 0);
}

@media (max-width: 760px) {
	.search {
		flex: 1 1 100%;
	}

	.hours {
		align-items: flex-start;
		margin-left: calc(56px + var(--space-3));
	}

	.date {
		min-width: 0;
		margin-left: auto;
	}
}

@media (max-width: 980px) {
	.keys {
		display: none;
	}
}
</style>
