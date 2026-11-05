<script lang="ts">
import { onMount } from "svelte"
import { afterNavigate, goto } from "$app/navigation"
import { page } from "$app/state"
import {
	type Bundle,
	type Check,
	claimShip,
	decideShip,
	fetchBundle,
	fetchCheckHead,
	fetchMe,
	heartbeat,
	program,
	refreshRepositoryEvidence,
	releaseShip,
	rerunChecks,
	saveDraft,
	sendShipBack,
} from "../../lib/api"
import { ApiError } from "../../lib/api-error"
import { ignore, sleep } from "../../lib/async"
import { isBlocking, isWarning, overallChecks } from "../../lib/checks"
import ChecksTab from "../../lib/components/review/ChecksTab.svelte"
import EvidenceTab from "../../lib/components/review/EvidenceTab.svelte"
import HistoryTab from "../../lib/components/review/HistoryTab.svelte"
import TimeEvidenceBar from "../../lib/components/review/TimeEvidenceBar.svelte"
import { maxNote } from "../../lib/contract"
import { formatDateTime, formatHours, formatMoney, formatMoneyAmount, humanStatus, statusTone } from "../../lib/format"
import { session } from "../../lib/session.svelte.js"

const tabKeys = ["overview", "checks", "journal", "bom", "evidence", "history"] as const
type Tab = (typeof tabKeys)[number]

interface TimeSegment {
	label: string
	tone: "journals" | "lapse" | "hackatime" | "cut"
	value: number
}

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
let refreshingEvidence = $state(false)
let evidenceError = $state("")
let overrideReason = $state("")
let overrideElement = $state<HTMLTextAreaElement | null>(null)
let sendBackReason = $state("")
let sendBackError = $state("")
let sendingBack = $state(false)
let rerunning = $state(false)
let rerunError = $state("")
let checksOutdated = $state(false)
let coverFailed = $state(false)
let checksTimer: ReturnType<typeof setTimeout> | null = null

interface CachedBundle {
	bundle: Bundle
	savedAt: number
	version: number
}

interface BundleCacheIndexEntry {
	id: string
	savedAt: number
}

const bundleCacheVersion = 1
const maxCachedBundles = 3
const maxCachedBundleBytes = 1_500_000

// bundleCacheKey scopes cached ships to the signed-in reviewer.
function bundleCacheKey(target: string): string {
	return `review:bundle:${program}:${session.me?.user.email ?? "anonymous"}:${target}`
}

function bundleCacheIndexKey(): string {
	return `review:bundle:${program}:${session.me?.user.email ?? "anonymous"}:index`
}

// readBundleCacheIndex lists cached ships, newest first, so old entries can be evicted.
function readBundleCacheIndex(): BundleCacheIndexEntry[] {
	try {
		const raw = sessionStorage.getItem(bundleCacheIndexKey())
		if (!raw) return []

		const entries = JSON.parse(raw) as BundleCacheIndexEntry[]
		if (!Array.isArray(entries)) return []

		return entries.filter(entry => entry && typeof entry.id === "string" && typeof entry.savedAt === "number")
	} catch {
		return []
	}
}

// readBundleCache returns a recently saved copy so the same ship opens without waiting for the network.
function readBundleCache(target: string): Bundle | null {
	try {
		const raw = sessionStorage.getItem(bundleCacheKey(target))
		if (!raw) return null

		const cached = JSON.parse(raw) as CachedBundle
		if (cached.version !== bundleCacheVersion) return null
		if (typeof cached.savedAt !== "number" || Date.now() - cached.savedAt > 24 * 60 * 60 * 1000) return null
		if (cached.bundle?.submission?.id !== target) return null

		return cached.bundle
	} catch {
		return null
	}
}

// writeBundleCache keeps the last few ships and skips oversized bundles that would fill the tab's storage.
function writeBundleCache(target: string, next: Bundle): void {
	try {
		const savedAt = Date.now()
		const payload = JSON.stringify({ bundle: next, savedAt, version: bundleCacheVersion } satisfies CachedBundle)

		if (payload.length > maxCachedBundleBytes) {
			sessionStorage.removeItem(bundleCacheKey(target))
			return
		}

		const index = [{ id: target, savedAt }, ...readBundleCacheIndex().filter(entry => entry.id !== target)]
		for (const entry of index.slice(maxCachedBundles)) sessionStorage.removeItem(bundleCacheKey(entry.id))

		sessionStorage.setItem(bundleCacheKey(target), payload)
		sessionStorage.setItem(bundleCacheIndexKey(), JSON.stringify(index.slice(0, maxCachedBundles)))
	} catch {
		// Storage can be unavailable or full; the page still works without a cache.
	}
}

// removeBundleCache drops a copy the reviewer can no longer open.
function removeBundleCache(target: string): void {
	try {
		sessionStorage.removeItem(bundleCacheKey(target))
		sessionStorage.setItem(
			bundleCacheIndexKey(),
			JSON.stringify(readBundleCacheIndex().filter(entry => entry.id !== target)),
		)
	} catch {
		// Storage can be unavailable; the next read will simply miss.
	}
}

const submission = $derived(bundle?.submission ?? null)
const reviewed = $derived(submission?.status === "decided" || submission?.status === "withdrawn")
const secondPass = $derived(!reviewed && (submission?.stage === 2 || submission?.status === "stage2_hold"))
const role = $derived(session.me?.programs.find(item => item.programSlug === program)?.role ?? "")
const canSecondPass = $derived(role === "organizer" || role === "admin")
const checking = $derived(submission?.status === "checking")
const readOnly = $derived(session.me?.readOnly ?? false)
const canDecide = $derived(!readOnly && !reviewed && !checking && (!secondPass || canSecondPass))
const heldReview = $derived(bundle?.reviews.find(review => review.held === 1) ?? null)
const warnings = $derived(bundle?.checks.filter(isWarning) ?? [])
const blocking = $derived(bundle?.checks.filter(isBlocking) ?? [])
const missingAcks = $derived(warnings.filter(check => !acknowledgements.includes(check.key)))
const canApprove = $derived(!reviewed && blocking.length === 0 && (secondPass || missingAcks.length === 0))
const approvedSeconds = $derived(Math.max(0, Math.round((Number(hours) || 0) * 3600)))
const timeSegments = $derived.by((): TimeSegment[] => {
	const claimed = submission?.hoursClaimedSeconds ?? 0
	const timelapsed = (bundle?.journals ?? []).reduce((sum, entry) => sum + entry.coveredSeconds, 0)
	const segments: TimeSegment[] = [
		{ label: "Journalled", tone: "journals", value: submission?.hoursJournalSeconds ?? 0 },
		{ label: "Timelapsed", tone: "lapse", value: timelapsed },
		{ label: "Hackatimed", tone: "hackatime", value: submission?.hoursHackatimeSeconds ?? 0 },
		{ label: "Deflated", tone: "cut", value: Math.max(0, claimed - approvedSeconds) },
	]

	return segments.filter(segment => segment.value > 0)
})
const backPath = $derived(page.url.searchParams.get("from") === "reviewed" ? "/reviewed" : "/queue")
const backLabel = $derived(backPath === "/reviewed" ? "Reviewed" : "Queue")

// idFromURL reads the ship id from /review/{id}, keeping the query form for old links.
function idFromURL(): string {
	const parts = page.url.pathname.split("/").filter(Boolean)
	if (parts[0] === "review" && parts[1]) return decodeURIComponent(parts[1])

	return page.url.searchParams.get("id") ?? ""
}

// isTab narrows a query value to a section tab.
function isTab(value: string): value is Tab {
	return tabKeys.includes(value as Tab)
}

// tabFromURL restores the section from ?tab=, falling back to the overview.
function tabFromURL(): Tab {
	const requested = page.url.searchParams.get("tab") ?? ""

	return isTab(requested) ? requested : "overview"
}

// selectTab keeps the section in the URL for reloads and links; review paths are outside the SvelteKit router.
function selectTab(next: Tab): void {
	if (next === tab) return

	tab = next
	const url = new URL(page.url.href)
	if (next === "overview") url.searchParams.delete("tab")
	else url.searchParams.set("tab", next)

	history.replaceState(history.state, "", url)
}

afterNavigate((): void => {
	const next = idFromURL()
	if (next === "" || next === id) return

	tab = tabFromURL()
	const previous = claimedId
	id = next
	loading = true
	coverFailed = false
	stopChecks()
	error = ""
	acknowledgements = []
	idempotencyKey = ""

	if (previous && previous !== next) ignore(releaseShip(previous))

	claimedId = ""

	const cached = readBundleCache(next)
	if (cached) showCachedBundle(cached)

	void load(next)
})

onMount((): (() => void) => {
	id = idFromURL()
	tab = tabFromURL()
	if (id === "") {
		void goto("/queue")
		return (): void => {}
	}

	void boot()

	const beat = setInterval((): void => {
		// A backgrounded tab does not need to hold the claim awake; the TTL releases it if the reviewer walks away.
		if (document.visibilityState !== "visible" || claimedId === "") return
		ignore(heartbeat(claimedId))
	}, 60000)

	function onKeydown(event: KeyboardEvent): void {
		if (
			event.target instanceof HTMLInputElement
			|| event.target instanceof HTMLTextAreaElement
			|| event.target instanceof HTMLSelectElement
		) {
			if (event.key === "Escape") (event.target as HTMLElement).blur()
			return
		}

		if (event.key === "Escape") {
			event.preventDefault()
			void goto(backPath)
			return
		}

		if (
			(event.key === "ArrowLeft" || event.key === "ArrowRight")
			&& !event.shiftKey
			&& !event.altKey
			&& !event.ctrlKey
			&& !event.metaKey
		) {
			event.preventDefault()
			const current = tabKeys.indexOf(tab)
			const step = event.key === "ArrowRight" ? 1 : -1
			selectTab(tabKeys[(current + step + tabKeys.length) % tabKeys.length])
			return
		}

		if (readOnly || reviewed) {
			if (event.key === "?") showShortcuts = !showShortcuts
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
		} else if (event.key === "?") showShortcuts = !showShortcuts
	}

	window.addEventListener("keydown", onKeydown)

	return (): void => {
		clearInterval(beat)
		stopChecks()
		window.removeEventListener("keydown", onKeydown)
		if (claimedId !== "") ignore(releaseShip(claimedId))
	}
})

// stopChecks cancels the polling started while a ship is checking.
function stopChecks(): void {
	if (checksTimer) {
		clearTimeout(checksTimer)
		checksTimer = null
	}
}

// watchChecks refreshes a ship while the check job runs, then reloads so the reviewer can claim it.
function watchChecks(): void {
	if (bundle?.submission.status !== "checking" || checksTimer) return

	checksTimer = setTimeout((): void => {
		checksTimer = null
		void refreshWhileChecking()
	}, 2000)
}

// checkMessage describes the running state for the banner.
function checkMessage(): string {
	const run = bundle?.checkRun
	if (run?.status !== "running") return "Waiting for the check worker…"

	const attempt = run.attempts > 1 ? ` · attempt ${run.attempts}` : ""

	return `Running repository checks${attempt}…`
}

// refreshWhileChecking loads the next state and keeps polling until the job finishes.
async function refreshWhileChecking(): Promise<void> {
	try {
		const next = await fetchBundle(id)
		if (id === "") return

		bundle = next

		if (next.submission.status === "checking") {
			watchChecks()
			return
		}

		flash("Checks finished.")
		await load(id)
	} catch {
		watchChecks()
	}
}

async function boot(): Promise<void> {
	try {
		session.me ??= await fetchMe()
	} catch {
		void goto("/")
		return
	}

	const cached = readBundleCache(id)
	if (cached) showCachedBundle(cached)

	await load()
}

// showCachedBundle paints the last copy while the fresh bundle loads behind it.
function showCachedBundle(cached: Bundle): void {
	bundle = cached
	loading = false
	error = ""
	actionError = ""
	note = cached.draft?.note ?? ""
	internalNote = cached.draft?.internalNote ?? ""
	hourInit()
}

async function load(target = id): Promise<void> {
	const sameShip = bundle?.submission.id === target

	try {
		if (!sameShip) {
			note = ""
			internalNote = ""
		}

		actionError = ""

		const fetched = await fetchBundle(target)
		if (target !== id) return

		let next = fetched
		const closed = fetched.submission.status === "decided" || fetched.submission.status === "withdrawn"
		const canClaim =
			!readOnly
			&& !closed
			&& fetched.submission.status !== "checking"
			&& (fetched.submission.stage !== 2 || canSecondPass)
		if (canClaim && fetched.claim?.isMine !== 1) {
			await claimShip(target)
			next = await fetchBundle(target)
			if (target !== id) return
		}

		bundle = next
		writeBundleCache(target, next)
		claimedId = canClaim ? target : ""

		// Restore the server-side draft so a refresh or another device does not lose typed notes.
		if (next.draft) ({ note, internalNote } = next.draft)

		hourInit()
		error = ""
		watchChecks()
		void checkHead()
	} catch (err) {
		if (err instanceof ApiError && err.code === "unauthorized") {
			void goto("/")
			return
		}

		if (target !== id) return

		const message = err instanceof Error ? err.message : "Could not open this ship."
		if (err instanceof ApiError && (err.code === "forbidden" || err.code === "not_found")) {
			removeBundleCache(target)
			bundle = null
			error = message
			return
		}

		if (sameShip) flash(`Could not refresh this ship. ${message}`)
		else error = message
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

function toggleAck(key: string): void {
	if (acknowledgements.includes(key)) acknowledgements = acknowledgements.filter(value => value !== key)
	else acknowledgements = [...acknowledgements, key]
}

function acknowledgeAll(): void {
	acknowledgements = warnings.map(check => check.key)
}

// checkHead shows a stale notice when the default branch moved after the snapshot.
async function checkHead(): Promise<void> {
	if (!bundle?.submission.repoUrl) {
		checksOutdated = false
		return
	}

	try {
		const head = await fetchCheckHead(bundle.submission.id)
		checksOutdated = head.outdated
	} catch {
		// A GitHub outage should not block the review; the reviewer can still rerun the checks.
		checksOutdated = false
	}
}

async function rerunChecksNow(): Promise<void> {
	if (!bundle || rerunning) return

	rerunning = true
	rerunError = ""

	try {
		await rerunChecks(bundle.submission.id)
		await load(id)
		checksOutdated = false
		flash("Checks queued.")
	} catch (err) {
		rerunError = err instanceof Error ? err.message : "Could not rerun the checks."
	} finally {
		rerunning = false
	}
}

async function refreshEvidenceNow(): Promise<void> {
	if (!bundle || refreshingEvidence) return

	refreshingEvidence = true
	evidenceError = ""

	try {
		const evidence = await refreshRepositoryEvidence(bundle.submission.id)
		bundle = { ...bundle, evidence }
		writeBundleCache(bundle.submission.id, bundle)
		flash("Evidence refreshed.")
	} catch (err) {
		evidenceError = err instanceof Error ? err.message : "Could not refresh the evidence."
	} finally {
		refreshingEvidence = false
	}
}

async function decide(decision: "approved" | "changes" | "rejected"): Promise<void> {
	if (!bundle || deciding) return

	actionError = ""

	if (!canDecide) {
		actionError = "Only organisers can decide the second pass."
		return
	}

	if (decision === "approved") {
		if (blocking.length > 0) {
			actionError = "Approval is blocked. Read the blocking check, then ask for changes or reject."
			return
		}
		if (!secondPass && missingAcks.length > 0) {
			actionError = `Acknowledge ${missingAcks.length} warning${missingAcks.length === 1 ? "" : "s"} before approving.`
			return
		}
	} else if (note.trim() === "") {
		actionError = "Write a note to the maker before returning or rejecting."
		noteElement?.focus()
		return
	}

	if (
		secondPass
		&& heldReview
		&& (decision !== "approved" || Math.round(hours * 3600) !== (heldReview.hoursApprovedSeconds ?? 0))
		&& overrideReason.trim() === ""
	) {
		actionError = "Explain why the second pass changes the first-pass decision or hours."
		overrideElement?.focus()
		return
	}

	deciding = true
	try {
		// One key per ship and version: if the response is lost, the retry returns the review that was already written instead of creating a second one.
		const prefix = `${id}:${bundle.submission.version}:`
		const key = idempotencyKey.startsWith(prefix) ? idempotencyKey : `${prefix}${crypto.randomUUID()}`
		idempotencyKey = key

		const result = await decideShip(id, {
			acknowledgements,
			decision,
			hoursApprovedSeconds: approvedSeconds,
			idempotencyKey: key,
			internalNote: internalNote.trim(),
			noteToMaker: note.trim(),
			overrideReason: overrideReason.trim(),
			version: bundle.submission.version,
		})
		idempotencyKey = ""

		if (result.held === 1) flash(`Held for the second pass: ${result.heldReason ?? "policy"}.`)
		else {
			const verb =
				decision === "approved" ? "Approved" : decision === "changes" ? "Changes requested" : "Rejected"
			const delivery = result.deliveryStatus ? ` (${result.deliveryStatus})` : ""
			flash(`${verb} and sent${delivery}.`)
		}

		await sleep(300)

		if (result.nextId) window.location.assign(`/review/${encodeURIComponent(result.nextId)}`)
		else await goto("/queue")
	} catch (err) {
		if (err instanceof ApiError && err.code === "stale_version")
			actionError = "This ship changed since you opened it. Reload before deciding."
		else actionError = err instanceof Error ? err.message : "Could not record the decision."
	} finally {
		deciding = false
	}
}

async function sendBackToStageOne(): Promise<void> {
	if (!bundle || sendingBack) return

	sendBackError = ""

	if (sendBackReason.trim() === "") {
		sendBackError = "Write why stage 1 should look at this ship again."
		return
	}

	sendingBack = true

	try {
		await sendShipBack(bundle.submission.id, {
			reason: sendBackReason.trim(),
			version: bundle.submission.version,
		})
		await goto("/queue")
	} catch (err) {
		if (err instanceof ApiError && err.code === "stale_version")
			sendBackError = "This ship changed since you opened it. Reload before sending it back."
		else sendBackError = err instanceof Error ? err.message : "Could not send the ship back."
	} finally {
		sendingBack = false
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
		await saveDraft(id, { internalNote, note })
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
		<a class="ui-button" href={backPath}>Return to {backLabel.toLowerCase()}</a>
	</section>
{:else}
	<header class="ship-head">
		<div class="head-main">
			<p class="crumbs"><a href={backPath}>{backLabel}</a></p>
			<div class="title-line">
				<h1>{submission.title}</h1>
				<span class="ui-badge" data-tone={statusTone(submission.status)}>{humanStatus(submission.status)}</span>
				<span class="version">v{submission.version}</span>
			</div>
			<p class="maker">
				{#if submission.makerName}
					{submission.makerName}
					·
				{/if}
				{submission.makerEmail}
				{#if submission.week}
					<span>· Week {submission.week}</span>
				{/if}
				{#if submission.requestedTier}
					<span>· Tier {submission.requestedTier}</span>
				{/if}
			</p>
		</div>
		<div class="head-links">
			{#if submission.repoUrl}
				<a class="ui-button ghost sm" href={submission.repoUrl} rel="noreferrer" target="_blank"
					>Repository ↗</a
				>
			{/if}
			{#if submission.demoUrl}
				<a class="ui-button ghost sm" href={submission.demoUrl} rel="noreferrer" target="_blank">Demo ↗</a>
			{/if}
			<button
				class="ui-button ghost sm shortcuts-button"
				onclick={(): void => {
					showShortcuts = !showShortcuts
				}}
				type="button"
			>
				Shortcuts <kbd class="ui-kbd">?</kbd>
			</button>
		</div>
	</header>

	{#if checking}
		<div class="checking-banner">
			<span class="ui-badge" data-tone="pending">Checking</span>
			<span>{checkMessage()}</span>
		</div>
	{/if}

	{#if showShortcuts}
		<div class="shortcuts">
			{#if !readOnly && !reviewed}
				<span><kbd class="ui-kbd">A</kbd> approve</span>
				<span><kbd class="ui-kbd">C</kbd> focus the note</span>
				<span><kbd class="ui-kbd">R</kbd> reject</span>
			{/if}
			<span><kbd class="ui-kbd">← →</kbd> sections</span>
			<span><kbd class="ui-kbd">Esc</kbd> back to {backLabel.toLowerCase()}</span>
		</div>
	{/if}

	{#if secondPass}
		<div class="second-pass">
			<span class="ui-badge" data-tone="secondpass">Second pass</span>
			<span>{submission.heldReason ?? "Held for a second review."}</span>
		</div>
		{#if heldReview}
			<div class="first-pass">
				<strong>First pass · {heldReview.reviewerEmail}</strong>
				{#if heldReview.noteToMaker}
					<p>{heldReview.noteToMaker}</p>
				{/if}
				{#if heldReview.acknowledgements.length > 0}
					<p class="acks">Acknowledged: {heldReview.acknowledgements.join(", ")}</p>
				{/if}
			</div>
		{/if}
	{/if}

	<div class="layout">
		<section class="content">
			<div aria-label="Ship sections" class="ui-tabs ship-tabs" role="tablist">
				{#each [
					{ count: undefined, key: "overview", label: "Overview" },
					{ count: bundle.checks.length, key: "checks", label: "Checks" },
					{ count: bundle.journals.length, key: "journal", label: "Journal" },
					{ count: bundle.bom.length, key: "bom", label: "BOM" },
					{ count: bundle.evidence?.files.length, key: "evidence", label: "Evidence" },
					{ count: bundle.reviews.length, key: "history", label: "History" },
				] as item (item.key)}
					<button
						aria-selected={tab === item.key}
						class="ui-tab"
						onclick={(): void => selectTab(item.key as Tab)}
						role="tab"
						type="button"
						class:active={tab === item.key}
					>
						{item.label}
						{#if item.count !== undefined}
							<span class="tab-count">{item.count}</span>
						{/if}
					</button>
				{/each}
			</div>

			{#if tab === "overview"}
				<article class="ui-card padded overview">
					{#if submission.thumbnailUrl && !coverFailed}
						<img
							alt={`${submission.title} project image`}
							class="cover"
							decoding="async"
							loading="lazy"
							onerror={(): void => {
								coverFailed = true
							}}
							src={submission.thumbnailUrl}
						>
					{:else}
						<div class="cover-empty">
							<svg aria-hidden="true" fill="none" height="26" viewBox="0 0 24 24" width="26">
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
							<strong
								>{submission.thumbnailUrl ? "Project image unavailable" : "No project image"}</strong
							>
							<span>
								{submission.thumbnailUrl
									? "The image link could not be loaded."
									: "This ship does not include a project image."}
							</span>
						</div>
					{/if}
					<h2>What the maker built</h2>
					<p class="description">{submission.description || "No description was sent."}</p>

					<div class="stats">
						<div><span>Claimed</span><strong>{formatHours(submission.hoursClaimedSeconds)}</strong></div>
						<div><span>Journals</span><strong>{formatHours(submission.hoursJournalSeconds)}</strong></div>
						<div>
							<span>Hackatime</span><strong>{formatHours(submission.hoursHackatimeSeconds)}</strong>
						</div>
						<div><span>BOM</span><strong>{formatMoney(submission.bomTotalCents)}</strong></div>
					</div>

					{#if bundle.priorReviews.length > 0}
						<div class="section">
							<h3>Prior review</h3>
							{#each bundle.priorReviews as prior (prior.at)}
								<div class="prior-row">
									<span
										class="ui-badge"
										data-tone={statusTone(
											prior.result.toLowerCase().includes("approved") ? "approved" : "changes",
										)}
										>{prior.result}</span
									>
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
									{#if event.actorEmail}
										<span> · {event.actorEmail}</span>
									{/if}
									<small>{formatDateTime(event.createdAt)}</small>
								</div>
							</div>
						{/each}
					</div>
				</article>
			{:else if tab === "checks"}
				<ChecksTab
					{acknowledgements}
					checks={bundle.checks}
					onAcknowledgeAll={acknowledgeAll}
					onToggle={toggleAck}
				/>
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
									<span class="ui-badge" data-tone="approved"
										>{entry.clipCount}
										clip{entry.clipCount === 1 ? "" : "s"}
										· {formatHours(entry.coveredSeconds)} covered</span
									>
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
								<tr>
									<th>Part</th>
									<th>Purpose</th>
									<th class="num">Qty</th>
									<th class="num">Unit (US$)</th>
									<th>Vendor</th>
								</tr>
							</thead>
							<tbody>
								{#each bundle.bom as line (line.name)}
									<tr>
										<td>
											{#if line.link}
												<a href={line.link} rel="noreferrer" target="_blank">{line.name} ↗</a>
											{:else}
												{line.name}
											{/if}
										</td>
										<td>{line.purpose ?? "–"}</td>
										<td class="num">{line.quantity}</td>
										<td class="num">{formatMoneyAmount(line.unitCostCents)}</td>
										<td>{line.vendor ?? "–"}</td>
									</tr>
								{/each}
							</tbody>
							<tfoot>
								<tr>
									<td colspan="3">Total (US$)</td>
									<td class="num">{formatMoneyAmount(submission.bomTotalCents)}</td>
									<td></td>
								</tr>
							</tfoot>
						</table>
					</div>
				{/if}
			{:else if tab === "evidence"}
				<EvidenceTab
					canRefresh={canSecondPass}
					error={evidenceError}
					evidence={bundle.evidence}
					onRefresh={refreshEvidenceNow}
					refreshing={refreshingEvidence}
				/>
			{:else if tab === "history"}
				<HistoryTab reviews={bundle.reviews} />
			{/if}
		</section>

		<aside class="rail">
			<section class="ui-card padded summary">
				<div class="summary-head">
					<h2>Checks</h2>
					<div class="summary-actions">
						{#if canSecondPass && submission.status !== "checking"}
							<button class="link" disabled={rerunning} onclick={rerunChecksNow} type="button">
								{rerunning ? "Rerunning…" : "Rerun checks"}
							</button>
						{/if}
						<span
							class="ui-badge"
							data-tone={statusTone(bundle.checks.length > 0 ? overallChecks(bundle.checks) : "pass")}
						>
							{bundle.checks.length > 0 ? overallChecks(bundle.checks) : "pass"}
						</span>
					</div>
				</div>
				<div class="summary-counts">
					<span><strong>{bundle.checks.filter(check => check.status === "pass").length}</strong> pass</span>
					<span><strong>{warnings.length}</strong> warn</span>
					<span><strong>{blocking.length}</strong> block</span>
				</div>
				{#if bundle.checkRun?.error}
					<div class="ui-notice" data-tone="bad">
						<strong>Check attempt {bundle.checkRun.attempts} failed</strong>
						{bundle.checkRun.error}
					</div>
				{/if}
				{#if checksOutdated}
					<div class="ui-notice" data-tone="warn">
						<strong>Checks are out of date</strong>
						The repository moved after the last check.
						{#if canSecondPass}
							<button class="link" disabled={rerunning} onclick={rerunChecksNow} type="button">
								Rerun checks
							</button>
						{:else}
							Ask an organiser to rerun the checks.
						{/if}
					</div>
				{/if}
				{#if rerunError}
					<p class="ui-notice" data-tone="bad" role="alert">{rerunError}</p>
				{/if}
				<p class="policy">
					{#if bundle.policy.holdOnWarn === 1}
						Approvals with warnings are held for the second pass.
					{:else}
						Warnings can be approved with an acknowledgement.
					{/if}
					Fraud runs at stage {bundle.policy.fraudMinStage} only.
				</p>
			</section>

			{#if reviewed}
				<section class="ui-card padded decision">
					<h2>{submission.status === "withdrawn" ? "Withdrawn" : "Final decision"}</h2>
					<TimeEvidenceBar segments={timeSegments} />
					<p class="policy">
						{submission.status === "withdrawn"
							? "This ship was withdrawn before a final decision."
							: "This ship has been reviewed. The History tab has the note, approved hours and acknowledgements."}
					</p>
				</section>
			{:else if readOnly}
				<section class="ui-card padded decision">
					<h2>Read-only shadow</h2>
					<TimeEvidenceBar segments={timeSegments} />
					<p class="policy">
						This copy is here for review and parity only. Claims, drafts and decisions stay in Ari, and
						Review sends nothing to the maker.
					</p>
				</section>
			{:else}
				<section class="ui-card padded decision">
					<h2>Decision</h2>

					<TimeEvidenceBar segments={timeSegments} />

					<label class="field">
						<span>Approved hours</span>
						<span class="hours-row">
							<input class="ui-input" max="720" min="0" step="0.25" type="number" bind:value={hours}>
							<span class="hours-hint">/ {formatHours(submission.hoursClaimedSeconds)} claimed</span>
						</span>
					</label>

					<label class="field">
						<span class="field-head">
							<span
								>Note to maker
								{!secondPass && (missingAcks.length > 0 || blocking.length > 0)
									? ""
									: "(optional)"}</span
							>
							<span aria-hidden="true" class="field-count">{note.length} / {maxNote}</span>
						</span>
						<textarea
							class="ui-input"
							maxlength={maxNote}
							onblur={(): void => void saveDraftNow()}
							placeholder="What should they change, or why is this good?"
							rows="5"
							bind:this={noteElement}
							bind:value={note}
						></textarea>
					</label>

					<label class="field">
						<span class="field-head">
							<span>Internal note</span>
							<span aria-hidden="true" class="field-count">{internalNote.length} / {maxNote}</span>
						</span>
						<textarea
							class="ui-input"
							maxlength={maxNote}
							onblur={(): void => void saveDraftNow()}
							placeholder="Only the team sees this."
							rows="2"
							bind:value={internalNote}
						></textarea>
					</label>

					{#if secondPass}
						<label class="field">
							<span class="field-head">
								<span>Override reason (required if you change the first pass)</span>
								<span aria-hidden="true" class="field-count">{overrideReason.length} / {maxNote}</span>
							</span>
							<textarea
								class="ui-input"
								maxlength={maxNote}
								placeholder="Why the first-pass hours or decision should change."
								rows="2"
								bind:this={overrideElement}
								bind:value={overrideReason}
							></textarea>
						</label>
					{/if}

					{#if !secondPass && missingAcks.length > 0}
						<div class="ui-notice" data-tone="warn">
							<strong>{missingAcks.length} warning{missingAcks.length === 1 ? "" : "s"} left</strong>
							Acknowledge them in Checks to unlock approve.
							<button class="link" onclick={(): void => selectTab("checks")} type="button">
								Open checks
							</button>
						</div>
					{/if}

					{#if blocking.length > 0}
						<div class="ui-notice" data-tone="bad">
							<strong>Approval disabled</strong>
							{blocking[0].summary}.
							<button class="link" onclick={(): void => selectTab("checks")} type="button">
								Read the blocking check
							</button>
						</div>
					{/if}

					{#if actionError}
						<p class="ui-notice" data-tone="bad" role="alert">{actionError}</p>
					{/if}

					<div class="buttons">
						<button
							class="ui-button ok block"
							disabled={deciding || !canApprove || !canDecide}
							onclick={(): Promise<void> => decide("approved")}
							type="button"
						>
							{deciding ? "Saving…" : secondPass ? "Confirm approval" : "Approve"}
							<kbd class="ui-kbd">A</kbd>
						</button>
						<div class="secondary">
							<button
								class="ui-button changes"
								disabled={deciding || !canDecide}
								onclick={(): Promise<void> => decide("changes")}
								type="button"
							>
								Request changes
							</button>
							<button
								class="ui-button danger"
								disabled={deciding || !canDecide}
								onclick={(): Promise<void> => decide("rejected")}
								type="button"
							>
								Reject
								<kbd class="ui-kbd">R</kbd>
							</button>
						</div>
					</div>

					{#if secondPass && canSecondPass}
						<div class="send-back">
							<h3>Send back to stage 1</h3>
							<p>
								Invalidate the held review and let a reviewer look again. No event is sent to the maker.
							</p>
							<textarea
								class="ui-input"
								placeholder="What should stage 1 look at again?"
								rows="2"
								bind:value={sendBackReason}
							></textarea>
							{#if sendBackError}
								<p class="ui-notice" data-tone="bad" role="alert">{sendBackError}</p>
							{/if}
							<button
								class="ui-button secondary block"
								disabled={sendingBack}
								onclick={sendBackToStageOne}
								type="button"
							>
								{sendingBack ? "Sending back…" : "Send back"}
							</button>
						</div>
					{/if}

					{#if bundle.claim}
						<p class="claim">
							Claimed by {bundle.claim.reviewerEmail} until {formatDateTime(bundle.claim.expiresAt)}
						</p>
					{/if}
				</section>
			{/if}
		</aside>
	</div>

	{#if !readOnly && !reviewed}
		<div class="mobile-actions">
			<button
				class="ui-button ok"
				disabled={deciding || !canApprove || !canDecide}
				onclick={(): Promise<void> => decide("approved")}
				type="button"
			>
				{deciding ? "Saving…" : secondPass ? "Confirm" : "Approve"}
			</button>
			<button
				class="ui-button changes"
				disabled={deciding || !canDecide}
				onclick={(): Promise<void> => decide("changes")}
				type="button"
			>
				Request changes
			</button>
			<button
				class="ui-button danger"
				disabled={deciding || !canDecide}
				onclick={(): Promise<void> => decide("rejected")}
				type="button"
			>
				Reject
			</button>
		</div>
	{/if}

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

h1 {
	font-size: var(--text-2xl);
	overflow-wrap: anywhere;
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

.title-line {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
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

.checking-banner {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
	padding: var(--space-2) var(--space-3);
	margin-top: var(--space-3);
	font-size: var(--text-sm);
	color: var(--text-2);
	background: var(--surface-2);
	border-radius: var(--radius-md);
}

.second-pass {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
	padding: var(--space-2) var(--space-3);
	margin-top: var(--space-3);
	font-size: var(--text-sm);
	color: var(--status-secondpass);
	background: color-mix(in srgb, var(--status-secondpass) 10%, var(--surface));
	border-radius: var(--radius-md);
}

.first-pass {
	padding: var(--space-2) var(--space-3);
	margin-top: var(--space-2);
	font-size: var(--text-sm);
	color: var(--text-2);
	background: var(--surface-2);
	border-radius: var(--radius-md);
}

.first-pass strong {
	display: block;
	color: var(--text);
}

.first-pass p {
	padding-top: 2px;
}

.ship-tabs {
	flex-wrap: wrap;
	overflow-x: visible;
}

.tab-count {
	font-weight: 600;
	font-variant-numeric: tabular-nums;
	color: var(--text-3);
}

.acks {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.send-back {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding-top: var(--space-3);
	margin-top: var(--space-3);
	border-top: 1px solid var(--border);
}

.send-back h3 {
	font-size: var(--text-sm);
}

.send-back p {
	font-size: var(--text-xs);
	color: var(--text-3);
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

.journals {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.content > .ui-card,
.content > .journals {
	margin-top: var(--space-3);
}

.ui-tabs {
	margin-bottom: var(--space-1);
}

.overview h2,
.decision h2,
.summary h2 {
	font-size: var(--text-md);
	font-weight: 700;
}

.description {
	max-width: 80ch;
	padding-top: var(--space-2);
	font-size: var(--text-sm);
	color: var(--text-2);
	white-space: pre-wrap;
}

.overview {
	overflow: hidden;
}

.cover {
	display: block;
	width: calc(100% + 2 * var(--space-4));
	max-height: 420px;
	margin: calc(-1 * var(--space-4)) calc(-1 * var(--space-4)) var(--space-4);
	object-fit: contain;
	background: var(--surface-2);
}

.cover-empty {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	align-items: center;
	justify-content: center;
	width: calc(100% + 2 * var(--space-4));
	min-height: 200px;
	padding: var(--space-6) var(--space-4);
	margin: calc(-1 * var(--space-4)) calc(-1 * var(--space-4)) var(--space-4);
	text-align: center;
	background: var(--surface-2);
	border-bottom: 1px solid var(--border);
}

.cover-empty svg {
	margin-bottom: var(--space-1);
	color: var(--text-3);
}

.cover-empty strong {
	font-size: var(--text-sm);
	font-weight: 700;
	color: var(--text-2);
}

.cover-empty span {
	max-width: 36ch;
	font-size: var(--text-xs);
	color: var(--text-3);
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

.summary-actions {
	display: flex;
	gap: var(--space-2);
	align-items: center;
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
	font-variant-numeric: tabular-nums;
	color: var(--text);
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

.field-head {
	display: flex;
	gap: var(--space-2);
	align-items: baseline;
	justify-content: space-between;
}

.field-count {
	flex: none;
	font-size: var(--text-sm);
	font-weight: 400;
	font-variant-numeric: tabular-nums;
	color: var(--text-2);
	white-space: nowrap;
}

.hours-row {
	display: flex;
	gap: var(--space-2);
	align-items: center;
}

.field .hours-row {
	font-size: var(--text-sm);
	font-weight: 400;
	color: var(--text-3);
}

.hours-row input {
	flex: 1;
	width: auto;
	min-width: 0;
}

.hours-hint {
	flex: none;
	font-size: var(--text-xs);
	color: var(--text-3);
	white-space: nowrap;
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

.mobile-actions {
	display: none;
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
		padding-bottom: 76px;
	}

	.rail {
		position: static;
		padding-top: 0;
	}

	.mobile-actions {
		position: fixed;
		right: 0;
		bottom: 0;
		left: 0;
		z-index: var(--layer-dialog);
		display: grid;
		grid-template-columns: 1.1fr 1.3fr 0.8fr;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3) max(var(--space-2), env(safe-area-inset-bottom));
		background: var(--surface);
		border-top: 1px solid var(--border);
		box-shadow: var(--shadow);
	}

	.mobile-actions .ui-button {
		min-width: 0;
		height: 44px;
	}

	.shortcuts-button {
		display: none;
	}

	.toast {
		bottom: calc(74px + var(--space-3));
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

	.ship-tabs {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		overflow: visible;
	}

	.ship-tabs button {
		justify-content: center;
		padding-inline: var(--space-2);
	}

	.shortcuts-button {
		display: none;
	}
}
</style>
