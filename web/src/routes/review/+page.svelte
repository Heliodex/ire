<script lang="ts">
import { onMount } from "svelte"
import { afterNavigate, goto } from "$app/navigation"
import { page } from "$app/state"
import {
	type Bundle,
	type Check,
	claimShip,
	createNote,
	decideShip,
	deleteNote,
	fetchBundle,
	fetchCheckHead,
	fetchMe,
	fetchQueue,
	heartbeat,
	type Note,
	program,
	refreshRepositoryEvidence,
	releaseShip,
	rerunChecks,
	saveDraft,
	sendShipBack,
} from "../../lib/api"
import { ApiError } from "../../lib/api-error"
import { ignore, sleep } from "../../lib/async"
import { type BundleCache, createBundleCache, prefetchNextBundle } from "../../lib/bundle-cache"
import { isBlocking, isWarning, overallChecks } from "../../lib/checks"
import LoadingState from "../../lib/components/LoadingState.svelte"
import BomTab from "../../lib/components/review/BomTab.svelte"
import ChecksTab from "../../lib/components/review/ChecksTab.svelte"
import EvidenceTab from "../../lib/components/review/EvidenceTab.svelte"
import HistoryTab from "../../lib/components/review/HistoryTab.svelte"
import JournalList from "../../lib/components/review/JournalList.svelte"
import NotesCard from "../../lib/components/review/NotesCard.svelte"
import PriorReviews from "../../lib/components/review/PriorReviews.svelte"
import ResubmissionDiff from "../../lib/components/review/ResubmissionDiff.svelte"
import ReviewTimeline from "../../lib/components/review/ReviewTimeline.svelte"
import TimeEvidenceBar from "../../lib/components/review/TimeEvidenceBar.svelte"
import { maxNote } from "../../lib/contract"
import {
	decisionLabel,
	formatDateTime,
	formatHours,
	formatMoney,
	formatMoneyAmount,
	humanStatus,
	statusTone,
} from "../../lib/format"
import { hasPermission, permissionNoteManage, permissionSecondPass, programPermissions } from "../../lib/permissions"
import { type ReviewPreview, readReviewPreview } from "../../lib/review-preview"
import { reviewerHref } from "../../lib/reviewer"
import { dropSignIn, session } from "../../lib/session.svelte.js"

const tabKeys = ["overview", "journal", "checks", "bom", "evidence", "history"] as const
type Tab = (typeof tabKeys)[number]

interface TimeSegment {
	label: string
	tone: "journals" | "lapse" | "hackatime" | "cut"
	value: number
}

let bundle = $state<Bundle | null>(undefined)
let id = $state("")
let loading = $state(true)
let preview = $state<ReviewPreview | null>(undefined)
let previewCoverFailed = $state(false)
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
let noteElement = $state<HTMLTextAreaElement | null>(undefined)
let claimedId = $state("")
let idempotencyKey = $state("")
let refreshingEvidence = $state(false)
let evidenceError = $state("")
let overrideReason = $state("")
let overrideElement = $state<HTMLTextAreaElement | null>(undefined)
let sendBackReason = $state("")
let sendBackError = $state("")
let sendingBack = $state(false)
let rerunning = $state(false)
let rerunError = $state("")
let checksOutdated = $state(false)
let coverFailed = $state(false)
let wide = $state(false)
let checksTimer: ReturnType<typeof setTimeout> | null
let prefetchTimer: ReturnType<typeof setTimeout> | null

// bundleCache scopes cached ships to the signed-in reviewer. It is built per call so a
// changed session is picked up, and sessionStorage is only touched on the client.
function bundleCache(): BundleCache {
	return createBundleCache(program, session.me?.user.email ?? "anonymous", sessionStorage)
}

const submission = $derived(bundle?.submission ?? undefined)
const reviewed = $derived(submission?.status === "decided" || submission?.status === "withdrawn")
const secondPass = $derived(!reviewed && (submission?.stage === 2 || submission?.status === "stage2_hold"))
const permissions = $derived(programPermissions(session.me, program))
const canSecondPass = $derived(hasPermission(permissions, permissionSecondPass))
const checking = $derived(submission?.status === "checking")
const readOnly = $derived(session.me?.readOnly ?? false)
const canDecide = $derived(!readOnly && !reviewed && !checking && (!secondPass || canSecondPass))
const canAdjustEntries = $derived(canDecide && bundle?.claim?.isMine === 1)
const heldReview = $derived(bundle?.reviews.find(review => review.held === 1) ?? undefined)
const warnings = $derived(bundle?.checks.filter(isWarning) ?? [])
const blocking = $derived(bundle?.checks.filter(isBlocking) ?? [])
const previewChecks = $derived(preview?.checks ?? [])
const previewWarnings = $derived(previewChecks.filter(isWarning))
const previewBlocking = $derived(previewChecks.filter(isBlocking))
const previewPass = $derived(previewChecks.filter(check => check.status === "pass").length)
const previewTimeSegments = $derived.by((): TimeSegment[] => {
	const segments: TimeSegment[] = [
		{ label: "Journalled", tone: "journals", value: preview?.hoursJournalSeconds ?? 0 },
		{ label: "Hackatimed", tone: "hackatime", value: preview?.hoursHackatimeSeconds ?? 0 },
	]

	return segments.filter(segment => segment.value > 0)
})
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

// cycleTab moves to the adjacent tab, matching keyboard navigation.
function cycleTab(step: number): void {
	const order: Tab[] = wide ? tabKeys.filter(key => key !== "journal") : [...tabKeys]
	const current = order.indexOf(tab)
	if (current === -1) {
		selectTab("overview")
		return
	}

	selectTab(order[(current + step + order.length) % order.length])
}

// swipeTabs cycles tabs on a horizontal swipe, for touch devices.
function swipeTabs(node: HTMLElement): { destroy(): void } {
	let startX = 0
	let startY = 0
	let startT = 0
	let tracking = false

	function onStart(event: TouchEvent): void {
		if (event.touches.length !== 1) {
			tracking = false
			return
		}

		const [touch] = event.touches
		if (!touch) return
		startX = touch.clientX
		startY = touch.clientY
		startT = Date.now()
		tracking = true
	}

	function onEnd(event: TouchEvent): void {
		if (!tracking) return
		tracking = false

		const [touch] = event.changedTouches
		if (!touch) return

		const dx = touch.clientX - startX
		const dy = touch.clientY - startY
		const absDx = Math.abs(dx)
		const absDy = Math.abs(dy)
		const dt = Date.now() - startT

		if (absDx < 56 || absDx < absDy * 1.25 || dt > 700) return

		cycleTab(dx < 0 ? 1 : -1)
	}

	function onCancel(): void {
		tracking = false
	}

	node.addEventListener("touchstart", onStart, { passive: true })
	node.addEventListener("touchend", onEnd, { passive: true })
	node.addEventListener("touchcancel", onCancel, { passive: true })

	return {
		destroy(): void {
			node.removeEventListener("touchstart", onStart)
			node.removeEventListener("touchend", onEnd)
			node.removeEventListener("touchcancel", onCancel)
		},
	}
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
	if (prefetchTimer) {
		clearTimeout(prefetchTimer)
		prefetchTimer = undefined
	}
	error = ""
	acknowledgements = []
	idempotencyKey = ""

	if (previous && previous !== next) ignore(releaseShip(previous))

	claimedId = ""

	const cached = bundleCache().read(next)
	if (cached) showCachedBundle(cached)

	void load(next)
})

onMount((): (() => void) => {
	id = idFromURL()
	tab = tabFromURL()
	preview = readReviewPreview(id) ?? undefined
	previewCoverFailed = false

	if (id === "") {
		void goto("/queue")
		return (): void => {}
	}

	void boot()

	// Wide screens show the journals beside the overview, so the journal tab is not a tab there.
	const media = window.matchMedia("(min-width: 100rem)")
	wide = media.matches
	if (wide && tab === "journal") selectTab("overview")
	function syncWide(event: MediaQueryListEvent): void {
		wide = event.matches
		if (event.matches && tab === "journal") selectTab("overview")
	}
	media.addEventListener("change", syncWide)

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
			// The journal lives beside the overview on wide screens, so it is not a tab there.
			const order: Tab[] = wide ? tabKeys.filter(key => key !== "journal") : [...tabKeys]
			const current = order.indexOf(tab)
			if (current === -1) {
				selectTab("overview")
				return
			}
			const step = event.key === "ArrowRight" ? 1 : -1
			selectTab(order[(current + step + order.length) % order.length])
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
		if (prefetchTimer) clearTimeout(prefetchTimer)
		window.removeEventListener("keydown", onKeydown)
		media.removeEventListener("change", syncWide)
		if (claimedId !== "") ignore(releaseShip(claimedId))
	}
})

// stopChecks cancels the polling started while a ship is checking.
function stopChecks(): void {
	if (checksTimer) {
		clearTimeout(checksTimer)
		checksTimer = undefined
	}
}

// watchChecks refreshes a ship while the check job runs, then reloads so the reviewer can claim it.
function watchChecks(): void {
	if (bundle?.submission.status !== "checking" || checksTimer) return

	checksTimer = setTimeout((): void => {
		checksTimer = undefined
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

	const cached = bundleCache().read(id)
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

// schedulePrefetch warms the next ship's bundle once the current screen is idle.
function schedulePrefetch(ship: Bundle["submission"]): void {
	if (prefetchTimer) clearTimeout(prefetchTimer)
	if (ship.status === "decided" || ship.status === "withdrawn" || ship.status === "checking") return

	prefetchTimer = setTimeout(() => {
		prefetchTimer = undefined
		void prefetchNextBundle(ship.id, ship.stage, session.me?.user.email, bundleCache())
	}, 2500)
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
		bundleCache().write(target, next)
		claimedId = canClaim ? target : ""

		// Restore the server-side draft so a refresh or another device does not lose typed notes.
		if (next.draft) ({ note, internalNote } = next.draft)

		hourInit()
		error = ""
		watchChecks()
		schedulePrefetch(next.submission)
		void checkHead()
	} catch (err) {
		if (err instanceof ApiError && err.code === "unauthorized") {
			dropSignIn()
			void goto("/")
			return
		}

		if (target !== id) return

		const message = err instanceof Error ? err.message : "Could not open this ship."
		if (err instanceof ApiError && (err.code === "forbidden" || err.code === "not_found")) {
			bundleCache().remove(target)
			bundle = undefined
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
	acknowledgements = acknowledgements.includes(key)
		? acknowledgements.filter(value => value !== key)
		: [...acknowledgements, key]
}

// ackLabel turns a stored check key into the heading the reviewer saw in Checks.
function ackLabel(key: string): string {
	return bundle?.checks.find(check => check.key === key)?.label ?? key
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
		bundleCache().write(bundle.submission.id, bundle)
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
		flash(actionError)
		return
	}

	if (decision === "approved") {
		if (blocking.length > 0) {
			actionError = "Approval is blocked. Read the blocking check, then ask for changes or reject."
			flash("Approval is blocked by a failing check.")
			return
		}
		if (!secondPass && missingAcks.length > 0) {
			actionError = `Acknowledge ${missingAcks.length} warning${missingAcks.length === 1 ? "" : "s"} before approving.`
			flash(actionError)
			return
		}
	} else if (note.trim() === "") {
		actionError = "Write a note to the maker before returning or rejecting."
		flash("Write a note to the maker first.")
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
		flash("Explain the override before deciding.")
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

// entrySaved keeps the rail total in step with the per-entry adjudication.
function entrySaved(seconds: number): void {
	hours = Math.round((seconds / 3600) * 100) / 100
}

// addNote stores an internal note on this ship and shows it immediately.
async function addNote(body: string): Promise<void> {
	if (!submission) return

	const entry = await createNote(submission.id, { body, subjectType: "submission" })
	if (bundle) bundle.notes = [entry, ...bundle.notes]
}

// removeNote deletes an internal note and drops it from the list.
async function removeNote(entry: Note): Promise<void> {
	await deleteNote(entry.id)
	if (bundle) bundle.notes = bundle.notes.filter(item => item.id !== entry.id)
}

// canDeleteNote lets the author or a member with note management remove a note.
function canDeleteNote(entry: Note): boolean {
	return session.me?.user.email === entry.authorEmail || hasPermission(permissions, permissionNoteManage)
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
	<title>{submission ? `${submission.title} · Ire` : preview ? `${preview.title} · Ire` : "Ire"}</title>
</svelte:head>

{#snippet shipTabs(counts: { bom?: number; checks?: number; evidence?: number; history?: number; journal?: number })}
	<div aria-label="Ship sections" class="ui-tabs ship-tabs" role="tablist">
		{#each [
			{ count: undefined, key: "overview", label: "Overview" },
			{ count: counts.journal, key: "journal", label: "Journal" },
			{ count: counts.checks, key: "checks", label: "Checks" },
			{ count: counts.bom, key: "bom", label: "BOM" },
			{ count: counts.evidence, key: "evidence", label: "Evidence" },
			{ count: counts.history, key: "history", label: "History" },
		] as item (item.key)}
			<button
				aria-selected={tab === item.key}
				class="ui-tab"
				onclick={(): void => selectTab(item.key as Tab)}
				role="tab"
				type="button"
				class:active={tab === item.key}
				class:tab-journal={item.key === "journal"}
			>
				{item.label}
				{#if item.count !== undefined}
					<span class="tab-count">{item.count}</span>
				{/if}
			</button>
		{/each}
	</div>
{/snippet}

{#if loading}
	{#if preview}
		<header class="ship-head">
			<div class="head-main">
				<p class="crumbs"><a href={backPath}>{backLabel}</a></p>
				<div class="title-line">
					<h1>{preview.title}</h1>
					<span class="ui-badge" data-tone={statusTone(preview.status)}
						>{preview.claimedBy === session.me?.user.email ? "Yours" : humanStatus(preview.status)}</span
					>
					<span class="version">v{preview.version}</span>
				</div>
				<p class="maker">
					{preview.makerName}
					{#if preview.makerEmail}
						· {preview.makerEmail}
					{/if}
					{#if preview.makerSlackId}
						<span> · {preview.makerSlackId}</span>
					{/if}
					{#if preview.week}
						<span> · Week {preview.week}</span>
					{/if}
					{#if preview.requestedTier}
						<span> · Tier {preview.requestedTier}</span>
					{/if}
				</p>
			</div>
		</header>

		<div class="layout">
			<section class="content">
				{@render shipTabs({ checks: preview.checkCount })}

				<div class="tab-content" use:swipeTabs>
					{#if tab === "overview"}
						<article class="ui-card padded overview">
							{#if preview.thumbnailUrl && !previewCoverFailed}
								<img
									alt={`${preview.title} project image`}
									class="cover"
									decoding="async"
									loading="lazy"
									onerror={(): void => {
										previewCoverFailed = true
									}}
									src={preview.thumbnailUrl}
								>
							{/if}
							<div class="stats">
								<div>
									<span>Claimed</span><strong>{formatHours(preview.hoursClaimedSeconds)}</strong>
								</div>
								<div>
									<span>Journals</span
									><strong>{formatHours(preview.hoursJournalSeconds ?? 0)}</strong>
								</div>
								<div>
									<span>Hackatime</span
									><strong>{formatHours(preview.hoursHackatimeSeconds ?? 0)}</strong>
								</div>
								<div><span>BOM</span><strong>{formatMoney(preview.bomTotalCents ?? 0)}</strong></div>
							</div>
							<LoadingState message="Loading ship…" />
						</article>
					{:else}
						<div class="ui-card padded">
							<LoadingState message="Loading ship…" />
						</div>
					{/if}
				</div>
			</section>

			<aside class="rail">
				<div class="rail-scroll">
					{#if readOnly}
						<section class="ui-card padded decision">
							<h2>Read-only shadow</h2>
							{#if previewTimeSegments.length > 0}
								<TimeEvidenceBar segments={previewTimeSegments} />
							{:else}
								<div class="preview-time">
									<span>Time evidence</span>
									<div class="preview-time-bar"></div>
								</div>
							{/if}
							<p class="policy">
								This copy is here for review and parity only. Claims, drafts and decisions stay in Ari,
								and Ire sends nothing to the maker.
							</p>
						</section>
					{/if}

					<section class="ui-card padded summary">
						<div class="summary-head">
							<h2>Checks</h2>
							<div class="summary-actions">
								<span
									class="ui-badge"
									data-tone={previewChecks.length > 0
										? statusTone(overallChecks(previewChecks))
										: "neutral"}
								>
									{previewChecks.length > 0 ? overallChecks(previewChecks) : "loading"}
								</span>
							</div>
						</div>
						{#if previewChecks.length > 0}
							<div class="summary-counts">
								<span><strong>{previewPass}</strong> pass</span>
								<span><strong>{previewWarnings.length}</strong> warn</span>
								<span><strong>{previewBlocking.length}</strong> block</span>
							</div>
						{:else}
							<LoadingState message="Loading checks…" />
						{/if}
					</section>
				</div>
			</aside>
		</div>
	{:else}
		<div class="ui-card padded">
			<LoadingState message="Loading ship…" />
		</div>
	{/if}
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
				<span class="ui-badge" data-tone={statusTone(submission.status)}
					>{bundle.claim?.isMine === 1 && submission.status === "claimed"
						? "Yours"
						: humanStatus(submission.status)}</span
				>
				<span class="version">v{submission.version}</span>
			</div>
			<p class="maker">
				{#if submission.makerName}
					{submission.makerName}
					·
				{/if}
				{submission.makerEmail}
				{#if submission.makerSlackId}
					<span>· {submission.makerSlackId}</span>
				{/if}
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
			{const heldHref = reviewerHref(heldReview.reviewerEmail)}
			<div class="first-pass">
				<strong>
					First pass ·
					{#if heldHref}
						<a data-sveltekit-reload href={heldHref}>{heldReview.reviewerEmail}</a>
					{:else}
						{heldReview.reviewerEmail}
					{/if}
				</strong>
				{#if heldReview.noteToMaker}
					<p>{heldReview.noteToMaker}</p>
				{/if}
				{#if heldReview.acknowledgements.length > 0}
					<p class="acks">Acknowledged: {heldReview.acknowledgements.map(ackLabel).join(", ")}</p>
				{/if}
			</div>
		{/if}
	{/if}

	<div class="layout">
		<section class="content">
			{@render shipTabs({
				bom: bundle.bom.length,
				checks: bundle.checks.length,
				evidence: bundle.evidence?.files.length,
				history: bundle.reviews.length,
				journal: bundle.journals.length,
			})}

			<div class="tab-content" use:swipeTabs>
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
									>{submission.thumbnailUrl
										? "Project image unavailable"
										: "No project image"}</strong
								>
								<span>
									{submission.thumbnailUrl
										? "The image link could not be loaded."
										: "This ship does not include a project image."}
								</span>
							</div>
						{/if}
						{#if bundle.resubmission}
							<ResubmissionDiff diff={bundle.resubmission} />
						{/if}
						<h2>What the maker built</h2>
						<p class="description">{submission.description || "No description was sent."}</p>

						<div class="stats">
							<div>
								<span>Claimed</span><strong>{formatHours(submission.hoursClaimedSeconds)}</strong>
							</div>
							<div>
								<span>Journals</span><strong>{formatHours(submission.hoursJournalSeconds)}</strong>
							</div>
							<div>
								<span>Hackatime</span><strong>{formatHours(submission.hoursHackatimeSeconds)}</strong>
							</div>
							<div><span>BOM</span><strong>{formatMoney(submission.bomTotalCents)}</strong></div>
						</div>

						<PriorReviews reviews={bundle.priorReviews} />

						<ReviewTimeline events={bundle.events} />

						<div class="section">
							<h3>Internal notes</h3>
							<NotesCard
								canDelete={canDeleteNote}
								notes={bundle.notes}
								onAdd={addNote}
								onRemove={removeNote}
							/>
						</div>
					</article>
					<div class="split-journals">
						<JournalList
							editable={canAdjustEntries}
							entries={bundle.journals}
							onEntrySaved={entrySaved}
							shipId={submission.id}
							version={submission.version}
						/>
					</div>
				{:else if tab === "checks"}
					<ChecksTab
						{acknowledgements}
						checks={bundle.checks}
						onAcknowledgeAll={acknowledgeAll}
						onToggle={toggleAck}
					/>
				{:else if tab === "journal"}
					<JournalList
						editable={canAdjustEntries}
						entries={bundle.journals}
						onEntrySaved={entrySaved}
						shipId={submission.id}
						version={submission.version}
					/>
				{:else if tab === "bom"}
					<BomTab bom={bundle.bom} totalCents={submission.bomTotalCents} />
				{:else if tab === "evidence"}
					<EvidenceTab
						canRefresh={canSecondPass}
						error={evidenceError}
						evidence={bundle.evidence}
						onRefresh={refreshEvidenceNow}
						refreshing={refreshingEvidence}
						submissionId={bundle.submission.id}
					/>
				{:else if tab === "history"}
					<HistoryTab reviews={bundle.reviews} />
				{/if}
			</div>
		</section>

		<aside class="rail">
			<div class="rail-scroll">
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
							Ire sends nothing to the maker.
						</p>
					</section>
				{:else}
					<section class="ui-card padded decision">
						<h2>Decision</h2>

						<TimeEvidenceBar segments={timeSegments} />

						<label class="field">
							<span>Approved hours</span>
							<span class="hours-row">
								<input
									aria-label="Approved hours"
									class="ui-input"
									max="720"
									min="0"
									step="0.25"
									type="number"
									bind:value={hours}
								>
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
								aria-label="Note to maker"
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
								aria-label="Internal note"
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
									<span aria-hidden="true" class="field-count"
										>{overrideReason.length}
										/ {maxNote}</span
									>
								</span>
								<textarea
									aria-label="Override reason"
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

						{#if secondPass && canSecondPass}
							<div class="send-back">
								<h3>Send back to stage 1</h3>
								<p>
									Invalidate the held review and let a reviewer look again. No event is sent to the
									maker.
								</p>
								<textarea
									aria-label="Reason to send back"
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
							{const claimHref = reviewerHref(bundle.claim.reviewerEmail)}
							<p class="claim">
								Claimed by
								{#if claimHref}
									<a data-sveltekit-reload href={claimHref}>{bundle.claim.reviewerEmail}</a>
								{:else}
									{bundle.claim.reviewerEmail}
								{/if}
								until {formatDateTime(bundle.claim.expiresAt)}
							</p>
						{/if}
					</section>
				{/if}

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
								data-tone={bundle.checks.length > 0
									? statusTone(overallChecks(bundle.checks))
									: checking
										? "pending"
										: "neutral"}
							>
								{bundle.checks.length > 0
									? overallChecks(bundle.checks)
									: checking
										? "checking"
										: "none"}
							</span>
						</div>
					</div>
					<div class="summary-counts">
						<span
							><strong>{bundle.checks.filter(check => check.status === "pass").length}</strong>
							pass</span
						>
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
			</div>

			{#if !readOnly && !reviewed}
				<div class="rail-actions">
					{#if actionError}
						<p class="ui-notice" data-tone="bad" role="alert">{actionError}</p>
					{:else if checking}
						<p class="rail-hint">Waiting for the repository checks to finish.</p>
					{:else if blocking.length > 0}
						<p class="rail-hint">Approval is blocked by a failing check.</p>
					{:else if !secondPass && missingAcks.length > 0}
						<p class="rail-hint">
							{`${missingAcks.length} warning${missingAcks.length === 1 ? "" : "s"} left to acknowledge.`}
						</p>
					{:else if secondPass && !canSecondPass}
						<p class="rail-hint">Only organisers can decide the second pass.</p>
					{/if}

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
h1 {
	font-size: var(--text-2xl);
	overflow-wrap: anywhere;
}

.missing {
	max-width: 32.5rem;
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
	grid-template-columns: minmax(0, 1fr) minmax(18.75rem, 22.5rem);
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

.split-journals {
	display: none;
}

.tab-content {
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
	max-height: 26.25rem;
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
	min-height: 12.5rem;
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

.resubmission details {
	padding-top: var(--space-2);
	font-size: var(--text-sm);
}

.resubmission summary {
	color: var(--text-2);
	cursor: pointer;
}

.change-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	padding: var(--space-2) 0 0 var(--space-3);
	margin: 0;
	font-size: var(--text-sm);
	color: var(--text-2);
	list-style: none;
}

.change-list .added {
	color: var(--accent-green);
}

.change-list .removed {
	color: var(--accent-red);
}

.event {
	display: flex;
	gap: var(--space-2);
	padding: var(--space-1) 0;
	font-size: var(--text-sm);
}

.event .dot {
	flex: none;
	width: 0.5rem;
	height: 0.5rem;
	margin-top: 0.5rem;
	background: var(--primary);
	border-radius: 50%;
}

.event small {
	display: block;
	font-size: var(--text-xs);
	color: var(--text-3);
}

.table-wrap {
	overflow-x: auto;
}

.table-wrap table {
	min-width: 35rem;
}

.rail {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	padding-top: var(--space-4);
}

/* The time bar is only known after the bundle arrives, so the rail preview reserves its height. */
.preview-time {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding-top: var(--space-3);
}

.preview-time span {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-3);
	text-transform: uppercase;
	letter-spacing: 0.04em;
}

.preview-time-bar {
	height: 0.75rem;
	background: var(--surface-3);
	border-radius: var(--radius-full);
}

.rail-scroll {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
}

.rail-actions {
	position: sticky;
	bottom: var(--space-4);
	z-index: 2;
	display: grid;
	flex: none;
	gap: var(--space-2);
	padding: var(--space-3);
	background: var(--surface);
	border: 1px solid var(--border);
	border-radius: var(--radius-lg);
	box-shadow: var(--shadow);
}

.rail-hint {
	font-size: var(--text-xs);
	font-weight: 600;
	color: var(--status-pending);
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

.first-pass a,
.prior-row a,
.claim a {
	color: var(--text-2);
	text-decoration: none;
}

.first-pass a:hover,
.prior-row a:hover,
.claim a:hover {
	color: var(--text);
	text-decoration: underline;
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

@media (max-width: 61.25rem) {
	.layout {
		grid-template-columns: 1fr;
		padding-bottom: 4.75rem;
	}

	.rail {
		padding-top: 0;
	}

	.rail-actions {
		display: none;
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
		height: 2.75rem;
	}

	.shortcuts-button {
		display: none;
	}

	.toast {
		bottom: calc(4.75rem + var(--space-3));
	}
}

@media (max-width: 40rem) {
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

/* Wide screens show the journals beside the overview, so the journal tab hides. */
@media (min-width: 100rem) {
	.tab-content:has(> .split-journals) {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
		gap: var(--space-3);
		align-items: start;
	}

	.split-journals {
		display: block;
	}

	.ship-tabs .tab-journal {
		display: none;
	}
}
</style>
