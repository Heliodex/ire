<script lang="ts">
import { onMount } from "svelte"
import { goto } from "$app/navigation"
import { type SearchResult, searchProjects } from "../api"
import { formatDuration, humanStatus, statusTone, timeAgo } from "../format"
import { rememberReviewPreview } from "../review-preview"
import { createSearchCache } from "../search-cache"

const debounceMs = 160
const cache = createSearchCache()

let query = $state("")
let results = $state<SearchResult[]>([])
let open = $state(false)
let loading = $state(false)
let error = $state("")
let selected = $state(-1)
let input = $state<HTMLInputElement | null>(null)
let box = $state<HTMLDivElement | null>(null)
let listbox = $state<HTMLDivElement | null>(null)
let timer: ReturnType<typeof setTimeout> | null = null
let sequence = 0
// The first page stays small; reaching its last row with the arrow keys asks for the next slice.
const initialSearchLimit = 8
const maxSearchResults = 20
let limit = initialSearchLimit
let exhausted = false
let extending = $state(false)

const showResults = $derived(open && query.trim() !== "")
const message = $derived.by((): string => {
	if (loading) return "Searching projects…"
	if (error) return error
	if (results.length === 0) return `No open projects match “${query.trim()}”.`

	return ""
})

onMount((): (() => void) => () => {
	if (timer) clearTimeout(timer)
})

// isTyping reports whether a keystroke belongs to a field rather than the page.
function isTyping(target: EventTarget | null): boolean {
	return (
		target instanceof HTMLInputElement
		|| target instanceof HTMLTextAreaElement
		|| target instanceof HTMLSelectElement
		|| (target instanceof HTMLElement && target.isContentEditable)
	)
}

// onHotkey focuses the search from anywhere on the page.
function onHotkey(event: KeyboardEvent): void {
	if (event.key !== "/" || event.ctrlKey || event.metaKey || event.altKey) return
	if (isTyping(event.target)) return

	event.preventDefault()
	input?.focus()
	input?.select()
}

// onPointerDown closes the dropdown when the reader clicks elsewhere.
function onPointerDown(event: PointerEvent): void {
	if (box?.contains(event.target as Node)) return

	open = false
}

// onInput shows a cached query immediately, then refreshes it in the background.
function onInput(): void {
	if (timer) clearTimeout(timer)

	const text = query.trim()
	const id = ++sequence

	error = ""
	limit = initialSearchLimit
	exhausted = false
	extending = false

	if (text === "") {
		results = []
		selected = -1
		loading = false
		return
	}

	open = true

	// Exact hits come first; otherwise reuse rows from earlier queries that still match the new text.
	const cached = cache.get(text)
	const suggested = (cached ?? cache.match(text)).slice(0, maxSearchResults)
	const showing = cached !== undefined || suggested.length > 0

	if (showing) {
		results = suggested
		selected = suggested.length > 0 ? 0 : -1
		loading = false
		limit = Math.max(limit, suggested.length)
	} else {
		results = []
		selected = -1
		loading = true
	}

	timer = setTimeout((): void => {
		timer = null
		void search(text, id, showing, limit)
	}, debounceMs)
}

async function search(text: string, id: number, revalidating: boolean, requestLimit: number): Promise<void> {
	try {
		const found = await searchProjects(text, requestLimit)
		if (id !== sequence) return

		cache.set(text, found)
		results = found
		limit = requestLimit
		exhausted = found.length < requestLimit

		if (revalidating) selected = found.length > 0 ? Math.min(Math.max(selected, 0), found.length - 1) : -1
		else selected = found.length > 0 ? 0 : -1
	} catch (err) {
		if (id !== sequence) return
		if (revalidating) return

		results = []
		error = err instanceof Error ? err.message : "Could not search projects."
	} finally {
		if (id === sequence && !revalidating) loading = false
	}
}

// extend loads the next slice of matches when the cursor reaches the last row or passes the middle.
async function extend(): Promise<void> {
	if (extending || loading || exhausted) return
	if (results.length >= maxSearchResults) return

	const text = query.trim()
	const id = sequence
	const next = Math.min(maxSearchResults, results.length + initialSearchLimit)
	const before = results.length

	extending = true

	try {
		const found = await searchProjects(text, next)
		if (id !== sequence) return

		cache.set(text, found)
		results = found
		limit = next
		exhausted = found.length < next

		// At the end, step onto the first new row; in the middle, keep the cursor where it is.
		if (found.length > before && selected >= before - 1) selected = before
		else selected = Math.min(selected, Math.max(found.length - 1, 0))
	} catch {
		// Keep the rows already on screen; the next press can try again.
	} finally {
		if (id === sequence) extending = false
	}
}

// maybeExtend pulls the next slice once the cursor passes the middle of the loaded rows, so the
// rows below are ready before the cursor reaches them.
function maybeExtend(): void {
	if (exhausted || extending || loading) return
	if (results.length >= maxSearchResults) return
	if (selected < Math.floor(results.length / 2)) return

	void extend()
}

// onFocus reopens the last results when the reader comes back to the field.
function onFocus(): void {
	open = query.trim() !== ""
}

function onKeydown(event: KeyboardEvent): void {
	if (event.key === "Escape") {
		open = false
		input?.blur()
		return
	}

	if (event.key === "ArrowDown") {
		event.preventDefault()

		if (!showResults) {
			open = query.trim() !== ""
			return
		}

		if (results.length === 0) return

		// Stop at the last row and pull in more matches instead of jumping back to the top.
		if (selected >= results.length - 1) {
			void extend()

			return
		}

		selected += 1
		maybeExtend()

		return
	}

	if (event.key === "ArrowUp") {
		event.preventDefault()

		if (!showResults) {
			open = query.trim() !== ""
			return
		}

		if (results.length === 0) return

		selected = Math.max(0, selected - 1)

		return
	}

	if (event.key !== "Enter" || loading) return

	const result = results[selected]
	if (result) {
		event.preventDefault()
		openResult(result)
	}
}

// revealSelection centres the arrow-key row in the dropdown, like the queue does for its cursor.
function revealSelection(): void {
	if (!listbox || selected < 0) return

	const node = listbox.querySelector<HTMLElement>(`#project-search-${selected}`)
	if (!node) return

	const max = listbox.scrollHeight - listbox.clientHeight
	const target = node.offsetTop + node.offsetHeight / 2 - listbox.clientHeight / 2

	listbox.scrollTop = Math.max(0, Math.min(target, max))
}

$effect((): void => {
	void selected
	void results
	void showResults
	revealSelection()
})

function openResult(result: SearchResult): void {
	open = false
	input?.blur()
	rememberReviewPreview({
		id: result.id,
		makerEmail: result.makerEmail,
		makerName: result.makerName,
		makerSlackId: result.makerSlackId,
		requestedTier: result.requestedTier,
		status: result.status,
		thumbnailUrl: result.thumbnailUrl,
		title: result.title,
		version: result.version,
		week: result.week,
	})
	void goto(`/review/${encodeURIComponent(result.id)}`)
}
</script>

<svelte:window onkeydown={onHotkey} onpointerdown={onPointerDown} />

<div class="search" bind:this={box}>
	<label class="visually-hidden" for="project-search">Search all projects</label>
	<svg aria-hidden="true" class="icon" fill="none" height="15" viewBox="0 0 24 24" width="15">
		<circle cx="11" cy="11" r="7" stroke="currentColor" stroke-width="2" />
		<path d="m20 20-4-4" stroke="currentColor" stroke-linecap="round" stroke-width="2" />
	</svg>
	<input
		aria-activedescendant={showResults && selected >= 0 ? `project-search-${selected}` : undefined}
		aria-autocomplete="list"
		aria-controls="project-search-results"
		aria-expanded={showResults}
		aria-label="Search all projects"
		autocomplete="off"
		class="ui-input"
		id="project-search"
		onfocus={onFocus}
		oninput={onInput}
		onkeydown={onKeydown}
		placeholder="Search all projects…"
		role="combobox"
		spellcheck="false"
		type="search"
		bind:this={input}
		bind:value={query}
	>
	<kbd aria-hidden="true" class="ui-kbd shortcut">/</kbd>

	{#if showResults}
		<div class="results ui-card">
			<div class="options" id="project-search-results" role="listbox" bind:this={listbox}>
				{#if message}
					<div class="message">{message}</div>
				{:else}
					{#each results as result, index (result.id)}
						<button
							aria-selected={index === selected}
							class="result"
							id={`project-search-${index}`}
							onclick={(): void => openResult(result)}
							onmouseenter={(): void => {
								selected = index
							}}
							role="option"
							tabindex="-1"
							type="button"
							class:active={index === selected}
						>
							<span class="thumb">
								{#if result.thumbnailUrl}
									<img alt="" decoding="async" loading="lazy" src={result.thumbnailUrl}>
								{:else}
									<svg aria-hidden="true" fill="none" height="14" viewBox="0 0 24 24" width="14">
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
								<span class="title">{result.title}</span>
								<span class="meta">{result.makerName} · {result.externalId}</span>
							</span>
							<span class="details">
								{#if result.stage === 2}
									<span class="fact pass">Pass 2</span>
								{/if}
								{#if result.theme}
									<span class="fact">{result.theme}</span>
								{/if}
								<span class="fact">v{result.version}</span>
								{#if result.week}
									<span class="fact">Week {result.week}</span>
								{/if}
								{#if result.hoursClaimedSeconds > 0}
									<span class="fact">{formatDuration(result.hoursClaimedSeconds)} claimed</span>
								{/if}
								<span class="fact">{timeAgo(result.ingestedAt)}</span>
							</span>
							<span class="ui-badge status" data-tone={statusTone(result.status)}>
								{humanStatus(result.status)}
							</span>
						</button>
					{/each}
				{/if}
			</div>
			<div class="hint">
				<kbd class="ui-kbd">↑</kbd><kbd class="ui-kbd">↓</kbd>
				to move ·
				<kbd class="ui-kbd">Enter</kbd>
				to open ·
				<kbd class="ui-kbd">Esc</kbd>
				to close
				{#if extending}
					· Loading more…
				{/if}
			</div>
		</div>
	{/if}
</div>

<style>
.search {
	position: relative;
	flex: 0 1 20rem;
	min-width: 0;
}

.icon {
	position: absolute;
	top: 50%;
	left: 0.75rem;
	color: var(--text-3);
	pointer-events: none;
	transform: translateY(-50%);
}

.search :global(input) {
	padding-right: 2.25rem;
	padding-left: 2rem;
}

.shortcut {
	position: absolute;
	top: 50%;
	right: 0.5rem;
	pointer-events: none;
	transform: translateY(-50%);
}

.search:focus-within .shortcut {
	opacity: 0;
}

.results {
	position: absolute;
	top: calc(100% + var(--space-2));
	right: 0;
	left: 0;
	z-index: var(--layer-dialog);
	display: flex;
	flex-direction: column;
	max-height: min(70vh, 40rem);
	overflow: hidden;
	box-shadow: var(--shadow);
}

.options {
	position: relative;
	min-height: 0;
	overflow-x: hidden;
	overflow-y: auto;
}

.result {
	position: relative;
	display: flex;
	gap: var(--space-3);
	align-items: center;
	width: 100%;
	padding: var(--space-2) var(--space-3);
	text-align: left;
	background: transparent;
	border: 0;
	border-bottom: 1px solid var(--border);
}

.result:hover,
.result.active {
	background: var(--surface-2);
}

.result.active::before {
	position: absolute;
	inset: 0 auto 0 0;
	width: 0.25rem;
	content: "";
	background: var(--primary);
}

.thumb {
	display: grid;
	flex: none;
	place-items: center;
	width: 2.5rem;
	height: 2rem;
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
	flex: 1;
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
	font-size: var(--text-sm);
	font-weight: 700;
}

.meta {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.status {
	flex: none;
}

/* The extra facts are only wide enough to fit when the dropdown grows on a large screen. */
.details {
	display: none;
	flex: none;
	gap: var(--space-3);
	align-items: center;
	font-size: var(--text-xs);
	color: var(--text-3);
	white-space: nowrap;
}

.fact.pass {
	font-weight: 700;
	color: var(--status-secondpass);
}

.message {
	padding: var(--space-4) var(--space-3);
	font-size: var(--text-sm);
	color: var(--text-2);
}

.hint {
	flex: none;
	padding: var(--space-2) var(--space-3);
	font-size: var(--text-xs);
	color: var(--text-3);
	background: var(--surface-2);
	border-top: 1px solid var(--border);
}

@media (max-width: 40rem) {
	.search {
		flex: 1 1 100%;
		order: 5;
	}

	.meta {
		display: none;
	}

	.shortcut {
		display: none;
	}

	.search :global(input) {
		padding-right: 0.75rem;
	}
}

/* On a wide header the dropdown reaches left of the field and can afford the per-ship facts. */
@media (min-width: 64rem) {
	.results {
		left: auto;
		width: min(46rem, calc(100vw - 2rem));
	}

	.details {
		display: flex;
	}
}
</style>
