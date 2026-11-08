<script lang="ts">
import { onMount } from "svelte"
import { SvelteMap } from "svelte/reactivity"
import { browser } from "$app/env"
import { goto } from "$app/navigation"
import { fetchAudit, fetchMe, program } from '../../lib/api';
import type { AuditEntry } from '../../lib/api';
import { ApiError } from "../../lib/api-error"
import { auditFilterKey, createAuditCache, isFreshAuditView } from '../../lib/audit-cache';
import type { AuditFilters, CachedAuditView } from '../../lib/audit-cache';
import AuditRow from "../../lib/components/audit/AuditRow.svelte"
import LoadingState from "../../lib/components/LoadingState.svelte"
import { hasPermission, permissionAuditRead, programPermissions } from "../../lib/permissions"
import { dropSignIn, session } from "../../lib/session.svelte.js"

const actionOptions = [
	{ label: "All actions", value: "" },
	{ label: "Review decisions", value: "review.decision" },
	{ label: "Sent back", value: "review.send_back" },
	{ label: "Journal adjusted", value: "journal.adjust" },
	{ label: "Notes added", value: "note.create" },
	{ label: "Notes removed", value: "note.delete" },
	{ label: "Members invited", value: "member.invite" },
	{ label: "Members updated", value: "member.update" },
	{ label: "Sessions revoked", value: "user.sessions_revoke" },
	{ label: "Bootstrap accounts", value: "user.bootstrap" },
]

const ageOptions = [
	{ label: "All time", value: 0 },
	{ label: "Last 24 hours", value: 1 },
	{ label: "Last 7 days", value: 7 },
	{ label: "Last 30 days", value: 30 },
	{ label: "Last 90 days", value: 90 },
]

let entries = $state<AuditEntry[]>([])
let action = $state("")
let days = $state(0)
let query = $state("")
let loading = $state(true)
let loadingMore = $state(false)
let revalidating = $state(false)
let hasMore = $state(false)
let error = $state("")
let forbidden = $state(false)
let request = 0
let searchTimer: ReturnType<typeof setTimeout> | null

// auditCache persists the first page per filter and the last selection in tab storage. It is
// built lazily so sessionStorage is only touched on the client.
const auditCache = browser ? createAuditCache(program, sessionStorage) : undefined
// views keeps the same first pages in memory, so switching back never waits for storage or the network.
const views = new SvelteMap<string, CachedAuditView>()

const anyFilter = $derived(action !== "" || days !== 0 || query.trim() !== "")

onMount((): (() => void) => {
	void boot()

	return (): void => {
		if (searchTimer) clearTimeout(searchTimer)
	}
})

function currentFilters(): AuditFilters {
	return { action, days, query }
}

// readCachedView returns a fresh first page for these filters from memory, then tab storage.
function readCachedView(filters: AuditFilters): CachedAuditView | null {
	const key = auditFilterKey(filters)
	const stored = views.get(key)

	if (stored && isFreshAuditView(stored)) return stored
	views.delete(key)

	const cached = auditCache?.readView(filters) ?? undefined
	if (cached) views.set(key, cached)

	return cached
}

// showCachedView paints a stored page while the fresh request runs behind it.
function showCachedView(view: CachedAuditView): void {
	const { entries: cachedEntries, hasMore: more } = view
	entries = cachedEntries
	hasMore = more
	loading = false
	loadingMore = false
	error = ""
	forbidden = false
}

function saveFilters(): void {
	auditCache?.writeFilters(currentFilters())
}

async function boot(): Promise<void> {
	try {
		session.me ??= await fetchMe()
	} catch {
		void goto("/")
		return
	}

	const permissions = programPermissions(session.me, program)
	if (!hasPermission(permissions, permissionAuditRead)) {
		forbidden = true
		loading = false
		return
	}

	const saved = auditCache?.readFilters()
	if (saved) {
		const { action: savedAction, days: savedDays, query: savedQuery } = saved
		if (actionOptions.some(option => option.value === savedAction)) action = savedAction
		if (ageOptions.some(option => option.value === savedDays)) days = savedDays
		query = savedQuery
	}

	const cached = readCachedView(currentFilters())
	if (cached) {
		showCachedView(cached)
		void load(true, true)
		return
	}

	await load(true)
}

async function load(reset: boolean, background = false): Promise<void> {
	const filters = currentFilters()
	const key = auditFilterKey(filters)
	const current = ++request
	// Revalidating a cached view fetches at most the same first page, then merges the older
	// cached rows so a background refresh never shrinks what the reader has already loaded.
	const shown = background && reset ? entries : undefined
	const limit = shown ? Math.min(Math.max(shown.length, 50), 100) : 50

	if (reset) loadingMore = false

	if (background) revalidating = true
	else {
		revalidating = false
		if (reset) loading = true
		else loadingMore = true
	}

	try {
		const page = await fetchAudit(reset ? 0 : entries.length, limit, {
			...(filters.action === "" ? {} : { action: filters.action }),
			...(filters.days === 0 ? {} : { days: filters.days }),
			...(filters.query.trim() === "" ? {} : { q: filters.query }),
		})

		if (current !== request) return

		const { entries: nextEntries, hasMore: more } = page
		let merged = nextEntries

		if (reset && shown && shown.length > 0 && more) {
			const seen = new Set(nextEntries.map(entry => entry.id))
			merged = [...nextEntries, ...shown.filter(entry => !seen.has(entry.id))]
		}

		if (!reset) {
			const seen = new Set(entries.map(entry => entry.id))
			merged = [...entries, ...nextEntries.filter(entry => !seen.has(entry.id))]
		}

		entries = merged
		hasMore = more
		error = ""
		forbidden = false

		const view: CachedAuditView = { entries: merged, hasMore: more, savedAt: Date.now() }
		views.set(key, view)
		auditCache?.writeView(filters, { entries: view.entries, hasMore: view.hasMore })
	} catch (err) {
		if (current !== request) return

		if (err instanceof ApiError && err.code === "unauthorized") {
			dropSignIn()
			void goto("/")
			return
		}

		if (err instanceof ApiError && (err.code === "forbidden" || err.code === "http_error")) {
			forbidden = err.message === "only admins and auditors can read the audit log"
			if (!forbidden && !background) error = err.message
		} else if (!background || entries.length === 0)
			error = err instanceof Error ? err.message : "Could not load the audit log."
	} finally {
		if (current === request) {
			loading = false
			loadingMore = false
			revalidating = false
		}
	}
}

function reload(): void {
	saveFilters()

	const cached = readCachedView(currentFilters())
	if (cached) {
		showCachedView(cached)
		void load(true, true)
		return
	}

	void load(true)
}

function onSearch(): void {
	if (searchTimer) clearTimeout(searchTimer)
	searchTimer = setTimeout((): void => {
		searchTimer = undefined
		reload()
	}, 250)
}

function clearFilters(): void {
	action = ""
	days = 0
	query = ""
	reload()
}
</script>

<svelte:head>
	<title>Audit log · Ire</title>
</svelte:head>

<section class="page-head">
	<div>
		<h1>Audit log</h1>
		<p>Review decisions, roster changes and account actions across the programme.</p>
	</div>
</section>

{#if forbidden}
	<div class="ui-card ui-empty">
		<strong>Admins and auditors only</strong>
		<span>Your account can review ships, but the full audit log is limited to admins and auditors.</span>
		<a class="ui-button" data-variant="outline" href="/queue">Back to the queue</a>
	</div>
{:else}
	<div class="toolbar">
		<input
			aria-label="Search the audit log"
			class="ui-input search"
			oninput={onSearch}
			placeholder="Search actions, actors, ships or reasons…"
			type="search"
			bind:value={query}
		>
		<select aria-label="Filter by action" class="ui-input select" onchange={reload} bind:value={action}>
			{#each actionOptions as option (option.value)}
				<option value={option.value}>{option.label}</option>
			{/each}
		</select>
		<select aria-label="Filter by age" class="ui-input select" onchange={reload} bind:value={days}>
			{#each ageOptions as option (option.value)}
				<option value={option.value}>{option.label}</option>
			{/each}
		</select>
		{#if anyFilter}
			<button class="ui-button quiet sm" onclick={clearFilters} type="button">Clear</button>
		{/if}
		<span class="count" role="status">
			{#if !loading}
				{entries.length}{entries.length === 1 ? " event" : " events"}
				{#if revalidating}
					· updating…
				{/if}
			{/if}
		</span>
	</div>

	{#if error}
		<div class="ui-notice" data-tone="bad" role="alert">{error}</div>
	{/if}

	{#if loading}
		<div class="ui-card audit-list">
			<LoadingState message="Loading audit log…" />
		</div>
	{:else if entries.length === 0}
		<div class="ui-card ui-empty">
			<strong>{anyFilter ? "No events match those filters" : "No events yet"}</strong>
			<span
				>{anyFilter
					? "Try a wider date range or fewer filters."
					: "Review decisions and roster changes will appear here."}</span
			>
			{#if anyFilter}
				<button class="ui-button" data-variant="outline" onclick={clearFilters} type="button">
					Clear filters
				</button>
			{/if}
		</div>
	{:else}
		<ol aria-busy={revalidating} class="ui-card audit-list">
			{#each entries as entry (entry.id)}
				<AuditRow {entry} />
			{/each}
		</ol>

		{#if hasMore}
			<div class="more">
				<button
					class="ui-button"
					data-variant="outline"
					disabled={loadingMore}
					onclick={(): void => void load(false)}
					type="button"
				>
					{loadingMore ? "Loading…" : "Load more events"}
				</button>
			</div>
		{/if}
	{/if}
{/if}

<style>
.page-head {
	margin-bottom: var(--space-4);
}

.page-head h1 {
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
	gap: var(--space-2);
	align-items: center;
	margin-bottom: var(--space-3);
}

.search {
	flex: 1 1 15rem;
	max-width: 22.5rem;
}

.select {
	width: auto;
	min-width: 8.75rem;
}

.count {
	margin-left: auto;
	font-size: var(--text-sm);
	color: var(--text-2);
}

.audit-list {
	padding: 0;
	margin: 0;
	overflow: hidden;
	list-style: none;
}

.more {
	display: flex;
	justify-content: center;
	padding-top: var(--space-3);
}
</style>
