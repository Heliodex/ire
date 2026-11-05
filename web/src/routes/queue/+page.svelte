<script lang="ts">
import { onMount, tick } from "svelte"
import { SvelteMap } from "svelte/reactivity"
import { browser } from "$app/env"
import { goto } from "$app/navigation"
import { claimShip, fetchMe, fetchQueue, program, type Queue, type QueueItem } from "../../lib/api"
import { ApiError } from "../../lib/api-error"
import { checkGroupLabel, checkStatusLabel, checkTone, groupChecks } from "../../lib/checks"
import Avatar from "../../lib/components/Avatar.svelte"
import CheckIcon from "../../lib/components/CheckIcon.svelte"
import LoadingState from "../../lib/components/LoadingState.svelte"
import { formatDateTime, formatDuration, humanStatus, statusTone, timeAgo } from "../../lib/format"
import { session } from "../../lib/session.svelte.js"

type Filter = "all" | "fast" | "attention" | "mine"

type SortKey = "ship" | "maker" | "status" | "type" | "version" | "claimed" | "age" | "checks"

const sortKeys: SortKey[] = ["ship", "maker", "status", "type", "version", "claimed", "age", "checks"]

interface SavedQueueState {
	filter: Filter
	queue: Queue
	savedAt: number
	scrollY: number
	search: string
	selectedId: string
	sortDir: 1 | -1
	sortKey: SortKey | null
	stage: number
	version: number
}

const maxCheckDots = 11

// wideChecks renders every check dot without the +N badge at the widest layout, where the checks column has room. Narrower layouts keep eliding.
let wideChecks = $state(false)
const queueStateKey = `review:queue:${program}`
const queueStateVersion = 1

// isFilter narrows a cached filter value.
function isFilter(value: string): value is Filter {
	return value === "all" || value === "fast" || value === "attention" || value === "mine"
}

// readQueueState restores the last visit from session storage, ignoring stale or malformed copies.
function readQueueState(): SavedQueueState | null {
	try {
		const raw = sessionStorage.getItem(queueStateKey)
		if (!raw) return null

		const state = JSON.parse(raw) as SavedQueueState
		if (state.version !== queueStateVersion) return null
		if (!isFilter(state.filter)) return null
		if (state.stage !== 1 && state.stage !== 2) return null
		if (!state.queue || !Array.isArray(state.queue.items) || typeof state.queue.counts !== "object") return null
		if (typeof state.search !== "string" || typeof state.scrollY !== "number") return null
		if (typeof state.savedAt !== "number" || Date.now() - state.savedAt > 24 * 60 * 60 * 1000) return null

		return state
	} catch {
		return null
	}
}

const emptyQueue: Queue = {
	counts: { checking: 0, claimed: 0, needsAttention: 0, ready: 0, stage2Hold: 0 },
	hasMore: false,
	items: [],
}
const savedQueue = browser ? readQueueState() : null

let queue = $state<Queue>(savedQueue?.queue ?? emptyQueue)
let filter = $state<Filter>(savedQueue?.filter ?? "all")
let stage = $state(savedQueue?.stage ?? 1)
let loading = $state(!savedQueue)
let refreshing = $state(false)
let opening = $state("")
let error = $state("")
let actionError = $state("")
let selected = $state(
	savedQueue
		? Math.max(
				0,
				savedQueue.queue.items.findIndex(item => item.id === savedQueue.selectedId),
			)
		: 0,
)
let search = $state(savedQueue?.search ?? "")
let sortKey = $state<SortKey | null>(
	savedQueue && sortKeys.includes(savedQueue.sortKey as SortKey) ? (savedQueue.sortKey as SortKey) : null,
)
let sortDir = $state<1 | -1>(savedQueue?.sortDir === -1 ? -1 : 1)
let filterTabs = $state<HTMLDivElement | null>(null)
let listElement = $state<HTMLDivElement | null>(null)
let loadingMore = $state(false)
let checkTip = $state<CheckTip | null>(null)
let spaceHeld = $state(false)
let restoring = savedQueue !== null

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

if (savedQueue)
	cache.set(
		`${savedQueue.stage}:${savedQueue.filter}:${savedQueue.sortKey ?? ""}:${savedQueue.sortDir ?? 1}`,
		savedQueue.queue,
	)

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

// toggleSort cycles a column through ascending, descending and unsorted, reloading every page server-side so appended rows keep arriving at the end.
function toggleSort(key: SortKey): void {
	if (sortKey !== key) {
		sortKey = key
		sortDir = 1
	} else if (sortDir === 1) sortDir = -1
	else {
		sortKey = null
		sortDir = 1
	}

	selected = 0
	void load(filter, stage)
}

// sortLabel describes what activating a column header will do next.
function sortLabel(key: SortKey, name: string): string {
	if (sortKey !== key) return `Sort by ${name}`
	if (sortDir === 1) return `Sorted by ${name}, ascending. Activate to sort descending.`
	return `Sorted by ${name}, descending. Activate to clear sorting.`
}

// previewItem is the selected ship shown large while the spacebar is held.
const previewItem = $derived(spaceHeld ? (visible[selected] ?? null) : null)

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

// saveQueueState keeps the loaded rows, filter and scroll position for reloads and returns from a review.
function saveQueueState(): void {
	if (restoring) return

	try {
		const state: SavedQueueState = {
			filter,
			queue,
			savedAt: Date.now(),
			scrollY: window.scrollY,
			search,
			selectedId: visible[selected]?.id ?? "",
			sortDir,
			sortKey,
			stage,
			version: queueStateVersion,
		}

		sessionStorage.setItem(queueStateKey, JSON.stringify(state))
	} catch {
		// Storage can be unavailable or full; the queue still works without a cache.
	}
}

let saveTimer: ReturnType<typeof setTimeout> | null = null

// scheduleSave batches scroll and data updates into one storage write.
function scheduleSave(): void {
	if (restoring || saveTimer) return

	saveTimer = setTimeout((): void => {
		saveTimer = null
		saveQueueState()
	}, 200)
}

// restorePosition scrolls back after the router and browser have settled on the new page.
async function restorePosition(top: number): Promise<void> {
	restoring = true
	await tick()

	window.scrollTo(0, top)
	requestAnimationFrame((): void => {
		window.scrollTo(0, top)
		setTimeout((): void => {
			window.scrollTo(0, top)
			restoring = false
			scheduleSave()
		}, 80)
	})
}

function onQueueScroll(): void {
	hideCheckTip()
	scheduleSave()
}

$effect((): void => {
	void queue
	void filter
	void stage
	void search
	void sortKey
	void sortDir
	scheduleSave()
})

$effect((): void => {
	const next =
		selected >= 0 && visible[selected] && isSelectable(visible[selected])
			? selected
			: visible.findIndex(isSelectable)

	if (next !== selected) selected = next
})

onMount((): (() => void) => {
	const wideQuery = window.matchMedia("(min-width: 1280px)")
	wideChecks = wideQuery.matches

	function onWideChange(event: MediaQueryListEvent): void {
		wideChecks = event.matches
	}

	wideQuery.addEventListener("change", onWideChange)

	if (savedQueue) void restorePosition(savedQueue.scrollY)
	void boot()

	function onPageHide(): void {
		saveQueueState()
	}

	function onVisibilityChange(): void {
		if (document.visibilityState === "hidden") saveQueueState()
	}

	window.addEventListener("pagehide", onPageHide)
	document.addEventListener("visibilitychange", onVisibilityChange)

	const timer = setInterval((): void => void load(filter, stage, true), 10000)

	// Storage writes publish a compact event, so the queue refetches immediately instead of waiting for the poll.
	let pending: ReturnType<typeof setTimeout> | null = null
	let stream: EventSource | null = null

	function onStorageEvent(): void {
		if (pending) return

		pending = setTimeout((): void => {
			pending = null
			void load(filter, stage, true)
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

		if (event.key === " ") {
			// Holding space previews the selected cover image; releasing it dismisses the overlay.
			event.preventDefault()

			if (visible[selected]) {
				hideCheckTip()
				spaceHeld = true
			}

			return
		}

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

	function onKeyup(event: KeyboardEvent): void {
		if (event.key !== " ") return

		// Releasing space dismisses the preview and swallows the keypress so a focused row does not activate.
		event.preventDefault()
		spaceHeld = false
	}

	window.addEventListener("keyup", onKeyup)

	function onBlur(): void {
		spaceHeld = false
	}

	window.addEventListener("blur", onBlur)

	return (): void => {
		clearInterval(timer)
		stream?.close()
		if (pending) clearTimeout(pending)
		if (saveTimer) {
			clearTimeout(saveTimer)
			saveTimer = null
		}
		window.removeEventListener("keydown", onKeydown)
		window.removeEventListener("keyup", onKeyup)
		window.removeEventListener("blur", onBlur)
		window.removeEventListener("pagehide", onPageHide)
		document.removeEventListener("visibilitychange", onVisibilityChange)
		wideQuery.removeEventListener("change", onWideChange)
	}
})

async function boot(): Promise<void> {
	try {
		session.me ??= await fetchMe()
	} catch {
		void goto("/")
		return
	}

	if (stage === 2 && !canSecondPass) {
		stage = 1
		filter = "all"
		queue = emptyQueue
		loading = true
		await load(filter, stage, false)
		return
	}

	await load(filter, stage, savedQueue !== null)

	if (savedQueue) await restorePosition(savedQueue.scrollY)
}

// cacheKey scopes cached rows by stage, filter and sort, so a sorted view never shows rows fetched for another order.
function cacheKey(targetStage: number, targetFilter: Filter): string {
	return `${targetStage}:${targetFilter}:${sortKey ?? ""}:${sortDir}`
}

// checkTone maps a check result to a dot tone. A failure with warning severity stays orange, matching the review screen.
async function load(target = filter, targetStage = stage, keepRows = false): Promise<void> {
	const key = cacheKey(targetStage, target)
	const wantSort = sortKey
	const wantDir = sortDir
	const limit = keepRows ? Math.min(Math.max(queue.items.length, 50), 100) : 50

	try {
		const next = await fetchQueue(target, targetStage, 0, limit, wantSort, wantDir)
		if (target !== filter || targetStage !== stage || wantSort !== sortKey || wantDir !== sortDir) return

		// Keep rows loaded past the refresh limit so polling cannot shrink the list.
		if (keepRows && queue.items.length > next.items.length) {
			const seen = new Set(next.items.map(item => item.id))
			next.items = [...next.items, ...queue.items.slice(next.items.length).filter(item => !seen.has(item.id))]
		}

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
		const next = await fetchQueue(filter, stage, queue.items.length, 50, sortKey, sortDir)

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

// autoLoad fills the next pages while the sentinel is near the viewport.
function autoLoad(node: HTMLElement): { destroy(): void } {
	async function fill(): Promise<void> {
		while (queue.hasMore && !loadingMore) {
			if (node.getBoundingClientRect().top > window.innerHeight + 400) return

			const before = queue.items.length
			await loadMore()
			await tick()

			if (queue.items.length === before) return
		}
	}

	const observer = new IntersectionObserver(
		entries => {
			if (entries.some(entry => entry.isIntersecting)) void fill()
		},
		{ rootMargin: "0px 0px 400px 0px" },
	)

	observer.observe(node)

	return { destroy: () => observer.disconnect() }
}

// refreshCache keeps the cache in step with the rows currently visible.
function refreshCache(): void {
	cache.set(cacheKey(stage, filter), queue)
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

	const cached = cache.get(cacheKey(1, next))
	if (cached) {
		// Show the last copy immediately and let load() replace it without a placeholder.
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

	const cached = cache.get(cacheKey(2, "all"))
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

	if (next === -1) {
		if (step < 0 || !queue.hasMore) return

		const before = queue.items.length
		await loadMore()
		await tick()

		if (queue.items.length === before) return

		return moveSelection(step)
	}

	selected = next

	await tick()
	listElement?.querySelector<HTMLElement>('.row[aria-current="true"]')?.scrollIntoView({ block: "nearest" })
}

// ageTone marks ships that have waited longer than the queue target.
function ageTone(ingestedAt: string): string {
	const days = (Date.now() - Date.parse(ingestedAt)) / 86_400_000
	if (days >= 7) return "age--late"
	if (days >= 3) return "age--warn"
	return ""
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

		const index = visible.indexOf(item)
		if (index !== -1) selected = index

		saveQueueState()
		window.location.assign(`/review/${encodeURIComponent(item.id)}`)
	} catch (err) {
		actionError = claimError(err)
		await load(filter, stage, true)
	} finally {
		opening = ""
	}
}
</script>

<svelte:head>
	<title>Queue · Half Life Review</title>
</svelte:head>

<svelte:window onscroll={onQueueScroll} />

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
		<input
			aria-label="Filter open ships"
			class="ui-input"
			placeholder="Filter open ships…"
			type="search"
			bind:value={search}
		>
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
				to open · hold
				<kbd class="ui-kbd">Space</kbd>
				to preview
			</span>
		</span>
	</div>

	{#if loading}
		<div class="ui-card list">
			<LoadingState message="Loading open ships…" />
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
			<div class="column-head">
				<span></span>
				<button
					aria-label={sortLabel("ship", "Ship")}
					class="sort"
					onclick={() => toggleSort("ship")}
					type="button"
				>
					Ship
					{#if sortKey === "ship"}
						<span aria-hidden="true" class="arrow">{sortDir === 1 ? "▲" : "▼"}</span>
					{/if}
				</button>
				<button
					aria-label={sortLabel("maker", "Maker")}
					class="sort"
					onclick={() => toggleSort("maker")}
					type="button"
				>
					Maker
					{#if sortKey === "maker"}
						<span aria-hidden="true" class="arrow">{sortDir === 1 ? "▲" : "▼"}</span>
					{/if}
				</button>
				<button
					aria-label={sortLabel("status", "Status")}
					class="sort"
					onclick={() => toggleSort("status")}
					type="button"
				>
					Status
					{#if sortKey === "status"}
						<span aria-hidden="true" class="arrow">{sortDir === 1 ? "▲" : "▼"}</span>
					{/if}
				</button>
				<button
					aria-label={sortLabel("type", "Type")}
					class="sort"
					onclick={() => toggleSort("type")}
					type="button"
				>
					Type
					{#if sortKey === "type"}
						<span aria-hidden="true" class="arrow">{sortDir === 1 ? "▲" : "▼"}</span>
					{/if}
				</button>
				<button
					aria-label={sortLabel("version", "Version")}
					class="sort"
					onclick={() => toggleSort("version")}
					type="button"
				>
					Ver
					{#if sortKey === "version"}
						<span aria-hidden="true" class="arrow">{sortDir === 1 ? "▲" : "▼"}</span>
					{/if}
				</button>
				<button
					aria-label={sortLabel("claimed", "Claimed hours")}
					class="sort"
					onclick={() => toggleSort("claimed")}
					type="button"
				>
					Claimed
					{#if sortKey === "claimed"}
						<span aria-hidden="true" class="arrow">{sortDir === 1 ? "▲" : "▼"}</span>
					{/if}
				</button>
				<button
					aria-label={sortLabel("age", "Age")}
					class="sort"
					onclick={() => toggleSort("age")}
					type="button"
				>
					Age
					{#if sortKey === "age"}
						<span aria-hidden="true" class="arrow">{sortDir === 1 ? "▲" : "▼"}</span>
					{/if}
				</button>
				<button
					aria-label={sortLabel("checks", "Checks")}
					class="sort"
					onclick={() => toggleSort("checks")}
					type="button"
				>
					Checks
					{#if sortKey === "checks"}
						<span aria-hidden="true" class="arrow">{sortDir === 1 ? "▲" : "▼"}</span>
					{/if}
				</button>
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
						<span class="maker inline-maker">
							<Avatar name={item.makerName} size="xs" url={item.makerAvatarUrl} />
							<span class="maker-name">{item.makerName}</span>
						</span>
						<span class="inline-theme">{item.theme ?? ""}</span>
					</span>
					<span class="badges">
						<span
							class="ui-badge status"
							data-tone={statusTone(item.status)}
							title={item.claimedBy ? `Claimed by ${item.claimedBy}` : humanStatus(item.status)}
						>
							{item.claimedBy === session.me?.user.email ? "Yours" : humanStatus(item.status)}
						</span>
						{#if item.previousReviewer}
							<span class="ui-badge previous" title={`Previously reviewed by ${item.previousReviewer}`}>
								{item.previousReviewer === session.me?.user.email
									? "Yours to continue"
									: "Resubmission"}
							</span>
						{/if}
						<span class="theme">{item.theme ?? ""}</span>
					</span>
					<div class="facts">
						<span class="maker fact-maker">
							<Avatar name={item.makerName} size="xs" url={item.makerAvatarUrl} />
							<span class="maker-name">{item.makerName}</span>
						</span>
						<span class="version">v{item.version}</span>
						<span class="timing">
							<span class="hours">{formatDuration(item.hoursClaimedSeconds)} claimed</span>
							<span class="age {ageTone(item.ingestedAt)}">{timeAgo(item.ingestedAt)}</span>
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
								{#each groupChecks(
									wideChecks ? item.checks : item.checks.slice(0, maxCheckDots),
								) as group (group.group)}
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
								{#if !wideChecks && item.checks.length > maxCheckDots}
									<span
										aria-hidden="true"
										class="dot-more"
										title={`${item.checks.length - maxCheckDots} more checks`}
									>
										+{item.checks.length - maxCheckDots}
									</span>
								{/if}
							</span>
						{/if}
					</span>
				</button>
			{/each}
		</div>
		{#if queue.hasMore}
			<div aria-live="polite" class="more" use:autoLoad>
				{#if loadingMore}
					<span>Loading more ships…</span>
				{/if}
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

{#if previewItem}
	<div aria-hidden="true" class="preview-veil">
		{#if previewItem.thumbnailUrl}
			<img alt="" draggable="false" src={previewItem.thumbnailUrl}>
		{:else}
			<span class="preview-empty">No project image</span>
		{/if}
	</div>
{/if}

<style>
.more {
	display: flex;
	align-items: center;
	justify-content: center;
	min-height: 2.5rem;
	padding-top: var(--space-2);
	font-size: var(--text-sm);
	color: var(--text-3);
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
	position: relative;
	display: grid;
	grid-template-columns: 64px minmax(0, 1fr) minmax(110px, 160px) 96px 64px 28px 136px 64px minmax(140px, 190px);
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
	grid-template-columns: 64px minmax(0, 1fr) minmax(110px, 160px) 96px 64px 28px 136px 64px minmax(140px, 190px);
	gap: var(--space-3);
	align-items: center;
	padding: var(--space-2) var(--space-4) var(--space-2) var(--space-3);
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-3);
	background: var(--surface-2);
	border-bottom: 1px solid var(--border);
}

.column-head .sort {
	min-width: 0;
	padding: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	font: inherit;
	color: inherit;
	text-align: left;
	white-space: nowrap;
	cursor: pointer;
	background: none;
	border: 0;
}

.column-head .sort:hover {
	color: var(--text);
}

.column-head .sort .arrow {
	padding-left: 4px;
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

.row.selected::before {
	position: absolute;
	inset: 0 auto 0 0;
	z-index: 1;
	width: 3px;
	content: "";
	background: var(--primary);
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

.maker {
	display: inline-flex;
	gap: var(--space-1);
	align-items: center;
	min-width: 0;
}

.maker-name {
	overflow: hidden;
	text-overflow: ellipsis;
}

.inline-maker {
	display: none;
	padding-left: var(--space-2);
	font-weight: 400;
}

.inline-theme {
	display: none;
	overflow: hidden;
	text-overflow: ellipsis;
	font-size: var(--text-xs);
	font-weight: 400;
	color: var(--text-3);
	white-space: nowrap;
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

.age--warn {
	color: var(--accent-yellow);
}

.age--late {
	color: var(--accent-red);
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
	align-items: center;
	overflow: hidden;
}

.dot-more {
	flex: none;
	font-size: 10px;
	font-weight: 700;
	line-height: 1;
	color: var(--text-3);
	white-space: nowrap;
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

.preview-veil {
	position: fixed;
	inset: 0;
	z-index: var(--layer-toast);
	display: grid;
	place-items: center;
	padding: 6vh 6vw;
	pointer-events: none;
	background: rgba(0, 0, 0, 0.82);
}

.preview-veil img {
	max-width: min(88vw, 1100px);
	max-height: 82vh;
	object-fit: contain;
	border-radius: var(--radius-lg);
	box-shadow: var(--shadow);
}

.preview-empty {
	font-size: var(--text-lg);
	font-weight: 700;
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

@media (min-width: 1280px) {
	.row,
	.column-head {
		grid-template-columns: 64px minmax(0, 1fr) minmax(110px, 190px) 96px 64px 28px 136px 64px minmax(140px, 300px);
	}
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

@media (min-width: 761px) and (max-width: 1024px) {
	.row {
		grid-template-areas:
			"thumb name maker badges"
			"thumb version timing checks";
		grid-template-columns: 64px minmax(0, 15rem) 13rem minmax(190px, 1fr);
	}

	.ship-title {
		display: contents;
	}

	.ship-name {
		grid-area: name;
		overflow: hidden;
		text-overflow: ellipsis;
		font-size: var(--text-md);
		font-weight: 700;
		white-space: nowrap;
	}

	.inline-maker {
		display: flex;
		grid-area: maker;
		padding-left: 0;
	}

	.fact-maker {
		display: none;
	}

	.facts {
		display: contents;
	}

	.badges {
		justify-content: end;
	}

	.version {
		grid-area: version;
		justify-self: start;
		text-align: left;
	}

	.timing {
		grid-area: timing;
		justify-self: start;
		text-align: left;
	}

	.checks {
		grid-area: checks;
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
			"thumb facts facts facts"
			"thumb status checks checks";
		grid-template-columns: 64px 96px 44px minmax(0, 1fr);
		column-gap: var(--space-2);
	}

	.ship-title {
		display: grid;
		grid-template-areas:
			"name name"
			"maker theme";
		grid-template-columns: minmax(0, 1fr) auto;
		gap: 2px var(--space-2);
		align-items: baseline;
	}

	.ship-name {
		grid-area: name;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.inline-maker {
		display: flex;
		grid-area: maker;
		min-width: 0;
		padding-left: 0;
	}

	.inline-theme {
		display: block;
		grid-area: theme;
		text-align: right;
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

	.checks {
		align-items: flex-end;
		justify-self: end;
		text-align: right;
	}

	.badges {
		display: contents;
	}

	.status {
		grid-area: status;
	}

	.badges .theme {
		display: none;
	}

	.timing {
		display: contents;
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
