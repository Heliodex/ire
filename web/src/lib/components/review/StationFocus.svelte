<script lang="ts">
import { onMount } from "svelte"
import { SvelteSet } from "svelte/reactivity"
import { goto } from "$app/navigation"
import { type Bundle, claimShip, fetchBundle, fetchMe, heartbeat, releaseShip, saveDraft } from "../../api"
import { saveStationAnswer } from "../../api.gen"
import { ApiError } from "../../api-error"
import { ignore } from "../../async"
import { isActive, severityTone, statusLabel } from "../../findings"
import { formatHours, humanStatus, statusTone } from "../../format"
import { dropSignIn, session } from "../../session.svelte.js"
import {
	draftValues,
	type MainField,
	parseDraftValues,
	suggestDeflationReason,
	suggestTechnicalFeatures,
} from "../../station-fields"
import { type StationVerdict, upsertStationAnswer } from "../../stations"
import LoadingState from "../LoadingState.svelte"
import BomTab from "./BomTab.svelte"
import JournalList from "./JournalList.svelte"
import Markdown from "./Markdown.svelte"
import StationFields from "./StationFields.svelte"
import StationSheet from "./StationSheet.svelte"
import TimeEvidenceBar from "./TimeEvidenceBar.svelte"

interface Props {
	shipId: string
	stationKey: string
}

interface FileGroup {
	extensions: string[]
	label: string
}

let { shipId, stationKey }: Props = $props()

let bundle = $state<Bundle | undefined>(undefined)
let loading = $state(true)
let error = $state("")
let actionError = $state("")
let notice = $state("")
let coverFailed = $state(false)
let holding = $state(false)
let note = $state("")
let internalNote = $state("")
let technicalFeatures = $state("")
let deflationReason = $state("")
let saveTimer: ReturnType<typeof setTimeout> | undefined
let claimedHere = false
const journalCollapsed = new SvelteSet<string>()

const triageCheckKeys = new Set(["repo.accessible", "repo.bom", "repo.readme", "repo.readme.images"])
const hardwareCheckKeys = new Set(["repo.accessible", "repo.design_files", "repo.files.empty"])
const firmwareExtensions = [
	".ino",
	".c",
	".cc",
	".cpp",
	".cxx",
	".h",
	".hh",
	".hpp",
	".py",
	".rs",
	".uf2",
	".hex",
	".bin",
	".go",
	".ts",
	".js",
]
const boardExtensions = [
	".kicad_pcb",
	".kicad_sch",
	".kicad_pro",
	".brd",
	".sch",
	".pcbdoc",
	".schdoc",
	".fzz",
	".epcb",
	".epro",
	".esch",
]
const cadExtensions = [
	".step",
	".stp",
	".f3d",
	".f3z",
	".fcstd",
	".sldprt",
	".sldasm",
	".iges",
	".igs",
	".stl",
	".obj",
	".3mf",
]
const fileGroups: FileGroup[] = [
	{ extensions: firmwareExtensions, label: "Firmware and code" },
	{ extensions: boardExtensions, label: "PCB and schematic" },
	{ extensions: cadExtensions, label: "CAD and enclosure" },
]

const submission = $derived(bundle?.submission)
const readOnly = $derived(session.me?.readOnly ?? false)
const closed = $derived(submission?.status === "decided" || submission?.status === "withdrawn")
const editable = $derived(!readOnly && !closed && holding)
const stationDefs = $derived(bundle?.stations ?? [])
const station = $derived(stationDefs.find(item => item.key === stationKey))
const stationState = $derived((bundle?.stationStates ?? []).find(item => item.key === stationKey))
const answers = $derived(bundle?.stationAnswers ?? [])
const stationComplete = $derived(stationState?.status === "done")
const stationBlocked = $derived(stationState?.status === "blocked")
const stationLabel = $derived(station?.label ?? stationKey)
const queueHref = $derived(`/station/${encodeURIComponent(stationKey)}`)
const reviewHref = $derived(`/review/${encodeURIComponent(shipId)}?station=synthesis`)
const fullReviewHref = $derived(`/review/${encodeURIComponent(shipId)}`)
const nextKey = $derived(bundle?.currentStation ?? "")
const nextStationKey = $derived(nextKey !== "" && nextKey !== stationKey ? nextKey : "")
const nextLabel = $derived(stationDefs.find(item => item.key === nextStationKey)?.label ?? nextStationKey)
const nextHref = $derived(
	nextStationKey === ""
		? reviewHref
		: `/station/${encodeURIComponent(nextStationKey)}?id=${encodeURIComponent(shipId)}`,
)
const evidence = $derived(bundle?.evidence)
const readme = $derived(evidence?.readme ?? "")
const triageChecks = $derived((bundle?.checks ?? []).filter(check => triageCheckKeys.has(check.key)))
const hardwareChecks = $derived((bundle?.checks ?? []).filter(check => hardwareCheckKeys.has(check.key)))
const timelapseSeconds = $derived((bundle?.journals ?? []).reduce((sum, entry) => sum + entry.coveredSeconds, 0))
const coverage = $derived(
	submission !== undefined && submission.hoursClaimedSeconds > 0
		? Math.round((timelapseSeconds / submission.hoursClaimedSeconds) * 100)
		: 0,
)
const timeSegments = $derived.by((): { label: string; tone: string; value: number }[] => {
	if (submission === undefined) return []

	return [
		{ label: "Journalled", tone: "journals", value: submission.hoursJournalSeconds },
		{ label: "Timelapsed", tone: "lapse", value: timelapseSeconds },
		{ label: "Hackatimed", tone: "hackatime", value: submission.hoursHackatimeSeconds },
	].filter(segment => segment.value > 0)
})
const commits = $derived((evidence?.commits ?? []).slice(0, 10))
const findings = $derived((bundle?.findings ?? []).filter(finding => isActive(finding)))
const hardwareFiles = $derived(
	fileGroups
		.map(group => ({
			files: (evidence?.files ?? []).filter(file =>
				group.extensions.some(extension => file.path.toLowerCase().endsWith(extension)),
			),
			label: group.label,
		}))
		.filter(group => group.files.length > 0),
)

onMount((): (() => void) => {
	void load()

	const beat = setInterval((): void => {
		if (!holding || document.visibilityState !== "visible") return
		ignore(heartbeat(shipId))
	}, 20000)

	return (): void => {
		clearInterval(beat)
		if (saveTimer !== undefined) {
			clearTimeout(saveTimer)
			saveTimer = undefined
			ignore(persistDraft())
		}
		if (claimedHere) ignore(releaseShip(shipId))
	}
})

// canClaim keeps closed and read-only ships out of the claim flow.
function canClaim(next: Bundle): boolean {
	const { status } = next.submission

	return (
		!readOnly && status !== "decided" && status !== "withdrawn" && status !== "checking" && next.claim?.isMine !== 1
	)
}

// applyBundle seeds the draft fields from the server copy of the review draft.
function applyBundle(next: Bundle): void {
	bundle = next
	note = next.draft?.note ?? ""
	internalNote = next.draft?.internalNote ?? ""
	const values = parseDraftValues(next.draft?.values)
	technicalFeatures = values.technicalFeatures ?? ""
	deflationReason = values.deflationReason ?? ""

	if (technicalFeatures === "" && (next.evidence?.files.length ?? 0) > 0)
		technicalFeatures = suggestTechnicalFeatures(next)

	const captured = Math.max(
		next.submission.hoursClaimedSeconds,
		next.submission.hoursJournalSeconds + next.submission.hoursHackatimeSeconds,
	)
	const hasCuts = next.journals.some(
		entry => entry.approvedSeconds !== undefined && entry.approvedSeconds < entry.seconds,
	)
	if (deflationReason === "" && hasCuts) deflationReason = suggestDeflationReason(next, captured)

	holding = next.claim?.isMine === 1
	coverFailed = false
	error = ""
}

// isUnauthorized sends a dead session back to sign-in and reports whether it handled the error.
function isUnauthorized(err: unknown): boolean {
	if (!(err instanceof ApiError && err.code === "unauthorized")) return false

	dropSignIn()
	void goto("/")
	return true
}

// fetchClaimedBundle takes the claim when it is free, then returns the freshest bundle.
async function fetchClaimedBundle(): Promise<Bundle> {
	let next = await fetchBundle(shipId)
	if (!canClaim(next)) return next

	try {
		await claimShip(shipId)
		claimedHere = true
		next = await fetchBundle(shipId)
	} catch (err) {
		if (isUnauthorized(err)) throw err

		notice = err instanceof Error ? err.message : "Another reviewer holds this ship. Showing it read-only."
	}

	return next
}

async function load(): Promise<void> {
	try {
		session.me ??= await fetchMe()
	} catch {
		void goto("/")
		return
	}

	try {
		applyBundle(await fetchClaimedBundle())
	} catch (err) {
		if (isUnauthorized(err)) return

		error = err instanceof Error ? err.message : "Could not open this ship."
	} finally {
		loading = false
	}
}

async function answerStation(
	key: string,
	questionKey: string,
	answer: StationVerdict,
	evidenceText: string,
): Promise<void> {
	if (bundle === undefined) return

	try {
		const result = await saveStationAnswer(bundle.submission.id, key, questionKey, {
			answer,
			evidence: evidenceText,
			version: bundle.submission.version,
		})
		bundle.stationAnswers = upsertStationAnswer(bundle.stationAnswers ?? [], result.answer)
		bundle.stationStates = result.states
		bundle.currentStation = result.currentStation
		actionError = ""
	} catch (err) {
		actionError = err instanceof Error ? err.message : "Could not save this answer."
		throw err
	}
}

// entrySaved keeps the approved total in step after an entry's time changes.
function entrySaved(seconds: number): void {
	if (bundle !== undefined) bundle.submission.hoursApprovedSeconds = seconds
}

// scheduleDraft waits for a pause in typing before writing the station draft.
function scheduleDraft(): void {
	if (!editable) return
	if (saveTimer !== undefined) clearTimeout(saveTimer)
	saveTimer = setTimeout((): void => {
		saveTimer = undefined
		ignore(persistDraft())
	}, 600)
}

function onFieldChange(field: MainField, value: string): void {
	if (field === "technicalFeatures") technicalFeatures = value
	else if (field === "deflationReason") deflationReason = value
	else if (field === "note") note = value
	else internalNote = value
	scheduleDraft()
}

// persistDraft writes the four station field values with the maker and internal notes.
async function persistDraft(): Promise<void> {
	if (bundle === undefined) return

	try {
		await saveDraft(bundle.submission.id, {
			internalNote,
			note,
			values: draftValues({ deflationReason, internalNote, note, technicalFeatures }),
		})
	} catch {
		// Draft saving is best effort; the station answers are the durable writes.
	}
}
</script>

<svelte:head>
	<title>{submission ? `${submission.title} · ${stationLabel} · Ire` : "Station · Ire"}</title>
</svelte:head>

{#snippet checkRows(checks: Bundle["checks"])}
	{#if checks.length === 0}
		<p class="muted">No matching checks were recorded.</p>
	{:else}
		<ul class="check-list">
			{#each checks as check (check.key)}
				<li>
					<span class="ui-badge" data-tone={statusTone(check.status)}>{check.status}</span>
					<span class="check-label">{check.label}</span>
					<span class="check-summary">{check.summary}</span>
				</li>
			{/each}
		</ul>
	{/if}
{/snippet}

{#if loading && bundle === undefined}
	<div class="ui-card">
		<LoadingState message="Loading station evidence…" />
	</div>
{:else if bundle === undefined}
	<div class="ui-notice" data-tone="bad" role="alert">
		{error || "Could not open this ship."}
	</div>
	<p class="queue-link"><a href={queueHref}>Back to the {stationLabel} queue</a></p>
{:else}
	<header class="focus-head">
		<div class="head-main">
			<p class="crumbs">
				<a href={queueHref}>← {stationLabel} queue</a>
				<span aria-hidden="true">·</span>
				<a href={fullReviewHref}>Full review</a>
			</p>
			<div class="title-line">
				<h1>{submission?.title ?? "Ship"}</h1>
				{#if submission}
					<span class="ui-badge" data-tone={statusTone(submission.status)}
						>{humanStatus(submission.status)}</span
					>
					<span class="version">v{submission.version}</span>
				{/if}
			</div>
			{#if submission}
				<p class="meta">
					<span>{submission.makerName || "Unknown maker"}</span>
					{#if submission.theme}
						<span class="meta-chip">{submission.theme}</span>
					{/if}
					{#if submission.phase}
						<span>{submission.phase}</span>
					{/if}
					{#if submission.week}
						<span>Week {submission.week}</span>
					{/if}
					<span>{formatHours(submission.hoursClaimedSeconds)} claimed</span>
					<span>{stationLabel}</span>
				</p>
			{/if}
		</div>
	</header>

	{#if actionError}
		<p class="ui-notice" data-tone="bad" role="alert">{actionError}</p>
	{/if}
	{#if notice}
		<p class="ui-notice" data-tone="warn">{notice}</p>
	{/if}
	{#if error}
		<p class="ui-notice" data-tone="bad" role="alert">{error}</p>
	{/if}

	<div class="focus-layout">
		<div class="focus-evidence">
			{#if stationKey === "triage"}
				<div class="ui-card cover-card">
					{#if submission?.thumbnailUrl && !coverFailed}
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
							<strong
								>{submission?.thumbnailUrl ? "Project image unavailable" : "No project image"}</strong
							>
							<span>
								{submission?.thumbnailUrl
									? "The image link could not be loaded."
									: "This ship does not include a project image."}
							</span>
						</div>
					{/if}
				</div>

				<section class="ui-card padded stack">
					<h2 class="card-title">What they built</h2>
					<p class="description">{submission?.description || "No description was sent."}</p>
					<div class="links">
						{#if submission?.repoUrl}
							<a class="ui-button ghost sm" href={submission.repoUrl} rel="noreferrer" target="_blank"
								>Repository ↗</a
							>
						{/if}
						{#if submission?.demoUrl}
							<a class="ui-button ghost sm" href={submission.demoUrl} rel="noreferrer" target="_blank"
								>Demo ↗</a
							>
						{/if}
					</div>
					<p class="maker-line">
						{submission?.makerName || "Unknown maker"}
						{#if submission?.makerEmail}
							· <a href={`mailto:${submission.makerEmail}`}>{submission.makerEmail}</a>
						{/if}
					</p>
				</section>

				{#if readme !== ""}
					<details class="ui-card padded readme-card">
						<summary>
							<span class="card-title">README</span>
							<span class="muted">Expand</span>
						</summary>
						<div class="readme-body"><Markdown text={readme} /></div>
					</details>
				{:else}
					<div class="ui-card ui-empty">
						<strong>No README</strong>
						<span>The repository snapshot has no README text.</span>
					</div>
				{/if}

				<section class="ui-card padded stack">
					<h2 class="card-title">Repo checks</h2>
					{@render checkRows(triageChecks)}
				</section>
			{:else if stationKey === "time"}
				<JournalList
					collapsed={journalCollapsed}
					{editable}
					entries={bundle.journals}
					onEntrySaved={entrySaved}
					{shipId}
					version={bundle.submission.version}
				/>

				<section class="ui-card padded stack">
					<h2 class="card-title">Time evidence</h2>
					<div class="stats">
						<div>
							<span>Claimed</span><strong>{formatHours(submission?.hoursClaimedSeconds ?? 0)}</strong>
						</div>
						<div>
							<span>Journals</span><strong>{formatHours(submission?.hoursJournalSeconds ?? 0)}</strong>
						</div>
						<div>
							<span>Hackatime</span><strong>{formatHours(submission?.hoursHackatimeSeconds ?? 0)}</strong>
						</div>
						<div>
							<span>Timelapse</span><strong>{formatHours(timelapseSeconds)}</strong>
						</div>
					</div>
					<p class="muted">Timelapse covers {coverage}% of the claimed hours.</p>
					<TimeEvidenceBar segments={timeSegments} />
				</section>
			{:else if stationKey === "narrative"}
				{#if readme !== ""}
					<section class="ui-card padded stack">
						<h2 class="card-title">README</h2>
						<div class="readme-body scroll"><Markdown text={readme} /></div>
					</section>
				{:else}
					<div class="ui-card ui-empty">
						<strong>No README</strong>
						<span>The repository snapshot has no README text.</span>
					</div>
				{/if}

				<section class="ui-card padded stack">
					<div class="card-head">
						<h2 class="card-title">Commits</h2>
						{#if submission?.repoUrl}
							<a class="card-link" href={submission.repoUrl} rel="noreferrer" target="_blank">Repo ↗</a>
						{/if}
					</div>
					{#if commits.length === 0}
						<p class="muted">No commits were fetched.</p>
					{:else}
						<ul class="commit-list">
							{#each commits as commit (commit.sha)}
								<li>
									<strong>{commit.message || "(no message)"}</strong>
									<span
										>{commit.author}
										· {commit.date.slice(0, 10)} · <code>{commit.sha.slice(0, 7)}</code></span
									>
								</li>
							{/each}
						</ul>
					{/if}
				</section>

				<div class="stack">
					<h2 class="section-title">BOM lines</h2>
					<BomTab bom={bundle.bom} totalCents={submission?.bomTotalCents ?? 0} />
				</div>
			{:else if stationKey === "hardware"}
				<div class="stack">
					<h2 class="section-title">BOM</h2>
					<BomTab bom={bundle.bom} totalCents={submission?.bomTotalCents ?? 0} />
				</div>

				{#if hardwareFiles.length === 0}
					<div class="ui-card ui-empty">
						<strong>No design or firmware files</strong>
						<span>No matching file extensions were found in the repository snapshot.</span>
					</div>
				{:else}
					<section class="ui-card padded stack">
						<h2 class="card-title">Design and firmware files</h2>
						{#each hardwareFiles as group (group.label)}
							<div class="file-group">
								<h3 class="file-group-title">
									{group.label} <span class="muted">{group.files.length}</span>
								</h3>
								<ul class="file-list">
									{#each group.files.slice(0, 12) as file (file.path)}
										<li title={file.path}>
											<span class="file-kind">{file.kind}</span>
											<code>{file.path}</code>
										</li>
									{/each}
								</ul>
								{#if group.files.length > 12}
									<p class="muted">and {group.files.length - 12} more files</p>
								{/if}
							</div>
						{/each}
					</section>
				{/if}

				<section class="ui-card padded stack">
					<h2 class="card-title">Design checks</h2>
					{@render checkRows(hardwareChecks)}
				</section>
			{:else if stationKey === "synthesis"}
				<section class="ui-card padded stack">
					<h2 class="card-title">Synthesis and decision</h2>
					<p class="description">
						Station answers are ready. Open the full review to read the decision rail and record the
						outcome.
					</p>
					<div>
						<a class="ui-button primary" href={reviewHref}>Open synthesis and decision</a>
					</div>
				</section>

				<section class="ui-card padded stack">
					<h2 class="card-title">Findings</h2>
					{#if findings.length === 0}
						<p class="muted">No open findings.</p>
					{:else}
						<ul class="finding-list">
							{#each findings as finding (finding.id)}
								<li>
									<div class="finding-head">
										<span class="ui-badge" data-tone={severityTone(finding.severity)}
											>{finding.severity}</span
										>
										<span>{statusLabel(finding.status)}</span>
										<span class="finding-station">{finding.station}</span>
									</div>
									<p class="finding-summary">{finding.summary}</p>
									{#if finding.makerAction}
										<p class="finding-action">Maker action: {finding.makerAction}</p>
									{/if}
								</li>
							{/each}
						</ul>
					{/if}
				</section>
			{:else}
				<div class="ui-card padded stack">
					<h2 class="card-title">No focused evidence for this station</h2>
					<p class="description">Open the full review to see every section.</p>
					<div><a class="ui-button primary" href={reviewHref}>Open full review</a></div>
				</div>
			{/if}
		</div>

		<aside class="focus-rail">
			{#if stationBlocked}
				<section class="ui-card padded complete-card blocked-card">
					<h2>{stationLabel} needs attention</h2>
					<p class="muted">
						A failed answer or open finding blocks this station. Resolve it or return the ship before moving
						on.
					</p>
					<div class="complete-actions">
						<a class="ui-button ghost" href={reviewHref}>Open findings</a>
					</div>
				</section>
			{:else if stationComplete}
				<section class="ui-card padded complete-card">
					<h2>{stationLabel} is complete</h2>
					{#if nextStationKey !== ""}
						<p class="muted">Next up: {nextLabel}.</p>
						<div class="complete-actions">
							<a class="ui-button primary" href={nextHref}>Open {nextLabel}</a>
						</div>
					{:else}
						<p class="muted">Every station is answered. The decision is ready in the full review.</p>
						<div class="complete-actions">
							<a class="ui-button primary" href={reviewHref}>Open synthesis and decision</a>
						</div>
					{/if}
				</section>
			{/if}

			{#if stationDefs.length > 0}
				<StationSheet
					answers={bundle.stationAnswers}
					currentStation={bundle.currentStation}
					{editable}
					focusStation={stationKey}
					onAnswer={answerStation}
					states={bundle.stationStates}
					stations={bundle.stations}
					version={bundle.submission.version}
				/>
			{/if}

			{#if stationKey === "time" || stationKey === "narrative"}
				<StationFields
					{bundle}
					{deflationReason}
					{editable}
					{internalNote}
					{note}
					onChange={onFieldChange}
					{stationKey}
					{technicalFeatures}
				/>
			{/if}

			<p class="queue-link"><a href={queueHref}>Back to queue</a></p>
		</aside>
	</div>
{/if}

<style>
.focus-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-3);
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

.title-line h1 {
	font-size: var(--text-xl);
	overflow-wrap: anywhere;
}

.version {
	font-size: var(--text-sm);
	color: var(--text-3);
}

.meta {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1) var(--space-2);
	padding-top: var(--space-1);
	font-size: var(--text-sm);
	color: var(--text-2);
}

.meta-chip {
	padding: 0 5px;
	background: var(--surface-3);
	border-radius: 99px;
}

.focus-layout {
	display: grid;
	grid-template-columns: minmax(0, 1fr) minmax(19rem, 24rem);
	gap: var(--space-4);
	align-items: start;
	padding-top: var(--space-4);
}

.focus-evidence,
.focus-rail {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	min-width: 0;
}

.cover-card {
	overflow: hidden;
}

.cover {
	display: block;
	width: 100%;
	max-height: 24rem;
	object-fit: contain;
	background: var(--surface-2);
}

.cover-empty {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	align-items: center;
	justify-content: center;
	min-height: 12rem;
	padding: var(--space-6) var(--space-4);
	text-align: center;
	background: var(--surface-2);
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

.stack {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.card-title {
	font-size: var(--text-md);
	font-weight: 700;
}

.section-title {
	font-size: var(--text-sm);
	font-weight: 700;
	color: var(--text-2);
}

.card-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: baseline;
	justify-content: space-between;
}

.card-link {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--primary);
}

.description {
	max-width: 80ch;
	font-size: var(--text-sm);
	color: var(--text-2);
	white-space: pre-wrap;
}

.links {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
}

.maker-line {
	font-size: var(--text-sm);
	color: var(--text-2);
	overflow-wrap: anywhere;
}

.maker-line a {
	color: var(--primary);
}

.readme-card summary {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: baseline;
	justify-content: space-between;
	cursor: pointer;
}

.readme-body {
	max-height: 28rem;
	padding-top: var(--space-2);
	overflow: auto;
}

.readme-body.scroll {
	max-height: 34rem;
}

.stats {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(6rem, 1fr));
	gap: var(--space-1);
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

.check-list,
.commit-list,
.finding-list,
.file-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	padding: 0;
	margin: 0;
	list-style: none;
}

.check-list li {
	display: grid;
	grid-template-columns: 3.5rem minmax(0, 9rem) minmax(0, 1fr);
	gap: var(--space-2);
	align-items: baseline;
	font-size: var(--text-sm);
}

.check-label {
	font-weight: 700;
}

.check-summary {
	color: var(--text-2);
	overflow-wrap: anywhere;
}

.commit-list li {
	display: flex;
	flex-direction: column;
	gap: 2px;
	padding: var(--space-1) 0;
	font-size: var(--text-sm);
	border-bottom: 1px solid var(--border);
}

.commit-list li:last-child {
	border-bottom: 0;
}

.commit-list strong {
	font-weight: 600;
	overflow-wrap: anywhere;
}

.commit-list span {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.commit-list code {
	font-family: var(--font-mono);
}

.file-group + .file-group {
	padding-top: var(--space-2);
	border-top: 1px solid var(--border);
}

.file-group-title {
	display: flex;
	gap: var(--space-2);
	align-items: baseline;
	font-size: var(--text-sm);
	font-weight: 700;
}

.file-list {
	max-height: 14rem;
	margin-top: var(--space-2);
	overflow: auto;
}

.file-list li {
	display: flex;
	gap: var(--space-2);
	align-items: baseline;
	font-size: var(--text-sm);
}

.file-list code {
	overflow-wrap: anywhere;
}

.file-kind {
	flex: 0 0 3.5rem;
	font-size: var(--text-xs);
	color: var(--text-3);
	text-transform: uppercase;
}

.finding-list {
	gap: var(--space-2);
}

.finding-list li {
	padding: var(--space-2) var(--space-3);
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.finding-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
	font-size: var(--text-xs);
	color: var(--text-3);
}

.finding-station {
	padding: 0 5px;
	background: var(--surface-3);
	border-radius: 99px;
}

.finding-summary {
	font-size: var(--text-sm);
	font-weight: 600;
	overflow-wrap: anywhere;
}

.finding-action {
	font-size: var(--text-xs);
	color: var(--text-2);
}

.complete-card h2 {
	font-size: var(--text-md);
	font-weight: 700;
}

.complete-actions {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	padding-top: var(--space-1);
}

.muted {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.queue-link {
	font-size: var(--text-sm);
	color: var(--text-2);
}

.queue-link a {
	color: var(--text-2);
}

.queue-link a:hover {
	color: var(--primary);
}

@media (max-width: 61.25rem) {
	.focus-layout {
		grid-template-columns: minmax(0, 1fr);
	}
}

@media (max-width: 40rem) {
	.check-list li {
		grid-template-columns: 3.5rem minmax(0, 1fr);
	}

	.check-summary {
		grid-column: 1 / -1;
	}
}
</style>
