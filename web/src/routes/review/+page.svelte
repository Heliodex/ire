<script lang="ts">
import { goto } from "$app/navigation"
import { page } from "$app/state"
import { onMount } from "svelte"
import { ApiError } from "../../lib/api-error"
import {
	claimShip,
	decideShip,
	fetchBundle,
	fetchMe,
	heartbeat,
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

const submission = $derived(bundle?.submission ?? null)
const warnings = $derived(bundle?.checks.filter(isWarning) ?? [])
const blocking = $derived(bundle?.checks.filter(isBlocking) ?? [])
const missingAcks = $derived(warnings.filter((check) => !acknowledgements.includes(check.key)))
const canApprove = $derived(blocking.length === 0 && missingAcks.length === 0)
const groups = $derived(groupChecks(bundle?.checks ?? []))

onMount((): (() => void) => {
	id = page.url.searchParams.get("id") ?? ""
	if (id === "") {
		void goto("/queue")
		return (): void => {}
	}

	void boot()

	const beat = setInterval((): void => void heartbeat(id).catch(() => {}), 60000)

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

async function load(): Promise<void> {
	try {
		bundle = await fetchBundle(id)
		const claim = bundle.claim
		if (!claim || claim.isMine !== 1) {
			await claimShip(id)
			bundle = await fetchBundle(id)
		}

		hourInit()
		error = ""
	} catch (err) {
		if (err instanceof ApiError && err.code === "unauthorized") {
			void goto("/")
			return
		}
		error = err instanceof Error ? err.message : "Could not open this ship."
	} finally {
		loading = false
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
		const result = await decideShip(id, {
			decision,
			version: bundle.submission.version,
			noteToMaker: note.trim(),
			internalNote: internalNote.trim(),
			hoursApprovedSeconds: Math.max(0, Math.round(hours * 3600)),
			acknowledgements,
			idempotencyKey: `dec_${crypto.randomUUID()}`,
		})

		if (result.held === 1) {
			toast = `Held for the second pass: ${result.heldReason ?? "policy"}.`
		} else {
			const verb = decision === "approved" ? "Approved" : decision === "changes" ? "Changes requested" : "Rejected"
			toast = `${verb} and sent${result.deliveryStatus ? ` (${result.deliveryStatus})` : ""}.`
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
		<div></div>
		<div></div>
	</div>
{:else if error || !bundle || !submission}
	<section class="missing">
		<p class="eyebrow">Cannot open this ship</p>
		<h1>Back to the queue.</h1>
		<p>{error || "This ship does not exist or is not yours to review."}</p>
		<a class="button primary" href="/queue">Return to queue</a>
	</section>
{:else}
	<header class="ship-head">
		<div class="head-main">
			<p class="eyebrow">
				<a href="/queue">Queue</a>
				<span>/</span>
				{humanStatus(submission.status)}
				{#if submission.theme}<span>/</span>{submission.theme}{/if}
			</p>
			<div class="title-line">
				<h1>{submission.title}</h1>
				<span class="status {statusTone(submission.status)}">{humanStatus(submission.status)}</span>
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
				<a class="button ghost" href={submission.repoUrl} rel="noreferrer" target="_blank">Repository ↗</a>
			{/if}
			{#if submission.demoUrl}
				<a class="button ghost" href={submission.demoUrl} rel="noreferrer" target="_blank">Demo ↗</a>
			{/if}
			<button class="button ghost" onclick={(): void => { showShortcuts = !showShortcuts }} type="button">Shortcuts <kbd>?</kbd></button>
		</div>
	</header>

	{#if showShortcuts}
		<div class="shortcuts">
			<span><kbd>A</kbd> approve</span>
			<span><kbd>C</kbd> focus the note</span>
			<span><kbd>R</kbd> reject</span>
			<span><kbd>Esc</kbd> back to queue</span>
		</div>
	{/if}

	<div class="layout">
		<section class="content">
			<div class="tabs" role="tablist" aria-label="Ship sections">
				{#each [{ key: "overview", label: "Overview" }, { key: "checks", label: `Checks (${bundle.checks.length})` }, { key: "journal", label: `Journal (${bundle.journals.length})` }, { key: "bom", label: `BOM (${bundle.bom.length})` }] as item (item.key)}
					<button
						aria-selected={tab === item.key}
						class:active={tab === item.key}
						onclick={(): void => { tab = item.key as Tab }}
						role="tab"
						type="button"
					>{item.label}</button>
				{/each}
			</div>

			{#if tab === "overview"}
				<article class="card prose">
					<h2>What the maker built</h2>
					<p>{submission.description || "No description was sent."}</p>

					<div class="stats">
						<div><span>Claimed</span><strong>{formatHours(submission.hoursClaimedSeconds)}</strong></div>
						<div><span>Journals</span><strong>{formatHours(submission.hoursJournalSeconds)}</strong></div>
						<div><span>Hackatime</span><strong>{formatHours(submission.hoursHackatimeSeconds)}</strong></div>
						<div><span>BOM</span><strong>{formatMoney(submission.bomTotalCents)}</strong></div>
					</div>

					{#if bundle.priorReviews.length > 0}
						<div class="prior">
							<h3>Prior review</h3>
							{#each bundle.priorReviews as prior (prior.at)}
								<div class="prior-row">
									<span class="status {statusTone(prior.result.toLowerCase().includes("approved") ? "approved" : "changes")}">{prior.result}</span>
									<p>{prior.feedback}</p>
									<small>{prior.reviewer ?? "Reviewer"} · {formatDateTime(prior.at)}</small>
								</div>
							{/each}
						</div>
					{/if}

					<div class="timeline">
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
					<div class="blocking" role="alert">
						<strong>Approval is blocked</strong>
						<p>{blocking[0].summary}. Ask the maker for changes or reject with a note.</p>
					</div>
				{/if}

				{#if warnings.length > 0}
					<div class="warn-box">
						<div>
							<strong>{warnings.length} warning{warnings.length === 1 ? "" : "s"}</strong>
							<p>Acknowledge each one before approving. Warnings are held for the second pass.</p>
						</div>
						{#if missingAcks.length > 0}
							<button class="button ghost" onclick={acknowledgeAll} type="button">Acknowledge all</button>
						{/if}
					</div>
				{/if}

				{#each groups as group (group.group)}
					<section class="card check-group">
						<h2>{group.group}</h2>
						<div class="check-list">
							{#each group.items as check (check.key)}
								<div class="check {statusTone(check.status)}">
									<div class="check-head">
										<span class="status {statusTone(check.status)}">{check.status}</span>
										<strong>{check.label}</strong>
										<code>{check.key}</code>
									</div>
									<p>{check.summary}</p>
									{#if check.detail}<small>{check.detail}</small>{/if}
									{#if isWarning(check)}
										<label class="ack">
											<input
												checked={acknowledgements.includes(check.key)}
												onchange={(): void => toggleAck(check.key)}
												type="checkbox"
											>
											<span>Acknowledge this warning</span>
										</label>
									{/if}
								</div>
							{/each}
						</div>
					</section>
				{/each}
			{:else if tab === "journal"}
				{#if bundle.journals.length === 0}
					<div class="empty">No journal entries were sent.</div>
				{:else}
					<div class="journals">
						{#each bundle.journals as entry (entry.id)}
							<article class="card journal">
								<div class="journal-head">
									<strong>{entry.title || "Work session"}</strong>
									<span>{formatHours(entry.seconds)} · {formatDateTime(entry.at)}</span>
								</div>
								{#if entry.clipCount > 0}
									<span class="clip">{entry.clipCount} clip{entry.clipCount === 1 ? "" : "s"} · {formatHours(entry.coveredSeconds)} covered</span>
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
					<div class="empty">No BOM lines were sent.</div>
				{:else}
					<div class="card table-wrap">
						<table>
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
			<section class="card summary">
				<div class="summary-head">
					<h2>Checks</h2>
					<span class="status {statusTone(bundle.checks.length > 0 ? overall(bundle.checks) : "pass")}">
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

			<section class="card decision">
				<h2>Decision</h2>

				<label class="field">
					<span>Approved hours</span>
					<input bind:value={hours} max="720" min="0" step="0.25" type="number">
					<small>{formatHours(Math.round(hours * 3600))} of {formatHours(submission.hoursClaimedSeconds)} claimed</small>
				</label>

				<label class="field">
					<span>Note to maker {missingAcks.length > 0 || blocking.length > 0 ? "" : "(optional)"}</span>
					<textarea
						bind:this={noteElement}
						bind:value={note}
						onblur={(): void => void saveDraftNow()}
						placeholder="What should they change, or why is this good?"
						rows="5"
					></textarea>
				</label>

				<label class="field">
					<span>Internal note</span>
					<textarea
						bind:value={internalNote}
						onblur={(): void => void saveDraftNow()}
						placeholder="Only the team sees this."
						rows="2"
					></textarea>
				</label>

				{#if missingAcks.length > 0}
					<div class="gate warn">
						<strong>{missingAcks.length} warning{missingAcks.length === 1 ? "" : "s"} left</strong>
						<p>Acknowledge them in Checks to unlock approve.</p>
						<button class="link" onclick={(): void => { tab = "checks" }} type="button">Open checks</button>
					</div>
				{/if}

				{#if blocking.length > 0}
					<div class="gate bad">
						<strong>Approval disabled</strong>
						<p>{blocking[0].summary}.</p>
						<button class="link" onclick={(): void => { tab = "checks" }} type="button">Read the blocking check</button>
					</div>
				{/if}

				{#if actionError}
					<p class="action-error" role="alert">{actionError}</p>
				{/if}

				<div class="buttons">
					<button
						class="button approve"
						disabled={deciding || !canApprove}
						onclick={(): Promise<void> => decide("approved")}
						type="button"
					>
						{deciding ? "Saving…" : "Approve"} <kbd>A</kbd>
					</button>
					<div class="secondary">
						<button class="button changes" disabled={deciding} onclick={(): Promise<void> => decide("changes")} type="button">
							Request changes
						</button>
						<button class="button reject" disabled={deciding} onclick={(): Promise<void> => decide("rejected")} type="button">
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
	gap: 12px;
	padding-top: 20px;
}

.loading div {
	height: 150px;
	background: linear-gradient(90deg, var(--card), var(--paper-deep), var(--card));
	background-size: 200% 100%;
	border: 1px solid var(--line);
	border-radius: var(--radius);
	animation: shimmer 1.4s infinite;
}

.missing {
	max-width: 620px;
	padding: 40px 0;
}

.missing .eyebrow {
	padding-bottom: 10px;
	font-size: 0.68rem;
	font-weight: 750;
	color: var(--accent);
	text-transform: uppercase;
	letter-spacing: 0.12em;
}

.missing h1 {
	font-size: 2.2rem;
}

.missing p {
	padding: 10px 0 20px;
	color: var(--muted);
}

.ship-head {
	display: flex;
	flex-wrap: wrap;
	gap: 20px;
	align-items: flex-end;
	justify-content: space-between;
	padding-bottom: 18px;
	border-bottom: 1px solid var(--line);
}

.head-main {
	min-width: 0;
}

.eyebrow {
	padding-bottom: 8px;
	font-size: 0.68rem;
	font-weight: 700;
	color: var(--muted);
	text-transform: uppercase;
	letter-spacing: 0.09em;
}

.eyebrow a {
	color: var(--accent);
}

.eyebrow span {
	padding: 0 6px;
	color: var(--line);
}

.title-line {
	display: flex;
	flex-wrap: wrap;
	gap: 10px;
	align-items: baseline;
}

h1 {
	font-size: clamp(1.8rem, 3.6vw, 2.7rem);
	overflow-wrap: anywhere;
}

.status {
	padding: 3px 8px;
	font-size: 0.62rem;
	font-weight: 750;
	text-transform: uppercase;
	letter-spacing: 0.06em;
	border-radius: 999px;
}

.status.good {
	color: var(--teal);
	background: var(--teal-soft);
}

.status.warn {
	color: var(--accent-deep);
	background: var(--accent-soft);
}

.status.bad,
.status.error {
	color: #7d2a12;
	background: #fbe5df;
}

.status.neutral {
	color: var(--muted);
	background: var(--paper-deep);
}

.version {
	font-size: 0.72rem;
	color: var(--muted);
}

.maker {
	padding-top: 5px;
	font-size: 0.78rem;
	color: var(--muted);
}

.maker span {
	padding-left: 6px;
}

.head-links {
	display: flex;
	flex-wrap: wrap;
	gap: 8px;
}

.button {
	display: inline-flex;
	gap: 6px;
	align-items: center;
	justify-content: center;
	padding: 9px 14px;
	font-size: 0.8rem;
	font-weight: 750;
	color: var(--ink);
	text-decoration: none;
	background: var(--card);
	border: 1px solid var(--line);
	border-radius: 9px;
}

.button:hover {
	text-decoration: none;
	border-color: var(--ink);
}

.button:disabled {
	cursor: not-allowed;
	opacity: 0.55;
}

.button.primary {
	color: #fff;
	background: var(--accent);
	border-color: var(--accent);
}

.button.ghost {
	font-size: 0.74rem;
}

.button.approve {
	color: #fff;
	background: var(--teal);
	border-color: var(--teal);
}

.button.approve:hover:not(:disabled) {
	background: #174f4c;
}

.button.changes {
	color: var(--accent-deep);
	background: var(--accent-soft);
	border-color: #ecc3b4;
}

.button.reject {
	color: #7d2a12;
	background: #fbe5df;
	border-color: #e9b9aa;
}

kbd {
	padding: 1px 5px;
	font-family: inherit;
	font-size: 0.64rem;
	font-weight: 700;
	background: rgba(255, 255, 255, 0.16);
	border: 1px solid rgba(255, 255, 255, 0.28);
	border-radius: 4px;
}

.button.ghost kbd,
.shortcuts kbd {
	background: var(--paper-deep);
	border-color: var(--line);
}

.shortcuts {
	display: flex;
	flex-wrap: wrap;
	gap: 14px;
	padding: 10px 13px;
	margin-top: 12px;
	font-size: 0.74rem;
	color: var(--muted);
	background: var(--card);
	border: 1px solid var(--line);
	border-radius: 10px;
}

.layout {
	display: grid;
	grid-template-columns: minmax(0, 1fr) minmax(300px, 360px);
	gap: 22px;
	align-items: start;
	padding-top: 20px;
}

.content {
	min-width: 0;
}

.tabs {
	display: flex;
	flex-wrap: wrap;
	gap: 3px;
	padding: 4px;
	margin-bottom: 14px;
	background: var(--paper-deep);
	border-radius: 999px;
}

.tabs button {
	padding: 7px 13px;
	font-size: 0.76rem;
	font-weight: 700;
	color: var(--muted);
	background: transparent;
	border: 0;
	border-radius: 999px;
}

.tabs button.active {
	color: var(--ink);
	background: var(--card);
	box-shadow: 0 2px 8px rgba(60, 45, 20, 0.08);
}

.card {
	padding: 20px;
	background: var(--card);
	border: 1px solid var(--line);
	border-radius: var(--radius);
	box-shadow: 0 5px 18px rgba(60, 45, 20, 0.04);
}

.prose h2,
.check-group h2,
.summary h2,
.decision h2 {
	font-size: 1.1rem;
}

.prose > p {
	padding-top: 10px;
	font-size: 0.9rem;
	color: #3b4441;
	white-space: pre-wrap;
}

.stats {
	display: grid;
	grid-template-columns: repeat(4, minmax(0, 1fr));
	gap: 8px;
	padding-top: 18px;
}

.stats div {
	padding: 10px;
	background: var(--paper);
	border-radius: 10px;
}

.stats span {
	display: block;
	font-size: 0.62rem;
	color: var(--muted);
	text-transform: uppercase;
	letter-spacing: 0.07em;
}

.stats strong {
	font-size: 1.02rem;
	font-variant-numeric: tabular-nums;
}

.prior,
.timeline {
	padding-top: 20px;
	margin-top: 20px;
	border-top: 1px solid var(--line);
}

.prior h3,
.timeline h3 {
	padding-bottom: 10px;
	font-size: 0.78rem;
	color: var(--muted);
	text-transform: uppercase;
	letter-spacing: 0.08em;
}

.prior-row {
	padding: 10px 12px;
	margin-bottom: 8px;
	background: var(--paper);
	border-radius: 9px;
}

.prior-row p {
	padding-top: 6px;
	font-size: 0.82rem;
}

.prior-row small {
	font-size: 0.7rem;
	color: var(--muted);
}

.event {
	display: flex;
	gap: 10px;
	padding: 5px 0;
	font-size: 0.78rem;
}

.event .dot {
	width: 7px;
	height: 7px;
	margin-top: 6px;
	background: var(--accent);
	border-radius: 50%;
}

.event small {
	display: block;
	font-size: 0.68rem;
	color: var(--muted);
}

.blocking {
	padding: 14px 16px;
	margin-bottom: 12px;
	color: #7d2a12;
	background: #fbe5df;
	border: 1px solid #e9b9aa;
	border-radius: 12px;
}

.blocking p {
	padding-top: 4px;
	font-size: 0.82rem;
}

.warn-box {
	display: flex;
	flex-wrap: wrap;
	gap: 14px;
	align-items: center;
	justify-content: space-between;
	padding: 13px 15px;
	margin-bottom: 12px;
	color: var(--accent-deep);
	background: var(--accent-soft);
	border: 1px solid #ecc3b4;
	border-radius: 12px;
}

.warn-box p {
	padding-top: 3px;
	font-size: 0.8rem;
}

.check-group {
	margin-bottom: 12px;
}

.check-list {
	display: grid;
	gap: 8px;
	padding-top: 12px;
}

.check {
	padding: 12px;
	background: var(--paper);
	border-left: 3px solid var(--line);
	border-radius: 8px;
}

.check.good {
	border-left-color: var(--teal);
}

.check.warn {
	border-left-color: var(--accent);
}

.check.bad,
.check.error {
	border-left-color: #b23a1d;
}

.check-head {
	display: flex;
	flex-wrap: wrap;
	gap: 8px;
	align-items: baseline;
}

.check-head strong {
	font-size: 0.84rem;
}

.check-head code {
	font-size: 0.66rem;
	color: var(--muted);
}

.check p {
	padding-top: 5px;
	font-size: 0.8rem;
}

.check small {
	display: block;
	padding-top: 3px;
	font-size: 0.72rem;
	color: var(--muted);
}

.ack {
	display: flex;
	gap: 7px;
	align-items: center;
	padding-top: 9px;
	font-size: 0.76rem;
	font-weight: 650;
	color: var(--accent-deep);
}

.ack input {
	accent-color: var(--accent);
}

.empty {
	padding: 32px;
	text-align: center;
	color: var(--muted);
	background: var(--card);
	border: 1px dashed var(--line);
	border-radius: var(--radius);
}

.journals {
	display: grid;
	gap: 10px;
}

.journal-head {
	display: flex;
	flex-wrap: wrap;
	gap: 10px;
	align-items: baseline;
	justify-content: space-between;
}

.journal-head span {
	font-size: 0.72rem;
	color: var(--muted);
}

.journal p {
	padding-top: 8px;
	font-size: 0.85rem;
	color: #3b4441;
	white-space: pre-wrap;
}

.journal small {
	display: block;
	padding-top: 6px;
	font-size: 0.7rem;
	color: var(--muted);
}

.clip {
	display: inline-block;
	padding: 2px 7px;
	margin-top: 6px;
	font-size: 0.66rem;
	font-weight: 700;
	color: var(--teal);
	background: var(--teal-soft);
	border-radius: 999px;
}

.table-wrap {
	overflow-x: auto;
}

table {
	width: 100%;
	font-size: 0.82rem;
	border-collapse: collapse;
}

th,
td {
	padding: 9px 10px;
	text-align: left;
	border-bottom: 1px solid var(--line);
}

th {
	font-size: 0.66rem;
	color: var(--muted);
	text-transform: uppercase;
	letter-spacing: 0.07em;
}

.num {
	text-align: right;
	font-variant-numeric: tabular-nums;
}

tfoot td {
	font-weight: 750;
	border-bottom: 0;
}

.rail {
	display: grid;
	gap: 14px;
	position: sticky;
	top: 14px;
}

.summary-head {
	display: flex;
	gap: 10px;
	align-items: baseline;
	justify-content: space-between;
}

.summary-counts {
	display: flex;
	gap: 12px;
	padding-top: 10px;
	font-size: 0.76rem;
	color: var(--muted);
}

.summary-counts strong {
	color: var(--ink);
	font-variant-numeric: tabular-nums;
}

.policy {
	padding-top: 12px;
	margin-top: 12px;
	font-size: 0.74rem;
	color: var(--muted);
	border-top: 1px solid var(--line);
}

.field {
	display: flex;
	flex-direction: column;
	gap: 5px;
	padding-top: 14px;
}

.field > span {
	font-size: 0.66rem;
	font-weight: 700;
	color: var(--muted);
	text-transform: uppercase;
	letter-spacing: 0.08em;
}

.field input,
.field textarea {
	width: 100%;
	padding: 9px 10px;
	font-size: 0.82rem;
	color: var(--ink);
	resize: vertical;
	background: #fff;
	border: 1px solid var(--line);
	border-radius: 9px;
	outline: none;
}

.field input:focus,
.field textarea:focus {
	border-color: var(--accent);
	box-shadow: 0 0 3px var(--accent-soft);
}

.field small {
	font-size: 0.68rem;
	color: var(--muted);
}

.gate {
	padding: 10px 12px;
	margin-top: 12px;
	border-radius: 9px;
}

.gate p {
	padding-top: 2px;
	font-size: 0.74rem;
}

.gate.warn {
	color: var(--accent-deep);
	background: var(--accent-soft);
}

.gate.bad {
	color: #7d2a12;
	background: #fbe5df;
}

.link {
	padding: 5px 0 0;
	font-size: 0.74rem;
	font-weight: 750;
	color: inherit;
	text-decoration: underline;
	background: none;
	border: 0;
}

.action-error {
	padding: 9px 11px;
	margin-top: 12px;
	font-size: 0.78rem;
	color: #7d2a12;
	background: #fbe5df;
	border: 1px solid #e9b9aa;
	border-radius: 9px;
}

.buttons {
	display: grid;
	gap: 8px;
	padding-top: 14px;
}

.secondary {
	display: grid;
	grid-template-columns: 1fr 1fr;
	gap: 8px;
}

.claim {
	padding-top: 12px;
	font-size: 0.7rem;
	color: var(--muted);
}

.toast {
	position: fixed;
	bottom: 24px;
	left: 50%;
	padding: 11px 16px;
	font-size: 0.82rem;
	font-weight: 700;
	color: var(--card);
	background: var(--ink);
	border-radius: 10px;
	box-shadow: 0 10px 30px rgba(30, 38, 39, 0.25);
	transform: translateX(-50%);
}

@keyframes shimmer {
	to {
		background-position: -200% 0;
	}
}

@media (max-width: 980px) {
	.layout {
		grid-template-columns: 1fr;
	}

	.rail {
		position: static;
	}
}

@media (max-width: 680px) {
	.stats {
		grid-template-columns: repeat(2, minmax(0, 1fr));
	}

	.ship-head {
		flex-direction: column;
		align-items: flex-start;
	}
}
</style>
