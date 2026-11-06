<script lang="ts">
import { onMount, tick } from "svelte"
import { SvelteMap, SvelteSet } from "svelte/reactivity"
import { browser } from "$app/env"
import { goto } from "$app/navigation"
import { page } from "$app/state"
import { claimShip, fetchBundle, fetchMe, fetchQueue, program, type Queue, type QueueItem } from "../../lib/api"
import { ApiError } from "../../lib/api-error"
import { createBundleCache } from "../../lib/bundle-cache"
import {
	type CheckDotMode,
	checkGroupLabel,
	checkOverflow,
	checkStatusLabel,
	checkTone,
	groupChecks,
	visibleChecks,
} from "../../lib/checks"
import Avatar from "../../lib/components/Avatar.svelte"
import CheckIcon from "../../lib/components/CheckIcon.svelte"
import LoadingState from "../../lib/components/LoadingState.svelte"
import { formatDateTime, formatDuration, humanStatus, statusTone, timeAgo } from "../../lib/format"
import { hasPermission, permissionSecondPass, programPermissions } from "../../lib/permissions"
import {
	hasStructuredQuery,
	matchesQueueQuery,
	parseQueueQuery,
	queueFilterSegments,
	setQueueFilterToken,
} from "../../lib/queue-filter"
import {
	createQueueState,
	type QueueFilter as Filter,
	type QueueState,
	type SavedQueueState,
	type QueueSortKey as SortKey,
} from "../../lib/queue-state"
import { streamQueue } from "../../lib/queue-stream"
import { rememberReviewPreview } from "../../lib/review-preview"
import { makerHref } from "../../lib/reviewer"
import { session } from "../../lib/session.svelte.js"
import { rowWindow, windowPads } from "../../lib/virtual-list"

const sortKeys: SortKey[] = ["ship", "maker", "status", "type", "version", "claimed", "age", "checks"]

// virtualizeAfter mounts every row below this count; longer lists render only the viewport window.
const virtualizeAfter = 80
// rowOverscan renders this far beyond the viewport so fast scrolling never shows a gap.
const rowOverscan = 800
// fallbackRowHeight estimates unmeasured rows until the running mean takes over.
const fallbackRowHeight = 48

// wideChecks renders up to twenty check dots at the wide layout, where the checks column has room. Narrower layouts keep eliding at eleven.
let wideChecks = $state(false)
// fullChecks renders every dot above 87.5rem, where the checks column expands to fit them all.
let fullChecks = $state(false)
const dotMode = $derived<CheckDotMode>(fullChecks ? "full" : wideChecks ? "wide" : "compact")

// queueState persists the visit in tab storage. It is built per call so sessionStorage is only touched on the client.
function queueState(): QueueState {
	return createQueueState(program, sessionStorage)
}

const emptyQueue: Queue = {
	counts: { checking: 0, claimed: 0, fast: 0, mine: 0, needsAttention: 0, ready: 0, stage2Hold: 0 },
	hasMore: false,
	items: [],
}
const savedQueue = browser ? queueState().read() : null

const filters: { key: Exclude<Filter, "attention">; label: string; short: string }[] = [
	{ key: "all", label: "All open", short: "All" },
	{ key: "fast", label: "Fast lane", short: "Fast" },
	{ key: "mine", label: "Mine", short: "Mine" },
]

// tabCount totals the server badges behind one filter tab.
function tabCount(key: Exclude<Filter, "attention">): number {
	const { counts } = queue

	return {
		all: counts.checking + counts.ready + counts.claimed + counts.needsAttention,
		fast: counts.fast,
		mine: counts.mine,
	}[key]
}

let queue = $state<Queue>(savedQueue?.queue ?? emptyQueue)
// A stored visit can still name the retired attention tab; fall back to the full list.
let filter = $state<Filter>(
	savedQueue && filters.some(option => option.key === savedQueue.filter) ? savedQueue.filter : "all",
)
let stage = $state(savedQueue?.stage ?? 1)
// A restored visit paints its cached rows at once; only a first visit waits on the loader.
let loading = $state(savedQueue === null)
// streaming is true while the first page arrives as NDJSON; load-more waits until the stream has finished.
let streaming = $state(false)
let streamController: AbortController | null = null
let opening = $state("")
let hoveredId = $state("")
let prefetchTimer: ReturnType<typeof setTimeout> | null = null
const prefetched = new SvelteSet<string>()
const prefetching = new SvelteSet<string>()
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
const urlSearch = searchFromURL()
let search = $state(urlSearch !== "" ? urlSearch : (savedQueue?.search ?? ""))
let sortKey = $state<SortKey | null>(
	savedQueue && sortKeys.includes(savedQueue.sortKey as SortKey) ? (savedQueue.sortKey as SortKey) : null,
)
let sortDir = $state<1 | -1>(savedQueue?.sortDir === -1 ? -1 : 1)
let filterTabs = $state<HTMLDivElement | null>(null)
let listElement = $state<HTMLDivElement | null>(null)
// measuredHeights remembers one rendered row height per ship, so heights survive filtering.
let measuredHeights = new SvelteMap<string, number>()
// listTop tracks the list's viewport offset so the render window follows scrolling.
let listTop = $state(0)
let viewportHeight = $state(0)
let scrollQueued = false
let loadingMore = $state(false)
let checkTip = $state<CheckTip | null>(null)
let spaceHeld = $state(false)
let restoring = savedQueue !== null
let filterHelp = $state<HTMLDialogElement | null>(null)
let filterInput = $state<HTMLInputElement | null>(null)
let filterHighlight = $state<HTMLSpanElement | null>(null)

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

const permissions = $derived(programPermissions(session.me, program))
const canSecondPass = $derived(hasPermission(permissions, permissionSecondPass))
const readOnly = $derived(session.me?.readOnly ?? false)

const queueQuery = $derived(parseQueueQuery(search))
const filterSegments = $derived(queueFilterSegments(search))
const visible = $derived(queue.items.filter(item => matchesQueueQuery(item, queueQuery)))

// estimatedHeight averages the measured rows so unmeasured ones start close to reality.
const estimatedHeight = $derived.by(() => {
	let sum = 0
	let count = 0
	for (const height of measuredHeights.values()) {
		sum += height
		count += 1
	}

	return count === 0 ? fallbackRowHeight : sum / count
})

// rowOffsets holds one top offset per visible row plus the total height, from measured rows or the running estimate.
const rowOffsets = $derived.by(() => {
	const offsets: number[] = Array.from({ length: visible.length + 1 }, () => 0)
	let top = 0
	for (const [index, item] of visible.entries()) {
		offsets[index] = top
		top += measuredHeights.get(item.id) ?? estimatedHeight
	}
	offsets[visible.length] = top

	return offsets
})

// windowedRows mounts only the rows around the viewport, or everything when the list is short.
const windowedRows = $derived.by(() => {
	if (visible.length <= virtualizeAfter) return { bottomPad: 0, rows: visible, start: 0, topPad: 0 }

	const range = rowWindow(rowOffsets, -listTop, viewportHeight, rowOverscan)
	const pads = windowPads(rowOffsets, range)

	return { bottomPad: pads.bottom, rows: visible.slice(range.start, range.end), start: range.start, topPad: pads.top }
})

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

	queueState().write({
		filter,
		queue,
		scrollY: window.scrollY,
		search,
		selectedId: visible[selected]?.id ?? "",
		sortDir,
		sortKey,
		stage,
	})
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
	if (scrollQueued) return

	scrollQueued = true
	requestAnimationFrame((): void => {
		scrollQueued = false
		if (listElement) listTop = listElement.getBoundingClientRect().top
	})
}

// measureRow records one rendered row height, keyed by ship so filtering keeps it.
function measureRow(node: HTMLElement, id: string): { destroy(): void } {
	function record(): void {
		const height = node.offsetHeight
		if (height > 0 && measuredHeights.get(id) !== height) measuredHeights.set(id, height)
	}
	record()
	const observer = new ResizeObserver(record)
	observer.observe(node)

	return {
		destroy: (): void => observer.disconnect(),
	}
}

// revealSelected centres the cursor row in the viewport. Scroll clamping at the page ends lets
// the selection settle towards the bottom only once there is no more room to centre it.
function revealSelected(): void {
	if (!listElement || selected < 0 || selected >= visible.length) return

	const top = listElement.getBoundingClientRect().top + rowOffsets[selected]
	const height = measuredHeights.get(visible[selected].id) ?? estimatedHeight
	const delta = top + height / 2 - viewportHeight / 2

	if (Math.abs(delta) < 1) return

	window.scrollBy({ top: delta })
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
	const wideQuery = window.matchMedia("(min-width: 80rem)")
	wideChecks = wideQuery.matches

	function onWideChange(event: MediaQueryListEvent): void {
		wideChecks = event.matches
	}

	wideQuery.addEventListener("change", onWideChange)

	const fullQuery = window.matchMedia("(min-width: 87.5rem)")
	fullChecks = fullQuery.matches

	function onFullChange(event: MediaQueryListEvent): void {
		fullChecks = event.matches
	}

	fullQuery.addEventListener("change", onFullChange)

	viewportHeight = window.innerHeight

	function onResize(): void {
		viewportHeight = window.innerHeight
	}

	window.addEventListener("resize", onResize)

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
		cancelStream()
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
		fullQuery.removeEventListener("change", onFullChange)
		window.removeEventListener("resize", onResize)
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

	if (savedQueue) {
		// The cached rows are already on screen and positioned, so refresh them in the background.
		void loadStreaming()
		await restorePosition(savedQueue.scrollY)

		return
	}

	await loadStreaming()
}

// cacheKey scopes cached rows by stage, filter and sort, so a sorted view never shows rows fetched for another order.
function cacheKey(targetStage: number, targetFilter: Filter): string {
	return `${targetStage}:${targetFilter}:${sortKey ?? ""}:${sortDir}`
}

// cancelStream stops an in-flight first page when the view changes underneath it.
function cancelStream(): void {
	streamController?.abort()
	streamController = null
	streaming = false
}

// loadStreaming fills the first page from the NDJSON endpoint, so each batch of ships paints as it arrives.
async function loadStreaming(): Promise<void> {
	const target = filter
	const targetStage = stage
	const wantSort = sortKey
	const wantDir = sortDir
	const key = cacheKey(targetStage, target)
	const previous = queue.items
	const cached = previous.length > 0
	const limit = Math.min(Math.max(previous.length, 50), 100)
	const items: QueueItem[] = []
	let { counts } = queue
	let hasMore = false
	let expected = 0
	let first = true
	let stale = false

	cancelStream()
	const controller = new AbortController()
	streamController = controller
	streaming = true

	function current(): boolean {
		return target === filter && targetStage === stage && wantSort === sortKey && wantDir === sortDir
	}

	try {
		await streamQueue(
			target,
			targetStage,
			limit,
			wantSort,
			wantDir,
			{
				onCounts: value => {
					if (!current()) return

					counts = value
					queue = { ...queue, counts }
				},
				onMeta: (value, total) => {
					hasMore = value
					expected = total
				},
				onShips: batch => {
					if (!current()) {
						stale = true
						controller.abort()

						return
					}

					items.push(...batch)

					// Cached rows stay on screen until the refresh is complete, so the list never shrinks to a batch.
					if (cached) return

					queue = { counts, hasMore, items: [...items] }

					if (first) {
						first = false
						loading = false
					}
				},
			},
			controller.signal,
		)

		if (stale || !current()) return

		if (items.length < expected) {
			// The stream ended before its announced page, so fetch the rest the ordinary way.
			await load(target, targetStage, true)

			return
		}

		// Keep rows loaded past the streamed page so re-entering the page cannot shrink the list.
		if (previous.length > items.length) {
			const seen = new Set(items.map(item => item.id))
			items.push(...previous.slice(items.length).filter(item => !seen.has(item.id)))
		}

		queue = { counts, hasMore, items }
		cache.set(key, queue)
		selected = Math.min(selected, Math.max(queue.items.length - 1, 0))
		error = ""
	} catch (err) {
		if (controller.signal.aborted) return
		if (err instanceof ApiError && err.code === "unauthorized") {
			void goto("/")
			return
		}

		// A broken stream leaves the page on the rows already painted; fetch the whole page instead.
		await load(target, targetStage, true)
	} finally {
		if (streamController === controller) {
			streamController = null
			streaming = false
		}

		if (!controller.signal.aborted && current()) loading = false
	}
}

// prefetchBundle keeps a ship's full bundle in the tab cache so opening it paints immediately.
async function prefetchBundle(id: string): Promise<void> {
	if (prefetched.has(id) || prefetching.has(id) || !session.me) return

	prefetching.add(id)

	try {
		const bundle = await fetchBundle(id)
		createBundleCache(program, session.me.user.email, sessionStorage).write(id, bundle)
		prefetched.add(id)
	} catch {
		// Prefetching is best effort; opening the ship still loads it normally.
	} finally {
		prefetching.delete(id)
	}
}

// schedulePrefetch warms the hovered or selected ship after a short pause, so a quick pass over the list does not
// fetch every row.
function schedulePrefetch(item: QueueItem | undefined): void {
	if (prefetchTimer) {
		clearTimeout(prefetchTimer)
		prefetchTimer = null
	}

	if (!item || !isSelectable(item) || opening !== "") return

	prefetchTimer = setTimeout((): void => {
		prefetchTimer = null
		void prefetchBundle(item.id)
	}, 200)
}

// prefetchItem is the row the cursor is on: the hovered row wins, otherwise the keyboard selection.
const prefetchItem = $derived(hoveredId !== "" ? visible.find(item => item.id === hoveredId) : visible[selected])

$effect((): (() => void) => {
	schedulePrefetch(prefetchItem)

	return (): void => {
		if (prefetchTimer) {
			clearTimeout(prefetchTimer)
			prefetchTimer = null
		}
	}
})

// checkTone maps a check result to a dot tone. A failure with warning severity stays orange, matching the review screen.
async function load(target = filter, targetStage = stage, keepRows = false): Promise<void> {
	cancelStream()

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
		if (target === filter && targetStage === stage) loading = false
	}
}

// loadMore appends the next page without losing the rows already on screen.
async function loadMore(): Promise<void> {
	if (loadingMore || !queue.hasMore || streaming) return

	const targetFilter = filter
	const targetStage = stage
	const wantSort = sortKey
	const wantDir = sortDir
	const targetQueue = queue
	const offset = queue.items.length

	loadingMore = true
	error = ""

	try {
		const next = await fetchQueue(targetFilter, targetStage, offset, 50, wantSort, wantDir)

		// A tab, sort or queue change while this page was in flight makes the result stale.
		if (targetFilter !== filter || targetStage !== stage || wantSort !== sortKey || wantDir !== sortDir) return
		if (queue !== targetQueue) return

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

// A structured filter with no rows in the loaded pages keeps pulling pages until it finds a match
// or runs out of queue. The debounce stops half-typed filters from walking the whole queue.
$effect((): (() => void) | undefined => {
	if (!hasStructuredQuery(queueQuery)) return
	if (
		visible.length > 0
		|| loading
		|| loadingMore
		|| streaming
		|| !queue.hasMore
		|| error !== ""
		|| actionError !== ""
	)
		return

	const timer = setTimeout((): void => void loadMore(), 250)

	return (): void => clearTimeout(timer)
})

// autoLoad fills the next pages while the sentinel is near the viewport.
function autoLoad(node: HTMLElement): { destroy(): void } {
	async function fill(): Promise<void> {
		if (streaming) return

		const targetFilter = filter
		const targetStage = stage
		const wantSort = sortKey
		const wantDir = sortDir

		while (queue.hasMore && !loadingMore) {
			if (!node.isConnected) return
			if (targetFilter !== filter || targetStage !== stage || wantSort !== sortKey || wantDir !== sortDir) return
			if (node.getBoundingClientRect().top > window.innerHeight + 400) return

			const before = queue.items.length
			await loadMore()
			await tick()

			if (targetFilter !== filter || targetStage !== stage || wantSort !== sortKey || wantDir !== sortDir) return
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
	if (next === filter && stage === 1) {
		void load(next, 1, true)
		return
	}

	stage = 1
	filter = next
	selected = 0
	error = ""
	actionError = ""

	const cached = cache.get(cacheKey(1, next))
	if (cached) {
		// Show the last copy immediately and let load() refresh it without shrinking the list.
		queue = cached
		loading = false
		void load(next, 1, true)
		return
	}

	// Drop the previous filter's rows so they never appear under this tab while its first fetch runs.
	queue = { ...queue, hasMore: false, items: [] }
	loading = true
	await load(next, 1)
}

async function chooseSecondPass(): Promise<void> {
	if (!canSecondPass) return

	const keeping = stage === 2 && filter === "all"
	const cached = cache.get(cacheKey(2, "all"))
	stage = 2
	filter = "all"
	selected = 0
	error = ""
	actionError = ""

	if (cached) {
		queue = cached
		loading = false
		void load(filter, stage, true)
		return
	}

	if (keeping) {
		void load(filter, stage, true)
		return
	}

	// Drop the previous tab's rows so they never appear under second pass while its first fetch runs.
	queue = { ...queue, hasMore: false, items: [] }
	loading = true
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
	revealSelected()
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

async function open(item: QueueItem, tab: string | null = null): Promise<void> {
	opening = item.id
	actionError = ""
	try {
		if (!session.me?.readOnly && (!item.claimedBy || item.claimedBy !== session.me?.user.email))
			await claimShip(item.id)

		const index = visible.indexOf(item)
		if (index !== -1) selected = index

		saveQueueState()
		rememberReviewPreview({
			bomTotalCents: item.bomTotalCents,
			checkCount: item.checks.length,
			checks: item.checks,
			claimedBy: item.claimedBy,
			hoursClaimedSeconds: item.hoursClaimedSeconds,
			hoursHackatimeSeconds: item.hoursHackatimeSeconds,
			hoursJournalSeconds: item.hoursJournalSeconds,
			id: item.id,
			makerEmail: item.makerEmail,
			makerName: item.makerName,
			makerSlackId: item.makerSlackId,
			requestedTier: item.requestedTier,
			status: item.status,
			thumbnailUrl: item.thumbnailUrl,
			title: item.title,
			version: item.version,
			week: item.week,
		})

		let target = `/review/${encodeURIComponent(item.id)}`
		if (tab) target += `?tab=${tab}`

		await goto(target)
	} catch (err) {
		actionError = claimError(err)
		await load(filter, stage, true)
	} finally {
		opening = ""
	}
}

// openChecks claims the ship like a normal open, then lands on its checks tab.
function openChecks(item: QueueItem, event?: Event): void {
	event?.stopPropagation()
	event?.preventDefault()
	void open(item, "checks")
}

// searchFromURL restores a shared filter, e.g. from a status badge link. It stays empty during prerendering, which has no query string.
function searchFromURL(): string {
	if (!browser) return ""

	return page.url.searchParams.get("search") ?? ""
}

// filterHref is the shareable URL for one key:value filter.
function filterHref(key: string, value: string | number): string {
	const token = `${key}:${value}`

	return `/queue?search=${encodeURIComponent(token)}`
}

// syncFilterScroll keeps the highlighted copy under the input when long filters scroll sideways.
function syncFilterScroll(): void {
	if (filterInput && filterHighlight) filterHighlight.scrollLeft = filterInput.scrollLeft
}

$effect((): void => {
	void search
	syncFilterScroll()
})

// setSearchValue writes the filter box, resets the cursor and keeps the URL shareable.
function setSearchValue(value: string): void {
	search = value.trim()
	selected = 0
	error = ""
	actionError = ""

	const url = new URL(page.url.href)
	if (search === "") url.searchParams.delete("search")
	else url.searchParams.set("search", search)
	history.replaceState(history.state, "", url)
}

// setFilterToken adds or replaces one key:value filter without dropping the other terms.
function setFilterToken(key: string, value: string): void {
	setSearchValue(setQueueFilterToken(search, key, value))
}

// filterByStatus adds status:ready and its sibling filters when a status badge is activated.
function filterByStatus(item: QueueItem, event: MouseEvent): void {
	event.preventDefault()
	event.stopPropagation()
	setFilterToken("status", item.status)
}

// filterByTheme adds type:cad and its sibling filters when a type label is activated.
function filterByTheme(item: QueueItem, event: MouseEvent): void {
	const theme = item.theme?.trim() ?? ""
	if (theme === "") return

	event.preventDefault()
	event.stopPropagation()
	setFilterToken("type", theme)
}

// filterByVersion adds ver:2 when the version label is activated.
function filterByVersion(item: QueueItem, event: MouseEvent): void {
	event.preventDefault()
	event.stopPropagation()
	setFilterToken("ver", String(item.version))
}

// filterByCheck adds check:repo.readme=warn when one check dot is activated.
function filterByCheck(check: QueueItem["checks"][number], event: MouseEvent): void {
	event.preventDefault()
	event.stopPropagation()
	setFilterToken("check", `${check.key}=${check.status}`)
}

// compactDuration renders a filter-friendly duration such as 6h24m or 3d1h0m.
function compactDuration(seconds: number): string {
	const total = Math.max(0, Math.round(seconds))
	const days = Math.floor(total / 86_400)
	const hours = Math.floor((total % 86_400) / 3_600)
	const minutes = Math.floor((total % 3_600) / 60)
	const secs = total % 60
	let out = ""

	if (days > 0) out += `${days}d`
	if (hours > 0 || days > 0) out += `${hours}h`
	if (minutes > 0 || hours > 0 || days > 0) out += `${minutes}m`

	if (secs === 0 && out !== "") return out

	return `${out}${secs}s`
}

// timeFilter asks for ships with more claimed time than this one.
function timeFilter(item: QueueItem): string {
	return `>${compactDuration(item.hoursClaimedSeconds)}`
}

// ageFilter asks for ships at least as old as this one, using its exact age rather than the rounded label.
function ageFilter(item: QueueItem): string {
	const ingested = Date.parse(item.ingestedAt)
	if (Number.isNaN(ingested)) return ">=0s"

	return `>=${compactDuration((Date.now() - ingested) / 1000)}`
}

// filterByTime adds time:>6h when the claimed hours are activated.
function filterByTime(item: QueueItem, event: MouseEvent): void {
	event.preventDefault()
	event.stopPropagation()
	setFilterToken("time", timeFilter(item))
}

// filterByAge adds age:>=2h when the age is activated.
function filterByAge(item: QueueItem, event: MouseEvent): void {
	event.preventDefault()
	event.stopPropagation()
	setFilterToken("age", ageFilter(item))
}
</script>

<svelte:head>
	<title>Queue · Ire</title>
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
		<div><strong>{queue.counts.needsAttention}</strong><span>Needs attention</span></div>
		<div><strong>{queue.counts.stage2Hold}</strong><span>Second pass</span></div>
	</div>
</section>

<div class="toolbar">
	<div aria-label="Filters and stages" class="ui-seg filters" role="tablist" bind:this={filterTabs}>
		{#each filters as option (option.key)}
			<button
				aria-label={`${option.label}, ${tabCount(option.key)} ships`}
				aria-selected={stage === 1 && filter === option.key}
				onclick={(): Promise<void> => choose(option.key)}
				role="tab"
				type="button"
				class:active={stage === 1 && filter === option.key}
			>
				<span class="label-full">{option.label} <span class="tab-count">{tabCount(option.key)}</span></span>
				<span class="label-short">{option.short} <span class="tab-count">{tabCount(option.key)}</span></span>
			</button>
		{/each}
		{#if canSecondPass}
			<button
				aria-label={`Second pass, ${queue.counts.stage2Hold} ships`}
				aria-selected={stage === 2}
				onclick={chooseSecondPass}
				role="tab"
				type="button"
				class:active={stage === 2}
			>
				<span class="label-full">Second pass <span class="tab-count">{queue.counts.stage2Hold}</span></span>
				<span class="label-short">Pass 2 <span class="tab-count">{queue.counts.stage2Hold}</span></span>
			</button>
		{/if}
	</div>
	<div class="filter-box">
		<button
			aria-haspopup="dialog"
			aria-label="Filter syntax help"
			class="ui-icon-button help"
			onclick={(): void => filterHelp?.showModal()}
			title="Filter syntax help"
			type="button"
		>
			?
		</button>
		<label class="search">
			<span class="visually-hidden">Filter open ships</span>
			<span class="filter-field">
				<span aria-hidden="true" class="highlight" bind:this={filterHighlight}>
					<span class="highlight-text">
						{#each filterSegments as segment, index (`${index}:${segment.text}`)}
							{#if segment.filter}
								<span class="filter-key">{segment.key}</span><span class="filter-colon">:</span
								><span class="filter-value">{segment.value}</span>
							{:else}
								{segment.text}
							{/if}
						{/each}
					</span>
				</span>
				<input
					aria-label="Filter open ships by text or week, checks, type, status, age, claimed time and version"
					class="ui-input"
					oninput={(): void => {
						error = ""
						actionError = ""
						requestAnimationFrame(syncFilterScroll)
					}}
					onscroll={syncFilterScroll}
					placeholder="Filter ships (type:cad, week:1…)"
					title="Try week:1, type:cad, status:ready, checks:warn, age:>1d, time:>6h or ver:2"
					type="search"
					bind:this={filterInput}
					bind:value={search}
				>
			</span>
		</label>
	</div>
</div>

<dialog
	aria-labelledby="filter-help-title"
	class="filter-help"
	onclick={(event): void => {
		// A modal dialog retargets clicks on its backdrop to the dialog element itself.
		if (event.target === filterHelp) filterHelp?.close()
	}}
	onkeydown={(event): void => {
		if (event.key === "Escape") filterHelp?.close()
	}}
	bind:this={filterHelp}
>
	<div class="filter-help-body">
		<header>
			<h2 id="filter-help-title">Filter syntax</h2>
			<button
				aria-label="Close filter help"
				class="ui-icon-button"
				onclick={(): void => filterHelp?.close()}
				type="button"
			>
				×
			</button>
		</header>

		<p>Combine filters with spaces. Plain terms still search the ship title, maker, status and type.</p>

		<dl class="syntax">
			<div>
				<dt><code>week:1</code></dt>
				<dd>Week 1 ships. Compare with <code>week:&gt;1</code>.</dd>
			</div>
			<div>
				<dt><code>type:cad</code></dt>
				<dd>One project type. Clicking a type label adds this.</dd>
			</div>
			<div>
				<dt><code>status:ready</code></dt>
				<dd>One status, such as <code>ready</code>, <code>claimed</code> or <code>needs_attention</code>.</dd>
			</div>
			<div>
				<dt><code>checks:warn</code></dt>
				<dd>Any warning. Counts compare with <code>checks:warn&lt;3</code> or <code>checks:&gt;=10</code>.</dd>
			</div>
			<div>
				<dt><code>check:repo.readme=warn</code></dt>
				<dd>One check with that status. Clicking a check dot adds this.</dd>
			</div>
			<div>
				<dt><code>age:&gt;1d</code></dt>
				<dd>
					Older than a duration. Units are <code>s</code>, <code>m</code>, <code>h</code>, <code>d</code> and
					<code>w</code>.
				</dd>
			</div>
			<div>
				<dt><code>time:&gt;6h</code></dt>
				<dd>More than this much claimed time.</dd>
			</div>
			<div>
				<dt><code>ver:2</code> / <code>ver:&gt;5</code></dt>
				<dd>Exact version or a version comparison.</dd>
			</div>
		</dl>

		<p class="hint">
			Status, type, version, claimed time and age labels in the queue also add their filter when activated.
		</p>
	</div>
</dialog>

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

<section aria-busy={loading} aria-labelledby="list-title">
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
		{#if queue.hasMore && hasStructuredQuery(queueQuery)}
			<div class="ui-card list">
				<LoadingState message="Searching all ships…" />
			</div>
		{:else}
			<div class="ui-card ui-empty">
				<strong
					>{search
						? "No ships match that search."
						: stage === 2
							? "No held ships."
							: "Nothing in this filter."}</strong
				>
				<span
					>{stage === 2 ? "Nothing is waiting for a second pass." : "Check back after the next ingest."}</span
				>
			</div>
		{/if}
	{:else}
		<div class="ui-card list" bind:this={listElement}>
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
			{#if windowedRows.topPad > 0}
				<div aria-hidden="true" class="vspacer" style:height={`${windowedRows.topPad}px`}></div>
			{/if}
			{#each windowedRows.rows as item, index (item.id)}
				<button
					aria-current={windowedRows.start + index === selected}
					class="row"
					disabled={opening !== "" || !isSelectable(item)}
					onclick={(): Promise<void> => open(item)}
					onmouseenter={(): void => {
						hoveredId = item.id
					}}
					onmouseleave={(): void => {
						if (hoveredId === item.id) hoveredId = ""
					}}
					type="button"
					class:selected={windowedRows.start + index === selected}
					class:unavailable={!isSelectable(item)}
					use:measureRow={item.id}
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
							<a
								class="maker-name"
								data-sveltekit-reload
								href={makerHref(item.makerEmail) ?? "#"}
								onclick={(event: MouseEvent): void => event.stopPropagation()}
							>
								{item.makerName}
							</a>
						</span>
						<span class="inline-theme">
							{#if item.theme}
								<a
									aria-label={`Show only ${item.theme} ships`}
									class="inline-theme-link"
									href={filterHref("type", item.theme)}
									onclick={(event: MouseEvent): void => filterByTheme(item, event)}
									title={`Show only ${item.theme} ships`}
								>
									{item.theme}
								</a>
							{/if}
						</span>
					</span>
					<span class="badges">
						<a
							aria-label={`Show only ${humanStatus(item.status)} ships`}
							class="ui-badge status"
							data-tone={statusTone(item.status)}
							href={filterHref("status", item.status)}
							onclick={(event: MouseEvent): void => filterByStatus(item, event)}
							title={item.claimedBy ? `Claimed by ${item.claimedBy}` : humanStatus(item.status)}
						>
							{item.claimedBy === session.me?.user.email ? "Yours" : humanStatus(item.status)}
						</a>
						{#if item.previousReviewer}
							<span class="ui-badge previous" title={`Previously reviewed by ${item.previousReviewer}`}>
								{item.previousReviewer === session.me?.user.email
									? "Yours to continue"
									: "Resubmission"}
							</span>
						{/if}
						{#if item.theme}
							<a
								aria-label={`Show only ${item.theme} ships`}
								class="theme"
								href={filterHref("type", item.theme)}
								onclick={(event: MouseEvent): void => filterByTheme(item, event)}
								title={`Show only ${item.theme} ships`}
							>
								{item.theme}
							</a>
						{/if}
					</span>
					<div class="facts">
						<span class="maker fact-maker">
							<Avatar name={item.makerName} size="xs" url={item.makerAvatarUrl} />
							<a
								class="maker-name"
								data-sveltekit-reload
								href={makerHref(item.makerEmail) ?? "#"}
								onclick={(event: MouseEvent): void => event.stopPropagation()}
							>
								{item.makerName}
							</a>
						</span>
						<a
							aria-label={`Show only version ${item.version} ships`}
							class="version"
							href={filterHref("ver", item.version)}
							onclick={(event: MouseEvent): void => filterByVersion(item, event)}
							title={`Show only version ${item.version} ships`}
						>
							v{item.version}
						</a>
						<span class="timing">
							<a
								aria-label={`Show ships with more than ${formatDuration(item.hoursClaimedSeconds)} claimed`}
								class="hours"
								href={filterHref("time", timeFilter(item))}
								onclick={(event: MouseEvent): void => filterByTime(item, event)}
								title={`Show ships with more than ${formatDuration(item.hoursClaimedSeconds)} claimed`}
							>
								{formatDuration(item.hoursClaimedSeconds)}
								claimed
							</a>
							<a
								aria-label="Show ships at least as old as this one"
								class="age {ageTone(item.ingestedAt)}"
								href={filterHref("age", ageFilter(item))}
								onclick={(event: MouseEvent): void => filterByAge(item, event)}
								title="Show ships at least as old as this one"
							>
								{timeAgo(item.ingestedAt)}
							</a>
						</span>
					</div>
					<span class="checks">
						<a
							class="checks-open"
							href={`/review/${encodeURIComponent(item.id)}?tab=checks`}
							onclick={(event: MouseEvent): void => openChecks(item, event)}
						>
							<span class="visually-hidden">Open {item.title} on its checks tab</span>
						</a>
						<span aria-hidden="true" class="counts">
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
							<span class="dots">
								{#each groupChecks(visibleChecks(item.checks, dotMode)) as group (group.group)}
									<span class="dot-group">
										{#each group.items as check (check.key)}
											<a
												aria-label={`Filter ${check.label}: ${checkStatusLabel(check.status)}`}
												class="dot"
												data-tone={checkTone(check)}
												href={filterHref("check", `${check.key}=${check.status}`)}
												onclick={(event: MouseEvent): void => filterByCheck(check, event)}
												onmouseenter={(event): void => showCheckTip(event, group.group, check)}
												onmouseleave={hideCheckTip}
												title={`Filter ${check.label}: ${checkStatusLabel(check.status)}`}
											>
												<CheckIcon status={check.status} />
											</a>
										{/each}
									</span>
								{/each}
								{#if checkOverflow(item.checks, dotMode) > 0}
									<span
										aria-hidden="true"
										class="dot-more"
										title={`${checkOverflow(item.checks, dotMode)} more checks`}
									>
										+{checkOverflow(item.checks, dotMode)}
									</span>
								{/if}
							</span>
						{/if}
					</span>
				</button>
			{/each}
			{#if windowedRows.bottomPad > 0}
				<div aria-hidden="true" class="vspacer" style:height={`${windowedRows.bottomPad}px`}></div>
			{/if}
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
	min-width: 5.25rem;
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

.filter-box {
	display: flex;
	flex: 1 1 16.25rem;
	gap: var(--space-2);
	align-items: center;
	max-width: 26rem;
}

.help {
	flex: none;
	font-size: var(--text-sm);
	font-weight: 800;
	background: var(--surface);
	border: 1px solid var(--border-2);
}

.search {
	flex: 1 1 auto;
	min-width: 0;
}

.filter-field {
	position: relative;
	display: block;
	width: 100%;
	height: var(--control-md);
	background: var(--input);
	border: 1px solid var(--border-2);
	border-radius: var(--radius-sm);
}

.filter-field:focus-within {
	border-color: var(--primary);
}

.filter-field input {
	position: relative;
	z-index: 1;
	height: 100%;
	padding: 0 var(--space-3);
	color: transparent;
	letter-spacing: inherit;
	caret-color: var(--text);
	background: transparent;
	border: 0;
	border-radius: inherit;
}

.filter-field input::placeholder {
	color: var(--text-3);
}

.filter-field input::selection {
	color: var(--text);
	background: var(--primary-soft);
}

.filter-field input::-webkit-search-cancel-button {
	cursor: pointer;
}

.highlight {
	position: absolute;
	inset: 0;
	display: flex;
	align-items: center;
	padding: 0 var(--space-3);
	overflow: hidden;
	color: var(--text);
	white-space: pre;
	pointer-events: none;
	user-select: none;
}

.highlight-text {
	flex: none;
	white-space: pre;
}

.filter-key {
	color: var(--primary);
}

.filter-colon {
	color: var(--text);
}

.filter-value {
	color: var(--accent-blue);
}

.filter-help {
	width: min(34rem, calc(100vw - 2rem));
	max-height: calc(100vh - 2rem);
	padding: 0;
	color: var(--text);
	background: var(--surface);
	border: 1px solid var(--border);
	border-radius: var(--radius-lg);
	box-shadow: var(--shadow-lg);
}

.filter-help::backdrop {
	background: rgba(0, 0, 0, 0.55);
	backdrop-filter: blur(2px);
}

.filter-help-body {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	max-height: calc(100vh - 2rem);
	padding: var(--space-4);
	overflow-y: auto;
}

.filter-help header {
	display: flex;
	gap: var(--space-2);
	align-items: center;
	justify-content: space-between;
}

.filter-help h2 {
	font-size: var(--text-xl);
}

.filter-help p {
	font-size: var(--text-sm);
	color: var(--text-2);
}

.filter-help .syntax {
	display: grid;
	gap: var(--space-2);
	padding: 0;
	margin: 0;
}

.filter-help .syntax div {
	display: grid;
	grid-template-columns: 9.5rem minmax(0, 1fr);
	gap: var(--space-2);
	padding-top: var(--space-2);
	border-top: 1px solid var(--border);
}

.filter-help .syntax dt {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text);
}

.filter-help .syntax dd {
	margin: 0;
	font-size: var(--text-xs);
	line-height: 1.5;
	color: var(--text-2);
}

.filter-help code {
	padding: 0.1rem 0.3rem;
	font-family: var(--font-mono);
	font-size: 0.92em;
	color: var(--text);
	background: var(--surface-2);
	border-radius: 0.25rem;
}

.filter-help .hint {
	font-size: var(--text-xs);
	color: var(--text-3);
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
}

.row {
	position: relative;
	display: grid;
	grid-template-columns: 4rem minmax(0, 1fr) minmax(7rem, 10rem) 6rem 4rem 1.75rem 8.5rem 4rem minmax(8.75rem, 12rem);
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
	grid-template-columns: 4rem minmax(0, 1fr) minmax(7rem, 10rem) 6rem 4rem 1.75rem 8.5rem 4rem minmax(8.75rem, 12rem);
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
	padding-left: 0.25rem;
}

.column-head span:first-child {
	min-width: 0;
}

.thumb {
	position: relative;
	display: grid;
	place-items: center;
	align-self: stretch;
	width: 4rem;
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
	width: 0.25rem;
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

a.theme,
.inline-theme-link {
	color: var(--text-3);
	text-decoration: none;
}

a.theme:hover,
.inline-theme-link:hover {
	color: var(--text);
	text-decoration: underline;
}

a.version {
	text-decoration: none;
}

a.version:hover {
	color: var(--text);
	text-decoration: underline;
}

a.hours,
a.age {
	text-decoration: none;
}

a.hours:hover,
a.age:hover {
	color: var(--text);
	text-decoration: underline;
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

a.maker-name {
	color: inherit;
	text-decoration: none;
}

a.maker-name:hover {
	text-decoration: underline;
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
	position: relative;
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	min-width: 0;
	padding: 0.25rem 0.5rem;
	margin: -0.25rem -0.5rem;
	font-size: var(--text-xs);
	font-weight: 700;
	border-radius: var(--radius-sm);
}

.checks:hover {
	background: color-mix(in srgb, var(--text) 8%, transparent);
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
	gap: 0.25rem;
	align-items: center;
	overflow: hidden;
}

.checks-open {
	position: absolute;
	inset: 0;
	z-index: 0;
	cursor: pointer;
	border-radius: var(--radius-sm);
}

.counts,
.dots {
	position: relative;
	z-index: 1;
	pointer-events: none;
}

a.dot,
a.status {
	text-decoration: none;
	cursor: pointer;
}

a.dot {
	pointer-events: auto;
}

a.checks-open:focus-visible,
a.dot:focus-visible,
a.status:focus-visible {
	outline: 2px solid var(--accent-blue);
	outline-offset: 2px;
}

.tab-count {
	font-weight: 600;
	color: var(--text-3);
}

.dot-more {
	flex: none;
	font-size: 0.75rem;
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
	padding-left: 0.25rem;
	border-left: 1px solid var(--border-2);
}

.dot {
	display: grid;
	flex: none;
	place-items: center;
	width: 0.75rem;
	height: 0.75rem;
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
	gap: 0.25rem;
	width: max-content;
	max-width: 16.5rem;
	padding: var(--space-2) var(--space-3);
	pointer-events: none;
	background: var(--surface);
	border: 1px solid var(--border-2);
	border-radius: var(--radius-md);
	box-shadow: var(--shadow);
}

.check-tip.above {
	transform: translate(-50%, calc(-100% - 0.5rem));
}

.check-tip.below {
	transform: translate(-50%, 0.5rem);
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
	max-width: min(88vw, 68.75rem);
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

@media (min-width: 47.56rem) {
	.checks {
		align-items: flex-end;
	}
}

@media (min-width: 64.06rem) {
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

@media (min-width: 80rem) {
	.row,
	.column-head {
		grid-template-columns: 4rem minmax(0, 1fr) minmax(7rem, 12rem) 6rem 4rem 1.75rem 8.5rem 4rem minmax(
				8.75rem,
				18.75rem
			);
	}
}

@media (min-width: 87.5rem) {
	.row,
	.column-head {
		grid-template-columns: 4rem minmax(0, 1fr) minmax(7rem, 12rem) 6rem 4rem 1.75rem 8.5rem 4rem minmax(
				12.5rem,
				30rem
			);
	}
}

@media (max-width: 64rem) {
	.column-head {
		display: none;
	}

	.dot {
		width: 0.5rem;
		height: 0.5rem;
	}

	.row {
		grid-template-areas:
			"thumb title badges checks"
			"thumb facts facts facts";
		grid-template-columns: 4rem minmax(0, 1fr) 10.75rem 10rem;
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
		grid-template-columns: 6rem 4rem;
		gap: var(--space-2);
		align-items: center;
	}

	.checks {
		grid-area: checks;
	}

	.facts {
		display: grid;
		grid-area: facts;
		grid-template-columns: minmax(7.5rem, 1fr) 2.25rem 9.5rem;
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

@media (min-width: 47.56rem) and (max-width: 64rem) {
	.row {
		grid-template-areas:
			"thumb name maker badges"
			"thumb version timing checks";
		grid-template-columns: 4rem minmax(0, 15rem) 13rem minmax(12rem, 1fr);
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

@media (max-width: 61.25rem) {
	.keys {
		display: none;
	}
}

@media (max-width: 47.5rem) {
	.stats {
		width: 100%;
	}

	.stats div {
		flex: 1;
		min-width: 0;
	}

	.filter-box {
		flex: 1 1 100%;
		max-width: none;
	}

	.filter-box .search {
		flex: 1 1 auto;
	}

	.dot {
		width: 0.5rem;
		height: 0.5rem;
	}

	.row {
		grid-template-areas:
			"thumb title title title"
			"thumb facts facts facts"
			"thumb status checks checks";
		grid-template-columns: 4rem 6rem 2.75rem minmax(0, 1fr);
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

@media (max-width: 40rem) {
	.label-full {
		display: none;
	}

	.label-short {
		display: inline;
	}

	.filters button {
		height: 2rem;
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
		flex: 0 0 5.5rem;
		min-width: 0;
		padding: var(--space-1) var(--space-2);
	}
}

@media (min-width: 64rem) {
	.filter-box {
		max-width: 28rem;
	}
}

@media (min-width: 80rem) {
	.filter-box {
		max-width: 32rem;
	}
}

@media (max-width: 25rem) {
	.dots {
		display: none;
	}
}

@media (max-width: 23.13rem) {
	.counts {
		flex-wrap: wrap;
		gap: 0.25rem;
	}

	.checks .fast {
		display: none;
	}
}

@media (max-width: 32rem) {
	.filter-help .syntax div {
		grid-template-columns: 1fr;
		gap: var(--space-1);
	}
}
</style>
