<script lang="ts">
import { afterNavigate, goto } from "$app/navigation"
import { page } from "$app/state"
import { onMount } from "svelte"
import { ApiError } from "../../lib/api-error"
import {
	claimShip,
	decideShip,
	fetchBundle,
	fetchMe,
	heartbeat,
	releaseShip,
	saveDraft,
	type Bundle,
	type Check,
} from "../../lib/api"
import { formatDateTime, formatHours, formatMoney, humanStatus, statusTone } from "../../lib/format"
import { session } from "../../lib/session.svelte.js"

type Tab = "overview" | "checks" | "journal" | "bom"

let bundle = $state<Bundle | null>(null)
let id = $state("")
let loading = $state(true)
let error = $state("")
let tab = $state<Tab>("overview")
let note = $state("")
let internalNote = $state("")
let hours = $state(0)
let acknowledgements = $state<string[]>([])
let deciding = $state(false)
let actionError = $state("")
let toast = $state("")
let showShortcuts = $state(false)
let noteElement = $state<HTMLTextAreaElement | null>(null)
let claimedId = $state("")
let idempotencyKey = $state("")

const submission = $derived(bundle?.submission ?? null)
const warnings = $derived(bundle?.checks.filter(isWarning) ?? [])
const blocking = $derived(bundle?.checks.filter(isBlocking) ?? [])
const missingAcks = $derived(warnings.filter((check) => !acknowledgements.includes(check.key)))
const canApprove = $derived(blocking.length === 0 && missingAcks.length === 0)
const groups = $derived(groupChecks(bundle?.checks ?? []))

afterNavigate((): void => {
	const next = page.url.searchParams.get("id") ?? ""
	if (next === "" || next === id) return

	const previous = claimedId
	id = next
	loading = true
	error = ""
	acknowledgements = []
	idempotencyKey = ""

	if (previous && previous !== next) void releaseShip(previous).catch(() => {})

	claimedId = ""
	void load(next)
})

onMount((): (() => void) => {
	id = page.url.searchParams.get("id") ?? ""
	if (id === "") {
		void goto("/queue")
		return (): void => {}
	}

	void boot()

	const beat = setInterval((): void => {
		// A backgrounded tab does not need to hold the claim awake; the TTL releases it if the reviewer walks away.
		if (document.visibilityState !== "visible" || claimedId === "") return
		void heartbeat(claimedId).catch(() => {})
	}, 60000)

	function onKeydown(event: KeyboardEvent): void {
		if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) {
			if (event.key === "Escape") (event.target as HTMLElement).blur()
			return
		}

		if (event.key === "a") {
			event.preventDefault()
			void decide("approved")
		} else if (event.key === "c") {
			event.preventDefault()
			noteElement?.focus()
		} else if (event.key === "r") {
			event.preventDefault()
			void decide("rejected")
		} else if (event.key === "Escape") {
			void goto("/queue")
		} else if (event.key === "?") {
			showShortcuts = !showShortcuts
		}
	}

	window.addEventListener("keydown", onKeydown)

	return (): void => {
		clearInterval(beat)
		window.removeEventListener("keydown", onKeydown)
		if (claimedId !== "") void releaseShip(claimedId).catch(() => {})
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

async function load(target = id): Promise<void> {
	try {
		note = ""
		internalNote = ""
		actionError = ""

		const fetched = await fetchBundle(target)
		if (target !== id) return

		let next = fetched
		if (!fetched.claim || fetched.claim.isMine !== 1) {
			await claimShip(target)
			next = await fetchBundle(target)
			if (target !== id) return
		}

		bundle = next
		claimedId = target

		// Restore the server-side draft so a refresh or another device does not lose typed notes.
		if (next.draft) {
			note = next.draft.note
			internalNote = next.draft.internalNote
		}

		hourInit()
		error = ""
	} catch (err) {
		if (err instanceof ApiError && err.code === "unauthorized") {
			void goto("/")
			return
		}
		if (target === id) error = err instanceof Error ? err.message : "Could not open this ship."
	} finally {
		if (target === id) loading = false
	}
}

function hourInit(): void {
	if (!bundle) return
	const seconds = bundle.submission.hoursApprovedSeconds ?? bundle.submission.hoursClaimedSeconds
	hours = Math.round((seconds / 3600) * 100) / 100
	acknowledgements = [...bundle.acknowledgements]
}

function isWarning(check: Check): boolean {
	return check.status === "warn" || (check.status === "fail" && check.severity === "warn")
}

function isBlocking(check: Check): boolean {
	return check.severity === "block" && (check.status === "fail" || check.status === "error")
}

function overall(checks: Check[]): string {
	if (checks.some(isBlocking)) return "fail"
	if (checks.some(isWarning)) return "warn"
	return "pass"
}

function groupChecks(checks: Check[]): { group: string; items: Check[] }[] {
	const order = ["repo", "evidence", "demo", "identity", "duplicate", "fraud"]
	const map = new Map<string, Check[]>()
	for (const check of checks) {
		const list = map.get(check.group) ?? []
		list.push(check)
		map.set(check.group, list)
	}
	return [...map.entries()]
		.sort(([a], [b]) => order.indexOf(a) - order.indexOf(b))
		.map(([group, items]) => ({ group, items }))
}

function toggleAck(key: string): void {
	if (acknowledgements.includes(key)) acknowledgements = acknowledgements.filter((value) => value !== key)
	else acknowledgements = [...acknowledgements, key]
}

function acknowledgeAll(): void {
	acknowledgements = warnings.map((check) => check.key)
}

async function decide(decision: "approved" | "changes" | "rejected"): Promise<void> {
	if (!bundle || deciding) return

	actionError = ""

	if (decision === "approved") {
		if (blocking.length > 0) {
			actionError = "Approval is blocked. Read the blocking check, then ask for changes or reject."
			return
		}
		if (missingAcks.length > 0) {
			actionError = `Acknowledge ${missingAcks.length} warning${missingAcks.length === 1 ? "" : "s"} before approving.`
			return
		}
	} else if (note.trim() === "") {
		actionError = "Write a note to the maker before returning or rejecting."
		noteElement?.focus()
		return
	}

	deciding = true
	try {
		// One key per ship and version: if the response is lost, the retry returns the review that was already written instead of creating a second one.
		const prefix = `${id}:${bundle.submission.version}:`
		const key = idempotencyKey.startsWith(prefix) ? idempotencyKey : `${prefix}${crypto.randomUUID()}`
		idempotencyKey = key

		const result = await decideShip(id, {
			decision,
			version: bundle.submission.version,
			noteToMaker: note.trim(),
			internalNote: internalNote.trim(),
			hoursApprovedSeconds: Math.max(0, Math.round(hours * 3600)),
			acknowledgements,
			idempotencyKey: key,
		})
		idempotencyKey = ""

		if (result.held === 1) {
			flash(`Held for the second pass: ${result.heldReason ?? "policy"}.`)
		} else {
			const verb = decision === "approved" ? "Approved" : decision === "changes" ? "Changes requested" : "Rejected"
			flash(`${verb} and sent${result.deliveryStatus ? ` (${result.deliveryStatus})` : ""}.`)
		}

		await new Promise((resolve) => setTimeout(resolve, 300))

		if (result.nextId) await goto(`/review?id=${encodeURIComponent(result.nextId)}`)
		else await goto("/queue")
	} catch (err) {
		if (err instanceof ApiError && err.code === "stale_version") {
			actionError = "This ship changed since you opened it. Reload before deciding."
		} else {
			actionError = err instanceof Error ? err.message : "Could not record the decision."
		}
	} finally {
		deciding = false
	}
}

function flash(message: string): void {
	toast = message
	setTimeout((): void => {
		if (toast === message) toast = ""
	}, 3500)
}

async function saveDraftNow(): Promise<void> {
	if (!bundle) return
	try {
		await saveDraft(id, { note, internalNote })
	} catch {
		// Draft saving is best effort; the decision is the durable write.
	}
}
</script>

<svelte:head>
	<title>{submission ? `${submission.title} · Review` : "Review · Half Life"}</title>
</svelte:head>

{#if loading}
	<div class="loading">
		<div class="ui-card"></div>
		<div class="ui-card"></div>
	</div>
{:else if error || !bundle || !submission}
	<section class="missing">
		<h1>Cannot open this ship</h1>
		<p>{error || "This ship does not exist or is not yours to review."}</p>
		<a class="ui-button" href="/queue">Return to queue</a>
	</section>
{:else}
	<header class="ship-head">
		<div class="head-main">
			<p class="crumbs">
				<a href="/queue">Queue</a>
				<span>/</span>
				{humanStatus(submission.status)}
				{#if submission.theme}<span>/</span>{submission.theme}{/if}
			</p>
			<div class="title-line">
				<h1>{submission.title}</h1>
				<span class="ui-badge" data-tone={statusTone(submission.status)}>{humanStatus(submission.status)}</span>
				<span class="version">v{submission.version}</span>
			</div>
			<p class="maker">
				{#if submission.makerName}{submission.makerName} · {/if}{submission.makerEmail}
				{#if submission.week}<span>· Week {submission.week}</span>{/if}
				{#if submission.requestedTier}<span>· Tier {submission.requestedTier}</span>{/if}
			</p>
		</div>
		<div class="head-links">
			{#if submission.repoUrl}
				<a class="ui-button ghost sm" href={submission.repoUrl} rel="noreferrer" target="_blank">Repository ↗</a>
			{/if}
			{#if submission.demoUrl}
				<a class="ui-button ghost sm" href={submission.demoUrl} rel="noreferrer" target="_blank">Demo ↗</a>
			{/if}
			<button class="ui-button ghost sm" onclick={(): void => { showShortcuts = !showShortcuts }} type="button">
				Shortcuts <kbd class="ui-kbd">?</kbd>
			</button>
		</div>
	</header>

	{#if showShortcuts}
		<div class="shortcuts">
			<span><kbd class="ui-kbd">A</kbd> approve</span>
			<span><kbd class="ui-kbd">C</kbd> focus the note</span>
			<span><kbd class="ui-kbd">R</kbd> reject</span>
			<span><kbd class="ui-kbd">Esc</kbd> back to queue</span>
		</div>
	{/if}

	<div class="layout">
		<section class="content">
			<div aria-label="Ship sections" class="ui-tabs" role="tablist">
				{#each [{ key: "overview", label: "Overview" }, { key: "checks", label: `Checks (${bundle.checks.length})` }, { key: "journal", label: `Journal (${bundle.journals.length})` }, { key: "bom", label: `BOM (${bundle.bom.length})` }] as item (item.key)}
					<button
						aria-selected={tab === item.key}
						class="ui-tab"
						class:active={tab === item.key}
						onclick={(): void => { tab = item.key as Tab }}
						role="tab"
						type="button"
					>{item.label}</button>
				{/each}
			</div>

			{#if tab === "overview"}
				<article class="ui-card padded overview">
					<h2>What the maker built</h2>
					<p class="description">{submission.description || "No description was sent."}</p>

					<div class="stats">
						<div><span>Claimed</span><strong>{formatHours(submission.hoursClaimedSeconds)}</strong></div>
						<div><span>Journals</span><strong>{formatHours(submission.hoursJournalSeconds)}</strong></div>
						<div><span>Hackatime</span><strong>{formatHours(submission.hoursHackatimeSeconds)}</strong></div>
						<div><span>BOM</span><strong>{formatMoney(submission.bomTotalCents)}</strong></div>
					</div>

					{#if bundle.priorReviews.length > 0}
						<div class="section">
							<h3>Prior review</h3>
							{#each bundle.priorReviews as prior (prior.at)}
								<div class="prior-row">
									<span class="ui-badge" data-tone={statusTone(prior.result.toLowerCase().includes("approved") ? "approved" : "changes")}>{prior.result}</span>
									<p>{prior.feedback}</p>
									<small>{prior.reviewer ?? "Reviewer"} · {formatDateTime(prior.at)}</small>
								</div>
							{/each}
						</div>
					{/if}

					<div class="section">
						<h3>Timeline</h3>
						{#each bundle.events as event (event.id)}
							<div class="event">
								<span class="dot"></span>
								<div>
									<strong>{event.kind}</strong>
									{#if event.actorEmail}<span> · {event.actorEmail}</span>{/if}
									<small>{formatDateTime(event.createdAt)}</small>
								</div>
							</div>
						{/each}
					</div>
				</article>
			{:else if tab === "checks"}
				{#if blocking.length > 0}
					<div class="ui-notice" data-tone="bad" role="alert">
						<strong>Approval is blocked</strong>
						{blocking[0].summary}. Ask the maker for changes or reject with a note.
					</div>
				{/if}

				{#if warnings.length > 0}
					<div class="ui-notice warn-box" data-tone="warn">
						<div>
							<strong>{warnings.length} warning{warnings.length === 1 ? "" : "s"}</strong>
							Acknowledge each one before approving. Warnings are held for the second pass.
						</div>
						{#if missingAcks.length > 0}
							<button class="ui-button sm" onclick={acknowledgeAll} type="button">Acknowledge all</button>
						{/if}
					</div>
				{/if}

				<div class="ui-card checks">
					{#each groups as group (group.group)}
						<div class="group">
							<h2>{group.group}</h2>
							{#each group.items as check (check.key)}
								<div class="check">
									<span class="ui-badge check-status" data-tone={statusTone(check.status)}>{check.status}</span>
									<span class="check-label">{check.label}</span>
									<code class="check-key ui-mono" title={check.key}>{check.key}</code>
									{#if isWarning(check)}
										<label class="ack" title="Acknowledge this warning">
											<input
												aria-label={`Acknowledge ${check.label}`}
												checked={acknowledgements.includes(check.key)}
												onchange={(): void => toggleAck(check.key)}
												type="checkbox"
											>
											<span>Ack</span>
										</label>
									{:else}
										<span class="ack-empty"></span>
									{/if}
									<span class="check-detail">
										<span class="check-summary">{check.summary}</span>
										{#if check.detail}<span class="check-note">{check.detail}</span>{/if}
									</span>
								</div>
							{/each}
						</div>
					{/each}
				</div>
			{:else if tab === "journal"}
				{#if bundle.journals.length === 0}
					<div class="ui-card ui-empty">
						<strong>No journal entries</strong>
						<span>Half Life sent no written evidence with this ship.</span>
					</div>
				{:else}
					<div class="journals">
						{#each bundle.journals as entry (entry.id)}
							<article class="ui-card padded journal">
								<div class="journal-head">
									<strong>{entry.title || "Work session"}</strong>
									<span>{formatHours(entry.seconds)} · {formatDateTime(entry.at)}</span>
								</div>
								{#if entry.clipCount > 0}
									<span class="ui-badge" data-tone="approved">{entry.clipCount} clip{entry.clipCount === 1 ? "" : "s"} · {formatHours(entry.coveredSeconds)} covered</span>
								{/if}
								<p>{entry.text || "No written entry."}</p>
								{#if entry.mediaCount > 0}
									<small>{entry.mediaCount} image{entry.mediaCount === 1 ? "" : "s"} attached</small>
								{/if}
							</article>
						{/each}
					</div>
				{/if}
			{:else if tab === "bom"}
				{#if bundle.bom.length === 0}
					<div class="ui-card ui-empty">
						<strong>No BOM lines</strong>
						<span>Half Life sent no priced parts with this ship.</span>
					</div>
				{:else}
					<div class="ui-card table-wrap">
						<table class="ui-table">
							<thead>
								<tr><th>Part</th><th>Purpose</th><th class="num">Qty</th><th class="num">Unit</th><th>Vendor</th></tr>
							</thead>
							<tbody>
								{#each bundle.bom as line (line.name)}
									<tr>
										<td>{#if line.link}<a href={line.link} rel="noreferrer" target="_blank">{line.name} ↗</a>{:else}{line.name}{/if}</td>
										<td>{line.purpose ?? "–"}</td>
										<td class="num">{line.quantity}</td>
										<td class="num">{formatMoney(line.unitCostCents)}</td>
										<td>{line.vendor ?? "–"}</td>
									</tr>
								{/each}
							</tbody>
							<tfoot>
								<tr><td colspan="3">Total</td><td class="num">{formatMoney(submission.bomTotalCents)}</td><td></td></tr>
							</tfoot>
						</table>
					</div>
				{/if}
			{/if}
		</section>

		<aside class="rail">
			<section class="ui-card padded summary">
				<div class="summary-head">
					<h2>Checks</h2>
					<span class="ui-badge" data-tone={statusTone(bundle.checks.length > 0 ? overall(bundle.checks) : "pass")}>
						{bundle.checks.length > 0 ? overall(bundle.checks) : "pass"}
					</span>
				</div>
				<div class="summary-counts">
					<span><strong>{bundle.checks.filter((check) => check.status === "pass").length}</strong> pass</span>
					<span><strong>{warnings.length}</strong> warn</span>
					<span><strong>{blocking.length}</strong> block</span>
				</div>
				<p class="policy">
					{#if bundle.policy.holdOnWarn === 1}
						Approvals with warnings are held for the second pass.
					{:else}
						Warnings can be approved with an acknowledgement.
					{/if}
					Fraud runs at stage {bundle.policy.fraudMinStage} only.
				</p>
			</section>

			<section class="ui-card padded decision">
				<h2>Decision</h2>

				<label class="field">
					<span>Approved hours</span>
					<input bind:value={hours} class="ui-input" max="720" min="0" step="0.25" type="number">
					<small>{formatHours(Math.round(hours * 3600))} of {formatHours(submission.hoursClaimedSeconds)} claimed</small>
				</label>

				<label class="field">
					<span>Note to maker {missingAcks.length > 0 || blocking.length > 0 ? "" : "(optional)"}</span>
					<textarea
						bind:this={noteElement}
						bind:value={note}
						class="ui-input"
						onblur={(): void => void saveDraftNow()}
						placeholder="What should they change, or why is this good?"
						rows="5"
					></textarea>
				</label>

				<label class="field">
					<span>Internal note</span>
					<textarea
						bind:value={internalNote}
						class="ui-input"
						onblur={(): void => void saveDraftNow()}
						placeholder="Only the team sees this."
						rows="2"
					></textarea>
				</label>

				{#if missingAcks.length > 0}
					<div class="ui-notice" data-tone="warn">
						<strong>{missingAcks.length} warning{missingAcks.length === 1 ? "" : "s"} left</strong>
						Acknowledge them in Checks to unlock approve.
						<button class="link" onclick={(): void => { tab = "checks" }} type="button">Open checks</button>
					</div>
				{/if}

				{#if blocking.length > 0}
					<div class="ui-notice" data-tone="bad">
						<strong>Approval disabled</strong>
						{blocking[0].summary}.
						<button class="link" onclick={(): void => { tab = "checks" }} type="button">Read the blocking check</button>
					</div>
				{/if}

				{#if actionError}
					<p class="ui-notice" data-tone="bad" role="alert">{actionError}</p>
				{/if}

				<div class="buttons">
					<button
						class="ui-button ok block"
						disabled={deciding || !canApprove}
						onclick={(): Promise<void> => decide("approved")}
						type="button"
					>
						{deciding ? "Saving…" : "Approve"} <kbd class="ui-kbd">A</kbd>
					</button>
					<div class="secondary">
						<button class="ui-button soft" disabled={deciding} onclick={(): Promise<void> => decide("changes")} type="button">
							Request changes
						</button>
						<button class="ui-button danger" disabled={deciding} onclick={(): Promise<void> => decide("rejected")} type="button">
							Reject
						</button>
					</div>
				</div>

				{#if bundle.claim}
					<p class="claim">Claimed by {bundle.claim.reviewerEmail} until {formatDateTime(bundle.claim.expiresAt)}</p>
				{/if}
			</section>
		</aside>
	</div>

	{#if toast}
		<div class="toast" role="status">{toast}</div>
	{/if}
{/if}

<style>
.loading {
	display: grid;
	gap: var(--space-3);
}

.loading div {
	height: 150px;
	animation: pulse 1.4s ease-in-out infinite;
}

.missing {
	max-width: 520px;
	padding-top: var(--space-5);
}

.missing h1 {
	font-size: var(--text-xl);
}

.missing p {
	padding: var(--space-2) 0 var(--space-4);
	font-size: var(--text-sm);
	color: var(--text-2);
}

.ship-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-4);
	align-items: flex-end;
	justify-content: space-between;
	padding-bottom: var(--space-4);
}

.head-main {
	min-width: 0;
}

.crumbs {
	padding-bottom: var(--space-1);
	font-size: var(--text-sm);
	color: var(--text-3);
}

.crumbs a {
	color: var(--text-2);
}

.crumbs a:hover {
	color: var(--primary);
}

.crumbs span {
	padding: 0 var(--space-1);
	color: var(--border-2);
}

.title-line {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
}

h1 {
	font-size: var(--text-2xl);
	overflow-wrap: anywhere;
}

.version {
	font-size: var(--text-sm);
	color: var(--text-3);
}

.maker {
	padding-top: var(--space-1);
	font-size: var(--text-sm);
	color: var(--text-2);
}

.maker span {
	padding-left: var(--space-1);
}

.head-links {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
}

.shortcuts {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-4);
	padding: var(--space-2) var(--space-3);
	margin-top: var(--space-3);
	font-size: var(--text-xs);
	color: var(--text-2);
	background: var(--surface-2);
	border-radius: var(--radius-md);
}

.layout {
	display: grid;
	grid-template-columns: minmax(0, 1fr) minmax(300px, 360px);
	gap: var(--space-4);
	align-items: start;
	padding-top: var(--space-4);
}

.content {
	display: flex;
	flex-direction: column;
	min-width: 0;
}

.content > .ui-card,
.content > .journals,
.content > .ui-notice {
	margin-top: var(--space-3);
}

.ui-tabs {
	margin-bottom: var(--space-1);
}

.overview h2,
.decision h2,
.summary h2,
.group h2 {
	font-size: var(--text-md);
	font-weight: 700;
}

.description {
	padding-top: var(--space-2);
	font-size: var(--text-sm);
	color: var(--text-2);
	white-space: pre-wrap;
}

.stats {
	display: grid;
	grid-template-columns: repeat(4, minmax(0, 1fr));
	gap: var(--space-1);
	padding-top: var(--space-4);
}

.stats div {
	padding: var(--space-2) var(--space-3);
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.stats span {
	display: block;
	font-size: var(--text-xs);
	color: var(--text-3);
}

.stats strong {
	font-size: var(--text-md);
	font-variant-numeric: tabular-nums;
}

.section {
	padding-top: var(--space-4);
	margin-top: var(--space-4);
	border-top: 1px solid var(--border);
}

.section h3 {
	padding-bottom: var(--space-2);
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-3);
}

.prior-row {
	padding: var(--space-2) var(--space-3);
	margin-top: var(--space-2);
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.prior-row p {
	padding-top: var(--space-1);
	font-size: var(--text-sm);
	color: var(--text-2);
}

.prior-row small {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.event {
	display: flex;
	gap: var(--space-2);
	padding: var(--space-1) 0;
	font-size: var(--text-sm);
}

.event .dot {
	flex: none;
	width: 6px;
	height: 6px;
	margin-top: 7px;
	background: var(--primary);
	border-radius: 50%;
}

.event small {
	display: block;
	font-size: var(--text-xs);
	color: var(--text-3);
}

.warn-box {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-3);
	align-items: center;
	justify-content: space-between;
}

.checks {
	overflow: hidden;
}

.group + .group {
	border-top: 1px solid var(--border);
}

.group h2 {
	padding: var(--space-2) var(--space-4);
	font-size: var(--text-xs);
	color: var(--text-3);
	background: var(--surface-2);
}

.check {
	display: grid;
	grid-template-columns: 58px 190px minmax(0, 1fr) 64px;
	column-gap: var(--space-3);
	row-gap: var(--space-1);
	align-items: start;
	padding: var(--space-2) var(--space-4);
	border-top: 1px solid var(--border);
}

.group h2 + .check {
	border-top: 0;
}

.check-status {
	justify-self: start;
}

.check-label {
	font-size: var(--text-sm);
	font-weight: 700;
}

.check-key {
	overflow: hidden;
	padding-top: 2px;
	font-size: var(--text-xs);
	color: var(--text-3);
	text-overflow: ellipsis;
	white-space: nowrap;
}

.check-detail {
	display: flex;
	flex-direction: column;
	grid-column: 1 / -1;
	gap: 1px;
}

.check-summary {
	font-size: var(--text-sm);
	color: var(--text-2);
}

.check-note {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.ack {
	display: flex;
	gap: var(--space-1);
	align-items: center;
	justify-self: end;
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--status-pending);
	cursor: pointer;
}

.ack input {
	accent-color: var(--primary);
}

.journals {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.journal-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: baseline;
	justify-content: space-between;
}

.journal-head span {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.journal p {
	padding-top: var(--space-2);
	font-size: var(--text-sm);
	color: var(--text-2);
	white-space: pre-wrap;
}

.journal small {
	display: block;
	padding-top: var(--space-1);
	font-size: var(--text-xs);
	color: var(--text-3);
}

.journal .ui-badge {
	margin-top: var(--space-2);
}

.table-wrap {
	overflow-x: auto;
}

.rail {
	position: sticky;
	top: 96px;
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	padding-top: var(--space-4);
}

.summary-head {
	display: flex;
	gap: var(--space-2);
	align-items: center;
	justify-content: space-between;
}

.summary-counts {
	display: flex;
	gap: var(--space-4);
	padding-top: var(--space-2);
	font-size: var(--text-sm);
	color: var(--text-3);
}

.summary-counts strong {
	color: var(--text);
	font-variant-numeric: tabular-nums;
}

.policy {
	padding-top: var(--space-2);
	margin-top: var(--space-3);
	font-size: var(--text-xs);
	color: var(--text-3);
	border-top: 1px solid var(--border);
}

.field {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	padding-top: var(--space-3);
}

.field > span {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-2);
}

.field small {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.decision .ui-notice {
	margin-top: var(--space-3);
}

.link {
	display: block;
	padding: var(--space-1) 0 0;
	font-size: var(--text-xs);
	font-weight: 700;
	color: inherit;
	text-decoration: underline;
	background: none;
	border: 0;
}

.buttons {
	display: grid;
	gap: var(--space-2);
	padding-top: var(--space-4);
}

.secondary {
	display: grid;
	grid-template-columns: 1fr 1fr;
	gap: var(--space-2);
}

.claim {
	padding-top: var(--space-3);
	font-size: var(--text-xs);
	color: var(--text-3);
}

.toast {
	position: fixed;
	bottom: var(--space-5);
	left: 50%;
	padding: var(--space-2) var(--space-4);
	font-size: var(--text-sm);
	font-weight: 700;
	color: var(--bg);
	background: var(--text);
	border-radius: var(--radius-full);
	box-shadow: var(--shadow-lg);
	transform: translateX(-50%);
}

@keyframes pulse {
	50% {
		opacity: 0.55;
	}
}

@media (max-width: 980px) {
	.layout {
		grid-template-columns: 1fr;
	}

	.rail {
		position: static;
		padding-top: 0;
	}
}

@media (max-width: 760px) {
	.check {
		grid-template-columns: 54px minmax(0, 1fr) 56px;
		grid-template-areas:
			"status label ack"
			"key key key"
			"detail detail detail";
		row-gap: var(--space-1);
	}

	.check-status {
		grid-area: status;
	}

	.check-label {
		grid-area: label;
	}

	.check-key {
		grid-area: key;
	}

	.check-detail {
		grid-area: detail;
	}

	.ack,
	.ack-empty {
		grid-area: ack;
	}
}

@media (max-width: 640px) {
	.stats {
		grid-template-columns: repeat(2, minmax(0, 1fr));
	}

	.ship-head {
		flex-direction: column;
		align-items: flex-start;
	}

	.check,
	.group h2 {
		padding-inline: var(--space-3);
	}
}
</style>
