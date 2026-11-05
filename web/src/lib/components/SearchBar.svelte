<script lang="ts">
import { onMount } from "svelte"
import { type SearchResult, searchProjects } from "../api"
import { humanStatus, statusTone } from "../format"
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
let timer: ReturnType<typeof setTimeout> | null = null
let sequence = 0

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
	const cached = cache.get(text)

	error = ""

	if (text === "") {
		results = []
		selected = -1
		loading = false
		return
	}

	open = true

	if (cached !== undefined) {
		results = cached
		selected = cached.length > 0 ? 0 : -1
		loading = false
	} else {
		results = []
		selected = -1
		loading = true
	}

	timer = setTimeout((): void => {
		timer = null
		void search(text, id, cached !== undefined)
	}, debounceMs)
}

async function search(text: string, id: number, revalidating: boolean): Promise<void> {
	try {
		const found = await searchProjects(text)
		if (id !== sequence) return

		cache.set(text, found)
		results = found

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

	if (event.key !== "ArrowDown" && event.key !== "ArrowUp") {
		if (event.key !== "Enter" || loading) return

		const result = results[selected]
		if (result) {
			event.preventDefault()
			openResult(result)
		}

		return
	}

	event.preventDefault()

	if (!showResults) {
		open = query.trim() !== ""
		return
	}

	if (results.length === 0) return

	const step = event.key === "ArrowDown" ? 1 : -1
	selected = (selected + step + results.length) % results.length
}

function openResult(result: SearchResult): void {
	open = false
	window.location.assign(`/review/${encodeURIComponent(result.id)}`)
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
		<div class="results ui-card" id="project-search-results" role="listbox">
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
						<span class="ui-badge status" data-tone={statusTone(result.status)}>
							{humanStatus(result.status)}
						</span>
					</button>
				{/each}
			{/if}
			<div class="hint">
				<kbd class="ui-kbd">↑</kbd><kbd class="ui-kbd">↓</kbd>
				to move ·
				<kbd class="ui-kbd">Enter</kbd>
				to open ·
				<kbd class="ui-kbd">Esc</kbd>
				to close
			</div>
		</div>
	{/if}
</div>

<style>
.search {
	position: relative;
	flex: 0 1 320px;
	min-width: 0;
}

.icon {
	position: absolute;
	top: 50%;
	left: 10px;
	color: var(--text-3);
	pointer-events: none;
	transform: translateY(-50%);
}

.search :global(input) {
	padding-right: 34px;
	padding-left: 31px;
}

.shortcut {
	position: absolute;
	top: 50%;
	right: 8px;
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
	overflow: hidden;
	box-shadow: var(--shadow);
}

.result {
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

.thumb {
	display: grid;
	flex: none;
	place-items: center;
	width: 40px;
	height: 30px;
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

.message {
	padding: var(--space-4) var(--space-3);
	font-size: var(--text-sm);
	color: var(--text-2);
}

.hint {
	padding: var(--space-2) var(--space-3);
	font-size: var(--text-xs);
	color: var(--text-3);
	background: var(--surface-2);
}

@media (max-width: 640px) {
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
		padding-right: 12px;
	}
}
</style>
