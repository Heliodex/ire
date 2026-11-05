<script lang="ts">
import { onMount } from "svelte"
import { goto } from "$app/navigation"
import { type AuditEntry, fetchAudit, fetchMe, program } from "../../lib/api"
import { ApiError } from "../../lib/api-error"
import Avatar from "../../lib/components/Avatar.svelte"
import LoadingState from "../../lib/components/LoadingState.svelte"
import { formatDateTime, formatHours, timeAgo } from "../../lib/format"
import { reviewerHref } from "../../lib/reviewer"
import { session } from "../../lib/session.svelte.js"

const actionOptions = [
	{ label: "All actions", value: "" },
	{ label: "Review decisions", value: "review.decision" },
	{ label: "Sent back", value: "review.send_back" },
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

interface Change {
	from: string
	key: string
	label: string
	to: string
}

let entries = $state<AuditEntry[]>([])
let action = $state("")
let days = $state(0)
let query = $state("")
let loading = $state(true)
let loadingMore = $state(false)
let hasMore = $state(false)
let error = $state("")
let forbidden = $state(false)
let request = 0
let searchTimer: ReturnType<typeof setTimeout> | null = null

const anyFilter = $derived(action !== "" || days !== 0 || query.trim() !== "")

onMount((): (() => void) => {
	void boot()

	return (): void => {
		if (searchTimer) clearTimeout(searchTimer)
	}
})

async function boot(): Promise<void> {
	try {
		session.me ??= await fetchMe()
	} catch {
		void goto("/")
		return
	}

	const role = session.me?.programs.find(item => item.programSlug === program)?.role ?? ""
	if (role !== "admin" && role !== "auditor") {
		forbidden = true
		loading = false
		return
	}

	await load(true)
}

async function load(reset: boolean): Promise<void> {
	const current = ++request

	if (reset) loading = true
	else loadingMore = true

	try {
		const page = await fetchAudit(reset ? 0 : entries.length, 50, {
			...(action === "" ? {} : { action }),
			...(days === 0 ? {} : { days }),
			...(query.trim() === "" ? {} : { q: query }),
		})

		if (current !== request) return

		const { entries: nextEntries, hasMore: more } = page
		const seen = new Set(reset ? [] : entries.map(entry => entry.id))
		entries = reset ? nextEntries : [...entries, ...nextEntries.filter(entry => !seen.has(entry.id))]
		hasMore = more
		error = ""
		forbidden = false
	} catch (err) {
		if (current !== request) return

		if (err instanceof ApiError && err.code === "unauthorized") {
			void goto("/")
			return
		}

		if (err instanceof ApiError && (err.code === "forbidden" || err.code === "http_error")) {
			forbidden = err.message === "only admins and auditors can read the audit log"
			if (!forbidden) error = err.message
		} else error = err instanceof Error ? err.message : "Could not load the audit log."
	} finally {
		if (current === request) {
			loading = false
			loadingMore = false
		}
	}
}

function reload(): void {
	void load(true)
}

function onSearch(): void {
	if (searchTimer) clearTimeout(searchTimer)
	searchTimer = setTimeout((): void => {
		searchTimer = null
		reload()
	}, 250)
}

function clearFilters(): void {
	action = ""
	days = 0
	query = ""
	reload()
}

function parseJSON(value: string | undefined): { [_: string]: unknown } {
	if (!value) return {}
	try {
		const parsed = JSON.parse(value) as unknown
		return parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)
			? (parsed as { [_: string]: unknown })
			: {}
	} catch {
		return {}
	}
}

const labels: { [_: string]: string } = {
	active: "Active",
	decision: "Decision",
	email: "Email",
	held: "Held",
	hours: "Approved hours",
	invalidated: "Invalidated review",
	reason: "Reason",
	role: "Role",
	stage: "Stage",
	status: "Status",
	version: "Version",
}

function valueLabel(key: string, value: unknown): string {
	if (key === "hours" && typeof value === "number") return formatHours(value)
	if (typeof value === "boolean") return value ? "Yes" : "No"
	if (value === null || value === undefined || value === "") return "–"
	if (Array.isArray(value)) return value.map(String).join(", ") || "–"
	if (typeof value === "object") return JSON.stringify(value)
	return String(value)
}

function sameValue(left: unknown, right: unknown): boolean {
	return JSON.stringify(left) === JSON.stringify(right)
}

function changes(entry: AuditEntry): Change[] {
	const before = parseJSON(entry.beforeJson)
	const after = parseJSON(entry.afterJson)
	const keys = new Set([...Object.keys(before), ...Object.keys(after)])
	const rows: Change[] = []

	for (const key of keys) {
		const from = before[key]
		const to = after[key]
		if (sameValue(from, to)) continue
		// A held=false that was absent before is the default, not a change worth a row.
		if (key === "held" && to !== true && from === undefined) continue

		rows.push({
			from: valueLabel(key, from),
			key,
			label: labels[key] ?? key.replaceAll("_", " "),
			to: valueLabel(key, to),
		})
	}

	return rows
}

function actionLabel(value: string): string {
	switch (value) {
		case "review.decision":
			return "Review decision"
		case "review.send_back":
			return "Sent back"
		case "member.invite":
			return "Member invited"
		case "member.update":
			return "Member updated"
		case "user.sessions_revoke":
			return "Sessions revoked"
		case "user.bootstrap":
			return "Bootstrap account"
		default:
			return value
	}
}

function actionTone(entry: AuditEntry): string {
	const after = parseJSON(entry.afterJson)
	switch (entry.action) {
		case "review.decision":
			if (after.decision === "approved") return "approved"
			if (after.decision === "changes") return "changes"
			if (after.decision === "rejected") return "rejected"
			return "ready"
		case "review.send_back":
			return "secondpass"
		case "member.invite":
		case "member.update":
			return "ready"
		default:
			return "ready"
	}
}

function summary(entry: AuditEntry): string {
	const after = parseJSON(entry.afterJson)

	switch (entry.action) {
		case "review.decision": {
			const decision =
				after.decision === "approved"
					? "Approved"
					: after.decision === "changes"
						? "Requested changes"
						: after.decision === "rejected"
							? "Rejected"
							: "Recorded a decision"
			return after.held === true ? `${decision}, held for a second pass` : decision
		}
		case "review.send_back":
			return "Returned the held ship to stage 1."
		case "member.invite":
			return `Invited ${valueLabel("email", after.email)} as ${valueLabel("role", after.role)}.`
		case "member.update":
			return `Updated ${entry.targetLabel ?? "a member"}'s role or active state.`
		case "user.sessions_revoke":
			return `Revoked every session for ${entry.targetLabel ?? "a member"}.`
		case "user.bootstrap":
			return `Created the first ${valueLabel("role", after.role)} membership for ${entry.actorEmail ?? "the bootstrap account"}.`
		default:
			return ""
	}
}

function actor(entry: AuditEntry): string {
	return entry.actorName || entry.actorEmail || "System"
}

function targetLink(entry: AuditEntry): string | null {
	return entry.targetType === "submission" && entry.targetId ? `/review/${encodeURIComponent(entry.targetId)}` : null
}
</script>

<svelte:head>
	<title>Audit log · Half Life Review</title>
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
		<div class="ui-card audit-list">
			{#each entries as entry (entry.id)}
				<article class="entry">
					<div class="entry-head">
						<span class="ui-badge" data-tone={actionTone(entry)}>{actionLabel(entry.action)}</span>
						{#if reviewerHref(entry.actorEmail)}
							<a
								class="actor"
								data-sveltekit-reload
								href={reviewerHref(entry.actorEmail) ?? "#"}
								title={entry.actorEmail ?? ""}
							>
								<Avatar
									email={entry.actorEmail}
									name={entry.actorName}
									size="xs"
									url={entry.actorAvatarUrl}
								/>
								<span>{actor(entry)}</span>
							</a>
						{:else}
							<span class="actor">
								<Avatar
									email={entry.actorEmail}
									name={entry.actorName}
									size="xs"
									url={entry.actorAvatarUrl}
								/>
								<span>{actor(entry)}</span>
							</span>
						{/if}
						{#if targetLink(entry)}
							<a class="target" href={targetLink(entry)}>{entry.targetLabel ?? entry.targetId}</a>
						{:else if entry.targetId
							&& (entry.action.startsWith("member.") || entry.action.startsWith("user."))}
							<a class="target" data-sveltekit-reload href={reviewerHref(entry.targetId) ?? "#"}>
								{entry.targetLabel ?? entry.targetId}
							</a>
						{:else if entry.targetLabel || entry.targetId}
							<span class="target">{entry.targetLabel ?? entry.targetId}</span>
						{/if}
						<time class="when" datetime={entry.createdAt} title={formatDateTime(entry.createdAt)}>
							{timeAgo(entry.createdAt)}
						</time>
					</div>

					{#if summary(entry)}
						<p class="summary">{summary(entry)}</p>
					{/if}

					{#if entry.reason}
						<p class="reason"><strong>Reason:</strong> {entry.reason}</p>
					{/if}

					{#if changes(entry).length > 0}
						<dl class="changes">
							{#each changes(entry) as change (change.key)}
								<div>
									<dt>{change.label}</dt>
									<dd>
										<span class="from">{change.from}</span>
										<span aria-hidden="true" class="arrow">→</span>
										<span class="to">{change.to}</span>
									</dd>
								</div>
							{/each}
						</dl>
					{/if}
				</article>
			{/each}
		</div>

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
	flex: 1 1 240px;
	max-width: 360px;
}

.select {
	width: auto;
	min-width: 140px;
}

.count {
	margin-left: auto;
	font-size: var(--text-sm);
	color: var(--text-2);
}

.audit-list {
	overflow: hidden;
}

.entry {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-4);
	border-bottom: 1px solid var(--border);
}

.entry:last-child {
	border-bottom: 0;
}

.entry-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
	font-size: var(--text-sm);
}

.actor {
	display: inline-flex;
	gap: var(--space-2);
	align-items: center;
	font-weight: 600;
	color: var(--text);
	text-decoration: none;
}

a.actor:hover {
	text-decoration: underline;
}

.target {
	color: var(--text-2);
	text-decoration: none;
}

a.target:hover {
	color: var(--text);
	text-decoration: underline;
}

.when {
	margin-left: auto;
	font-size: var(--text-xs);
	color: var(--text-3);
	white-space: nowrap;
}

.summary {
	font-size: var(--text-sm);
	color: var(--text-2);
}

.reason {
	font-size: var(--text-sm);
	color: var(--text-2);
}

.changes {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	padding-top: var(--space-1);
}

.changes div {
	display: inline-flex;
	gap: var(--space-2);
	align-items: baseline;
	padding: 4px var(--space-2);
	font-size: var(--text-xs);
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.changes dt {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-3);
}

.changes dd {
	display: inline-flex;
	gap: 4px;
	align-items: baseline;
	margin: 0;
	font-size: var(--text-xs);
	font-variant-numeric: tabular-nums;
	white-space: nowrap;
}

.changes .from {
	color: var(--text-3);
}

.changes .arrow {
	color: var(--text-3);
}

.changes .to {
	font-weight: 700;
	color: var(--text);
}

.more {
	display: flex;
	justify-content: center;
	padding-top: var(--space-3);
}
</style>
