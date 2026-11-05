<script lang="ts">
import { onMount, tick } from "svelte"
import { SvelteMap } from "svelte/reactivity"
import { goto } from "$app/navigation"
import { claimShip, fetchMe, fetchQueue, program, type Queue, type QueueItem } from "../../lib/api"
import { ApiError } from "../../lib/api-error"
import { checkGroupLabel, checkStatusLabel, checkTone, groupChecks } from "../../lib/checks"
import CheckIcon from "../../lib/components/CheckIcon.svelte"
import { formatDateTime, formatHours, humanStatus, statusTone, timeAgo } from "../../lib/format"
import { session } from "../../lib/session.svelte.js"

type Filter = "all" | "fast" | "attention" | "mine"

let queue = $state<Queue>({
	counts: { checking: 0, claimed: 0, needsAttention: 0, ready: 0, stage2Hold: 0 },
	hasMore: false,
	items: [],
})
let filter = $state<Filter>("all")
let stage = $state(1)
let loading = $state(true)
let refreshing = $state(false)
let opening = $state("")
let error = $state("")
let actionError = $state("")
let selected = $state(0)
let search = $state("")
let filterTabs = $state<HTMLDivElement | null>(null)
let listElement = $state<HTMLDivElement | null>(null)
let loadingMore = $state(false)
let checkTip = $state<CheckTip | null>(null)

interface CheckTip {
	above: boolean
	group: string
	label: string
	status: string
	tone: string
	x: number
	y: number
}

// cache keeps each filter and stage response for this visit, so switching back is instant while a fresh copy loads behind it.
const cache = new SvelteMap<string, Queue>()

const role = $derived(session.me?.programs.find(item => item.programSlug === program)?.role ?? "")
const canSecondPass = $derived(role === "organizer" || role === "admin")
const readOnly = $derived(session.me?.readOnly ?? false)

const filters: { key: Filter; label: string; short: string }[] = [
	{ key: "all", label: "All open", short: "All" },
	{ key: "fast", label: "Fast lane", short: "Fast" },
	{ key: "attention", label: "Needs a look", short: "Attention" },
	{ key: "mine", label: "Mine", short: "Mine" },
]

const visible = $derived(
	queue.items.filter(item => {
		if (search.trim() === "") return true
		const needle = search.trim().toLowerCase()
		return item.title.toLowerCase().includes(needle) || item.makerName.toLowerCase().includes(needle)
	}),
)

// isSelectable keeps ships claimed by another reviewer out of the keyboard cursor.
function isSelectable(item: QueueItem): boolean {
	return readOnly || !item.claimedBy || item.claimedBy === session.me?.user.email
}

// showCheckTip anchors the custom check popover to the hovered square.
function showCheckTip(event: MouseEvent, group: string, check: QueueItem["checks"][number]): void {
	const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
	const center = rect.left + rect.width / 2
	const half = Math.min(132, window.innerWidth / 2 - 8)
	const above = rect.top > 100

	checkTip = {
		above,
		group: checkGroupLabel(group),
		label: check.label,
		status: checkStatusLabel(check.status),
		tone: checkTone(check),
		x: Math.min(window.innerWidth - half - 8, Math.max(half + 8, center)),
		y: above ? rect.top : rect.bottom,
	}
}

function hideCheckTip(): void {
	checkTip = null
}

$effect((): void => {
	const next =
		selected >= 0 && visible[selected] && isSelectable(visible[selected])
			? selected
			: visible.findIndex(isSelectable)

	if (next !== selected) selected = next
})

onMount((): (() => void) => {
	void boot()

	const timer = setInterval((): void => void load(filter, stage, true), 10000)

	// Storage writes publish a compact event, so the queue refetches immediately instead of waiting for the poll.
	let pending: ReturnType<typeof setTimeout> | null = null
	let stream: EventSource | null = null

	function onStorageEvent(): void {
		if (pending) return

		pending = setTimeout((): void => {
			pending = null
			void load()
		}, 250)
	}

	try {
		stream = new EventSource("/v1/events?topic=storage")
		stream.addEventListener("message", onStorageEvent)
	} catch {
		// EventSource is optional; the interval remains the fallback.
	}

	function onKeydown(event: KeyboardEvent): void {
		if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return

		if (event.key === "ArrowDown") {
			event.preventDefault()
			void moveSelection(1)
		} else if (event.key === "ArrowUp") {
			event.preventDefault()
			void moveSelection(-1)
		} else if (event.key === "Enter" && visible[selected] && isSelectable(visible[selected])) {
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
		stream?.close()
		if (pending) clearTimeout(pending)
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
async function load(target = filter, targetStage = stage, keepRows = false): Promise<void> {
	const key = `${targetStage}:${target}`
	const limit = keepRows ? Math.min(Math.max(queue.items.length, 50), 100) : 50

	try {
		const next = await fetchQueue(target, targetStage, 0, limit)
		if (target !== filter || targetStage !== stage) return

		cache.set(key, next)
		queue = next
		selected = Math.min(selected, Math.max(queue.items.length - 1, 0))
		error = ""
	} catch (err) {
		if (err instanceof ApiError && err.code === "unauthorized") {
			void goto("/")
			return
		}
		if (target === filter && targetStage === stage)
			error = err instanceof Error ? err.message : "Could not load the queue."
	} finally {
		if (target === filter && targetStage === stage) {
			loading = false
			refreshing = false
		}
	}
}

// loadMore appends the next page without losing the rows already on screen.
async function loadMore(): Promise<void> {
	if (loadingMore || !queue.hasMore) return

	loadingMore = true
	error = ""

	try {
		const next = await fetchQueue(filter, stage, queue.items.length)

		const seen = new Set(queue.items.map(item => item.id))
		queue = {
			...next,
			items: [...queue.items, ...next.items.filter(item => !seen.has(item.id))],
		}
		refreshCache()
	} catch (err) {
		error = err instanceof Error ? err.message : "Could not load more ships."
	} finally {
		loadingMore = false
	}
}

// refreshCache keeps the cache in step with the rows currently visible.
function refreshCache(): void {
	cache.set(`${stage}:${filter}`, queue)
}

async function choose(next: Filter): Promise<void> {
	stage = 1

	if (next === filter) {
		void load(next, stage)
		return
	}

	filter = next
	selected = 0
	error = ""
	actionError = ""

	const cached = cache.get(`1:${next}`)
	if (cached) {
		// Show the last copy immediately and let load() replace it without a skeleton.
		queue = cached
		refreshing = false
		void load(next, stage)
		return
	}

	// Keep the current rows visible while the first fetch for this filter runs.
	refreshing = true
	await load(next, stage)
}

async function chooseSecondPass(): Promise<void> {
	if (!canSecondPass) return

	const cached = cache.get("2:all")
	stage = 2
	filter = "all"
	selected = 0
	error = ""
	actionError = ""

	if (cached) {
		queue = cached
		refreshing = false
		void load(filter, stage)
		return
	}

	refreshing = true
	await load(filter, stage)
}

// moveFilter selects the next filter or the second-pass tab, wrapping at the ends.
function moveFilter(step: number): void {
	const count = filters.length + (canSecondPass ? 1 : 0)
	const current = stage === 2 ? filters.length : filters.findIndex(option => option.key === filter)
	const next = (current + step + count) % count

	filterTabs?.querySelectorAll<HTMLButtonElement>("button")[next]?.focus()

	if (next === filters.length) {
		void chooseSecondPass()
		return
	}

	void choose(filters[next].key)
}

// moveSelection moves the row cursor and scrolls the new row into view when the list is taller than the viewport.
async function moveSelection(step: number): Promise<void> {
	const start = selected === -1 ? (step > 0 ? -1 : visible.length) : selected
	let next = -1

	for (let index = start + step; index >= 0 && index < visible.length; index += step) {
		if (!isSelectable(visible[index])) continue

		next = index
		break
	}

	if (next === -1) return

	selected = next

	await tick()
	listElement?.querySelector<HTMLElement>('.row[aria-current="true"]')?.scrollIntoView({ block: "nearest" })
}

// claimError makes a claim conflict readable and formats the expiry from the API.
function claimError(err: unknown): string {
	if (!(err instanceof ApiError) || err.code !== "claim_taken")
		return err instanceof Error ? err.message : "Could not claim this ship."

	const until = /until (\S+)$/.exec(err.message)?.[1]

	return until
		? `Another reviewer has already claimed this ship until ${formatDateTime(until)}.`
		: "Another reviewer has already claimed this ship."
}

async function open(item: QueueItem): Promise<void> {
	opening = item.id
	actionError = ""
	try {
		if (!session.me?.readOnly && (!item.claimedBy || item.claimedBy !== session.me?.user.email))
			await claimShip(item.id)
		window.location.assign(`/review/${encodeURIComponent(item.id)}`)
	} catch (err) {
		actionError = claimError(err)
		await load()
	} finally {
		opening = ""
	}
}
</script>

<svelte:head>
	<title>Queue · Half Life Review</title>
</svelte:head>

<svelte:window onscroll={hideCheckTip} />

<section class="page-head">
	<div>
		<h1>Queue</h1>
		<p>Stage 1 · first pass</p>
	</div>

	<div class="stats ui-card">
		{#if queue.counts.checking > 0}
			<div><strong>{queue.counts.checking}</strong><span>Checking</span></div>
		{/if}
		<div><strong>{queue.counts.ready}</strong><span>Ready</span></div>
		<div><strong>{queue.counts.claimed}</strong><span>In review</span></div>
		<div><strong>{queue.counts.needsAttention}</strong><span>Needs a look</span></div>
		<div><strong>{queue.counts.stage2Hold}</strong><span>Second pass</span></div>
	</div>
</section>

<div class="toolbar">
	<div aria-label="Filters and stages" class="ui-seg filters" role="tablist" bind:this={filterTabs}>
		{#each filters as option (option.key)}
			<button
				aria-label={option.label}
				aria-selected={stage === 1 && filter === option.key}
				onclick={(): Promise<void> => choose(option.key)}
				role="tab"
				type="button"
				class:active={stage === 1 && filter === option.key}
			>
				<span class="label-full">{option.label}</span>
				<span class="label-short">{option.short}</span>
			</button>
		{/each}
		{#if canSecondPass}
			<button
				aria-label={`Second pass ${queue.counts.stage2Hold}`}
				aria-selected={stage === 2}
				onclick={chooseSecondPass}
				role="tab"
				type="button"
				class:active={stage === 2}
			>
				<span class="label-full">Second pass {queue.counts.stage2Hold}</span>
				<span class="label-short">Pass 2 · {queue.counts.stage2Hold}</span>
			</button>
		{/if}
	</div>
	<label class="search">
		<span class="visually-hidden">Filter open ships</span>
		<input class="ui-input" placeholder="Filter open ships…" type="search" bind:value={search}>
	</label>
</div>

{#if actionError || error}
	<div class="ui-notice error-notice" data-tone="bad" role="alert">
		<span>{actionError || error}</span>
		{#if actionError}
			<button
				class="dismiss"
				onclick={(): void => {
					actionError = ""
				}}
				type="button"
			>
				Dismiss
			</button>
		{/if}
	</div>
{/if}

<section aria-busy={loading || refreshing} aria-labelledby="list-title">
	<div class="list-head">
		<h2 id="list-title">{stage === 2 ? "Second pass" : "Open ships"}</h2>
		<span>
			{visible.length} {visible.length === 1 ? "ship" : "ships"}
			<span class="keys">
				· <kbd class="ui-kbd">↑</kbd><kbd class="ui-kbd">↓</kbd>
				to move ·
				<kbd class="ui-kbd">←</kbd><kbd class="ui-kbd">→</kbd>
				filters ·
				<kbd class="ui-kbd">Enter</kbd>
				to open
			</span>
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
			<strong
				>{search
					? "No ships match that search."
					: stage === 2
						? "No held ships."
						: "Nothing in this filter."}</strong
			>
			<span
				>{stage === 2
					? "Nothing is waiting for a second pass."
					: filter === "attention"
						? "The checks found nothing blocking."
						: "Check back after the next ingest."}</span
			>
		</div>
	{:else}
		<div class="ui-card list" bind:this={listElement} class:refreshing>
			<div aria-hidden="true" class="column-head">
				<span></span>
				<span>Ship</span>
				<span>Maker</span>
				<span>Status</span>
				<span>Type</span>
				<span>Ver</span>
				<span>Claimed</span>
				<span>Age</span>
				<span>Checks</span>
			</div>
			{#each visible as item, index (item.id)}
				<button
					aria-current={index === selected}
					class="row"
					disabled={opening !== "" || !isSelectable(item)}
					onclick={(): Promise<void> => open(item)}
					type="button"
					class:selected={index === selected}
					class:unavailable={!isSelectable(item)}
				>
					<span class="thumb">
						<svg aria-hidden="true" fill="none" height="16" viewBox="0 0 24 24" width="16">
							<rect height="16" rx="2" stroke="currentColor" stroke-width="1.6" width="18" x="3" y="4" />
							<circle cx="8.5" cy="9.5" fill="currentColor" r="1.5" />
							<path
								d="m4 17 4.5-4.5 3 3L15 12l5 5"
								stroke="currentColor"
								stroke-linecap="round"
								stroke-linejoin="round"
								stroke-width="1.6"
							/>
						</svg>
						{#if item.thumbnailUrl}
							<img
								alt=""
								decoding="async"
								loading="lazy"
								onerror={(event): void => {
									;(event.currentTarget as HTMLImageElement).style.display = "none"
								}}
								src={item.thumbnailUrl}
							>
						{/if}
					</span>
					<span class="ship-title" title={item.title}>
						<span class="ship-name">{item.title}</span>
						<span class="maker inline-maker">{item.makerName}</span>
					</span>
					<span class="badges">
						<span
							class="ui-badge status"
							data-tone={statusTone(item.status)}
							title={item.claimedBy ? `Claimed by ${item.claimedBy}` : humanStatus(item.status)}
						>
							{item.claimedBy === session.me?.user.email ? "Yours" : humanStatus(item.status)}
						</span>
						<span class="theme">{item.theme ?? ""}</span>
					</span>
					<div class="facts">
						<span class="maker fact-maker">{item.makerName}</span>
						<span class="version">v{item.version}</span>
						<span class="timing">
							<span class="hours">{formatHours(item.hoursClaimedSeconds)} claimed</span>
							<span class="age">{timeAgo(item.ingestedAt)}</span>
						</span>
					</div>
					<span class="checks">
						<span class="counts">
							{#if item.status === "checking"}
								<span class="pending-text">Checks running…</span>
							{:else}
								{#if item.checkSummary.pass > 0}
									<span class="pass">{item.checkSummary.pass} pass</span>
								{/if}
								{#if item.checkSummary.warn > 0}
									<span class="warn">{item.checkSummary.warn} warn</span>
								{/if}
								{#if item.checkSummary.fail > 0}
									<span class="fail">{item.checkSummary.fail} fail</span>
								{/if}
								{#if item.checkSummary.error > 0}
									<span class="fail">{item.checkSummary.error} error</span>
								{/if}
								{#if item.checkSummary.overall === "pass"}
									<span class="fast">Fast lane</span>
								{/if}
							{/if}
						</span>
						{#if item.checks.length > 0 && item.status !== "checking"}
							<span
								aria-label={item.checks.map(check => `${check.label}: ${check.status}`).join(", ")}
								class="dots"
								role="img"
							>
								{#each groupChecks(item.checks) as group (group.group)}
									<span class="dot-group">
										{#each group.items as check (check.key)}
											<span
												aria-hidden="true"
												class="dot"
												data-tone={checkTone(check)}
												onmouseenter={(event): void => showCheckTip(event, group.group, check)}
												onmouseleave={hideCheckTip}
											>
												<CheckIcon status={check.status} />
											</span>
										{/each}
									</span>
								{/each}
							</span>
						{/if}
					</span>
				</button>
			{/each}
		</div>
		{#if queue.hasMore}
			<div class="more">
				<button
					class="ui-button"
					data-variant="outline"
					disabled={loadingMore}
					onclick={loadMore}
					type="button"
				>
					{loadingMore ? "Loading…" : "Load more ships"}
				</button>
			</div>
		{/if}
	{/if}
</section>

{#if checkTip}
	<div
		class="check-tip"
		role="tooltip"
		style={`left:${checkTip.x}px; top:${checkTip.y}px`}
		class:above={checkTip.above}
		class:below={!checkTip.above}
	>
		<div class="tip-head">
			<span class="tip-group">{checkTip.group}</span>
			<span class="ui-badge" data-tone={checkTip.tone}>{checkTip.status}</span>
		</div>
		<span class="tip-label">{checkTip.label}</span>
	</div>
{/if}

<style>
.more {
	display: flex;
	justify-content: center;
	padding-top: var(--space-2);
}

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

.filters {
	flex-wrap: wrap;
	border-radius: var(--radius-lg);
}

.label-short {
	display: none;
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

.error-notice {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
	justify-content: space-between;
	margin-bottom: var(--space-3);
}

.dismiss {
	padding: 0;
	font-size: var(--text-xs);
	font-weight: 700;
	color: inherit;
	text-decoration: underline;
	cursor: pointer;
	background: none;
	border: 0;
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
	grid-template-columns: 64px minmax(0, 1fr) minmax(110px, 160px) 96px 64px 36px 96px 64px minmax(140px, 190px);
	gap: var(--space-3);
	align-items: center;
	width: 100%;
	padding: var(--space-1) var(--space-4) var(--space-1) var(--space-3);
	text-align: left;
	background: transparent;
	border: 0;
	border-bottom: 1px solid var(--border);
}

.column-head {
	display: grid;
	grid-template-columns: 64px minmax(0, 1fr) minmax(110px, 160px) 96px 64px 36px 96px 64px minmax(140px, 190px);
	gap: var(--space-3);
	align-items: center;
	padding: var(--space-2) var(--space-4) var(--space-2) var(--space-3);
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-3);
	background: var(--surface-2);
	border-bottom: 1px solid var(--border);
}

.column-head span:first-child {
	min-width: 0;
}

.thumb {
	position: relative;
	display: grid;
	place-items: center;
	align-self: stretch;
	width: 64px;
	margin: calc(-1 * var(--space-1)) 0 calc(-1 * var(--space-1)) calc(-1 * var(--space-3));
	overflow: hidden;
	background: var(--surface-2);
}

.thumb img {
	position: absolute;
	inset: 0;
	display: block;
	width: 100%;
	height: 100%;
	object-fit: cover;
}

.thumb svg {
	display: block;
	color: var(--text-3);
}

.row:last-child {
	border-bottom: 0;
}

.row:hover:not(:disabled) {
	background: var(--surface-2);
}

.row.selected {
	background: var(--surface-3);
}

.row:disabled {
	cursor: not-allowed;
}

.row:not(.unavailable):disabled {
	cursor: wait;
}

.row.unavailable {
	opacity: 0.6;
}

.ship-title {
	overflow: hidden;
	text-overflow: ellipsis;
	font-size: var(--text-md);
	font-weight: 700;
	white-space: nowrap;
}

.status {
	justify-self: start;
}

.badges {
	display: contents;
}

.theme {
	overflow: hidden;
	text-overflow: ellipsis;
	font-size: var(--text-xs);
	color: var(--text-3);
	white-space: nowrap;
}

.facts {
	display: contents;
}

.timing {
	display: contents;
}

.maker,
.hours {
	overflow: hidden;
	text-overflow: ellipsis;
	font-size: var(--text-sm);
	color: var(--text-2);
	white-space: nowrap;
}

.inline-maker {
	display: none;
	padding-left: var(--space-2);
	font-weight: 400;
}

.version,
.age {
	font-size: var(--text-xs);
	font-variant-numeric: tabular-nums;
	color: var(--text-2);
}

.version {
	text-align: right;
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
	font-weight: 600;
	color: var(--text-3);
}

.dots {
	display: flex;
	flex-wrap: nowrap;
	gap: 3px;
	overflow: hidden;
}

.dot-group {
	display: flex;
	gap: 1px;
}

.dot-group + .dot-group {
	padding-left: 4px;
	border-left: 1px solid var(--border-2);
}

.dot {
	display: grid;
	flex: none;
	place-items: center;
	width: 11px;
	height: 11px;
	color: var(--text-2);
	background: var(--border-2);
	border-radius: 2px;
}

.dot[data-tone="approved"],
.dot[data-tone="pending"],
.dot[data-tone="changes"],
.dot[data-tone="rejected"] {
	color: var(--on-ok);
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

.check-tip {
	position: fixed;
	z-index: var(--layer-toast);
	display: flex;
	flex-direction: column;
	gap: 3px;
	width: max-content;
	max-width: 264px;
	padding: var(--space-2) var(--space-3);
	pointer-events: none;
	background: var(--surface);
	border: 1px solid var(--border-2);
	border-radius: var(--radius-md);
	box-shadow: var(--shadow);
}

.check-tip.above {
	transform: translate(-50%, calc(-100% - 6px));
}

.check-tip.below {
	transform: translate(-50%, 6px);
}

.tip-head {
	display: flex;
	gap: var(--space-3);
	align-items: center;
	justify-content: space-between;
}

.tip-group {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-3);
	text-transform: uppercase;
	letter-spacing: 0.04em;
}

.tip-label {
	font-size: var(--text-sm);
	font-weight: 600;
	line-height: 1.35;
	color: var(--text);
}

@media (min-width: 761px) {
	.checks {
		align-items: flex-end;
	}
}

@media (min-width: 1025px) {
	.ship-title {
		grid-row: 1;
		grid-column: 2;
	}

	.maker {
		grid-row: 1;
		grid-column: 3;
	}

	.status {
		grid-row: 1;
		grid-column: 4;
	}

	.theme {
		grid-row: 1;
		grid-column: 5;
	}

	.version {
		grid-row: 1;
		grid-column: 6;
	}

	.hours {
		grid-row: 1;
		grid-column: 7;
	}

	.age {
		grid-row: 1;
		grid-column: 8;
	}

	.checks {
		grid-row: 1;
		grid-column: 9;
	}
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
	white-space: nowrap;
	clip: rect(0 0 0 0);
}

@media (max-width: 1024px) {
	.column-head {
		display: none;
	}

	.dot {
		width: 9px;
		height: 9px;
	}

	.row {
		grid-template-areas:
			"thumb title badges checks"
			"thumb facts facts facts";
		grid-template-columns: 64px minmax(0, 1fr) 170px 160px;
		row-gap: var(--space-1);
		column-gap: var(--space-3);
	}

	.thumb {
		grid-area: thumb;
		align-self: stretch;
	}

	.ship-title {
		grid-area: title;
	}

	.badges {
		display: grid;
		grid-area: badges;
		grid-template-columns: 96px 64px;
		gap: var(--space-2);
		align-items: center;
	}

	.checks {
		grid-area: checks;
	}

	.facts {
		display: grid;
		grid-area: facts;
		grid-template-columns: minmax(120px, 1fr) 36px 150px;
		gap: var(--space-2);
		align-items: center;
	}

	.timing {
		display: flex;
		gap: var(--space-3);
		align-items: baseline;
		justify-self: end;
		text-align: right;
	}
}

@media (max-width: 980px) {
	.keys {
		display: none;
	}
}

@media (max-width: 760px) {
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

	.dot {
		width: 8px;
		height: 8px;
	}

	.row {
		grid-template-areas:
			"thumb title title title"
			"thumb status theme checks"
			"facts facts facts facts";
		grid-template-columns: 64px 96px 44px minmax(0, 1fr);
		column-gap: var(--space-2);
	}

	.ship-title {
		display: grid;
		grid-template-columns: minmax(0, 10rem) minmax(0, 1fr);
		gap: var(--space-2);
		align-items: baseline;
	}

	.ship-name {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.inline-maker {
		display: block;
		padding-left: 0;
	}

	.fact-maker {
		display: none;
	}

	.facts {
		display: flex;
		gap: var(--space-3);
		align-items: flex-start;
		justify-content: flex-end;
	}

	.badges {
		display: contents;
	}

	.status {
		grid-area: status;
	}

	.theme {
		grid-area: theme;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.timing {
		flex-direction: column;
		gap: 0;
	}
}

@media (max-width: 640px) {
	.label-full {
		display: none;
	}

	.label-short {
		display: inline;
	}

	.filters button {
		height: 32px;
	}

	.stats {
		overflow-x: auto;
		overflow-y: hidden;
		scrollbar-width: none;
	}

	.stats::-webkit-scrollbar {
		display: none;
	}

	.stats div {
		flex: 0 0 88px;
		min-width: 0;
		padding: var(--space-1) var(--space-2);
	}
}

@media (max-width: 400px) {
	.dots {
		display: none;
	}
}

@media (max-width: 370px) {
	.counts {
		flex-wrap: wrap;
		gap: 3px;
	}

	.checks .fast {
		display: none;
	}
}
</style>
