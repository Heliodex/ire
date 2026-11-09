<script lang="ts">
import { onMount, tick } from "svelte"
import { cubicOut } from "svelte/easing"
import { fly } from "svelte/transition"
import { goto } from "$app/navigation"
import { fetchReviewed, type ReviewedItem } from "../../lib/api"
import { ApiError } from "../../lib/api-error"
import LoadingState from "../../lib/components/LoadingState.svelte"
import { decisionLabel, formatDate, formatDateTime, formatHours, statusTone } from "../../lib/format"
import { dropSignIn } from "../../lib/session.svelte.js"

type DecisionFilter = "all" | "approved" | "changes" | "rejected"

const filters: { key: DecisionFilter; label: string }[] = [
	{ key: "all", label: "All decisions" },
	{ key: "approved", label: "Approved" },
	{ key: "changes", label: "Changes" },
	{ key: "rejected", label: "Rejected" },
]

let items = $state<ReviewedItem[]>([])
let filter = $state<DecisionFilter>("all")
let filterDir = $state(1)
let filterDragX = $state(0)
let filterDragStep = $state(1)
let filterDragName = $state("")
let filterDragging = $state(false)
let filterSettling = $state(false)
let filterSkipAnim = $state(false)
const filterSwipeGap = 16
let search = $state("")
let loading = $state(true)
let loadingMore = $state(false)
let hasMore = $state(false)
let error = $state("")
let filterTabs = $state<HTMLDivElement | null>(undefined)

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
			dropSignIn()
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

function filterTarget(step: number): { dir: number; filter: DecisionFilter; name: string } | undefined {
	const current = filters.findIndex(option => option.key === filter)
	const next = (current + step + filters.length) % filters.length
	const option = filters[next]
	return option ? { dir: step, filter: option.key, name: option.label } : undefined
}

function choose(next: DecisionFilter, dir?: number): void {
	if (next === filter) return

	const current = filters.findIndex(option => option.key === filter)
	const target = filters.findIndex(option => option.key === next)
	filterDir = dir ?? (target >= current ? 1 : -1)
	filter = next
	items = []
	hasMore = false
	loadingMore = false
	void load(next)
}

async function loadMore(): Promise<void> {
	if (loadingMore || !hasMore) return

	loadingMore = true
	await load(filter, true)
}

function stageLabel(stage: number): string {
	return stage === 2 ? "2nd pass" : "1st pass"
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
	const target = filterTarget(step)
	if (!target) return

	const idx = filters.findIndex(option => option.key === target.filter)
	filterTabs?.querySelectorAll<HTMLButtonElement>("button")[idx]?.focus()
	choose(target.filter, step)
}

// swipeFilters follows the finger during a horizontal swipe and commits or snaps back on release.
function swipeFilters(node: HTMLElement): { destroy(): void } {
	let startX = 0
	let startY = 0
	let startT = 0
	let tracking = false
	let locked: "h" | "v" | undefined
	let target: { dir: number; filter: DecisionFilter; name: string } | undefined
	let settleTimer: ReturnType<typeof setTimeout> | undefined

	function clearDrag(): void {
		filterDragName = ""
		filterDragX = 0
		filterDragging = false
		filterSettling = false
		filterDragStep = 1
		target = undefined
	}

	function settle(commit: boolean): void {
		filterDragging = false
		filterSettling = true
		filterDragX = commit && target ? -target.dir * ((node.offsetWidth || 1) + filterSwipeGap) : 0
		settleTimer = setTimeout(() => {
			settleTimer = undefined
			if (commit && target) {
				const next = target
				filterSkipAnim = true
				choose(next.filter, next.dir)
				void (async (): Promise<void> => {
					await tick()
					filterDragging = true
					filterSettling = false
					filterDragX = 0
					await tick()
					clearDrag()
					filterDragging = false
					filterSkipAnim = false
				})()
				return
			}
			clearDrag()
		}, 190)
	}

	function onStart(event: TouchEvent): void {
		if (event.touches.length !== 1) {
			tracking = false
			return
		}
		if (settleTimer) {
			clearTimeout(settleTimer)
			settleTimer = undefined
			filterSkipAnim = false
			clearDrag()
		}
		const [touch] = event.touches
		if (!touch) return
		startX = touch.clientX
		startY = touch.clientY
		startT = Date.now()
		tracking = true
		locked = undefined
		target = undefined
	}

	function onMove(event: TouchEvent): void {
		if (!tracking) return
		const [touch] = event.touches
		if (!touch) return
		const dx = touch.clientX - startX
		const dy = touch.clientY - startY
		if (locked === undefined) {
			if (Math.abs(dx) < 8 && Math.abs(dy) < 8) return
			locked = Math.abs(dx) > Math.abs(dy) * 1.2 ? "h" : "v"
		}
		if (locked !== "h") return
		const dir = dx < 0 ? 1 : -1
		target = filterTarget(dir)
		if (!target) return
		filterDragStep = dir
		filterDragName = target.name
		filterDragging = true
		filterDragX = dx
	}

	function onEnd(event: TouchEvent): void {
		if (!tracking) return
		tracking = false
		if (locked !== "h" || !filterDragging || !target) {
			clearDrag()
			locked = undefined
			return
		}
		const [touch] = event.changedTouches
		const dx = touch ? touch.clientX - startX : filterDragX
		const dt = Date.now() - startT
		const width = node.offsetWidth || 1
		const commit = Math.abs(dx) > width * 0.22 || (Math.abs(dx) / Math.max(dt, 1) > 0.45 && Math.abs(dx) > 40)
		settle(commit)
		locked = undefined
	}

	function onCancel(): void {
		tracking = false
		locked = undefined
		if (filterDragging || filterDragName) settle(false)
	}

	node.addEventListener("touchstart", onStart, { passive: true })
	node.addEventListener("touchmove", onMove, { passive: true })
	node.addEventListener("touchend", onEnd, { passive: true })
	node.addEventListener("touchcancel", onCancel, { passive: true })

	return {
		destroy(): void {
			if (settleTimer) clearTimeout(settleTimer)
			node.removeEventListener("touchstart", onStart)
			node.removeEventListener("touchmove", onMove)
			node.removeEventListener("touchend", onEnd)
			node.removeEventListener("touchcancel", onCancel)
		},
	}
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
	<title>Reviewed · Ire</title>
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
		<input
			aria-label="Filter reviewed ships"
			class="ui-input"
			placeholder="Filter reviewed ships…"
			type="search"
			bind:value={search}
		>
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

	<div class="filter-swipe" use:swipeFilters>
		<div class="filter-stack">
			{#key filter}
				<div
					class="filter-slide"
					style:transform={filterDragging || filterSettling ? `translateX(${filterDragX}px)` : undefined}
					style:transition={filterDragging || filterSkipAnim
						? "none"
						: filterSettling
							? "transform 180ms cubic-bezier(0.33, 1, 0.68, 1)"
							: "none"}
					in:fly={{
						duration: filterDragging || filterSettling || filterSkipAnim ? 0 : 180,
						easing: cubicOut,
						x: filterDir * 180,
					}}
				>
					{#if loading}
						<div class="ui-card list">
							<LoadingState message="Loading reviewed ships…" />
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
									<a class="row" href={`/review/${encodeURIComponent(item.id)}?from=reviewed`}>
										<span class="thumb">
											{#if item.thumbnailUrl}
												<img alt="" decoding="async" loading="lazy" src={item.thumbnailUrl}>
											{:else}
												<svg
													aria-hidden="true"
													fill="none"
													height="16"
													viewBox="0 0 24 24"
													width="16"
												>
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
													item.decision === "approved"
														? item.hoursApprovedSeconds
														: item.hoursClaimedSeconds,
												)}</strong
											>
											<span>{item.decision === "approved" ? "approved" : "claimed"}</span>
										</span>
										<span class="date" title={formatDateTime(item.decidedAt)}
											>{formatDate(item.decidedAt)}</span
										>
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
				</div>
			{/key}
			{#if (filterDragging || filterSettling) && filterDragName}
				<div
					class="filter-slide filter-slide--adjacent"
					style:transform={filterDragStep > 0
						? `translateX(calc(100% + ${filterSwipeGap}px + ${filterDragX}px))`
						: `translateX(calc(-100% - ${filterSwipeGap}px + ${filterDragX}px))`}
					style:transition={filterDragging || filterSkipAnim
						? "none"
						: filterSettling
							? "transform 180ms cubic-bezier(0.33, 1, 0.68, 1)"
							: "none"}
				>
					<div class="ui-card list filter-adjacent-card">
						<LoadingState message={`Loading ${filterDragName}…`} />
					</div>
				</div>
			{/if}
		</div>
	</div>
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
	flex: 0 1 18.25rem;
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
	width: 3.5rem;
	height: 2.75rem;
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
	flex: 1 1 13.75rem;
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
	gap: 0.25rem;
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
	min-width: 4.75rem;
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
	min-width: 5.75rem;
	font-size: var(--text-xs);
	color: var(--text-3);
	text-align: right;
}

.more {
	display: flex;
	justify-content: center;
	padding-top: var(--space-2);
}

@media (max-width: 47.5rem) {
	.search {
		flex: 1 1 100%;
	}

	.hours {
		align-items: flex-start;
		margin-left: calc(3.5rem + var(--space-3));
	}

	.date {
		min-width: 0;
		margin-left: auto;
	}
}

.filter-swipe {
	min-height: 55dvh;
	touch-action: pan-y;
}

.filter-stack {
	display: grid;
	min-height: inherit;
	overflow: clip;
}

.filter-stack > .filter-slide {
	grid-area: 1 / 1;
	min-width: 0;
}

.filter-adjacent-card {
	pointer-events: none;
}

@media (max-width: 61.25rem) {
	.keys {
		display: none;
	}
}
</style>
