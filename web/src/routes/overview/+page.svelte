<script lang="ts">
import { onMount } from "svelte"
import { goto } from "$app/navigation"
import { fetchMe, fetchOverview, type Overview } from "../../lib/api"
import { ApiError } from "../../lib/api-error"
import LoadingState from "../../lib/components/LoadingState.svelte"
import { decisionLabel, humanStatus, statusTone } from "../../lib/format"
import { reviewerHref } from "../../lib/reviewer"
import { dropSignIn, session } from "../../lib/session.svelte.js"

let overview = $state<Overview | null>(null)
let loading = $state(true)
let error = $state("")

// formatWait renders a wait the way the Stasis readout does: hours below a day, days above.
function formatWait(ms: number): string {
	const hours = Math.floor(ms / 3_600_000)
	if (hours < 1) return "<1h"
	if (hours < 24) return `${hours}h`

	return `${Math.floor(hours / 24)}d ${hours % 24}h`
}

const waitRows = $derived(
	overview
		? [
				{ label: "P50", value: formatWait(overview.wait.p50Ms) },
				{ label: "P95", value: formatWait(overview.wait.p95Ms) },
				{ label: "Max", value: formatWait(overview.wait.maxMs) },
			]
		: [],
)

const openTotal = $derived(overview?.byStatus.reduce((sum, row) => sum + row.count, 0) ?? 0)
const dayMax = $derived(Math.max(1, ...(overview?.daily.flatMap(day => [day.decided, day.ingested]) ?? [1])))
const boardMax = $derived(Math.max(1, ...(overview?.leaderboard.map(row => row.count) ?? [1])))
const outcomeTotal = $derived(overview?.outcomes.reduce((sum, row) => sum + row.count, 0) ?? 0)

onMount((): (() => void) => {
	let cancelled = false
	const timer = setInterval((): void => void load(true), 30_000)

	async function load(quiet: boolean): Promise<void> {
		if (!quiet) loading = true
		error = ""

		try {
			session.me ??= await fetchMe()
			const next = await fetchOverview()
			if (!cancelled) overview = next
		} catch (err) {
			if (cancelled) return
			if (err instanceof ApiError && err.code === "unauthorized") {
				dropSignIn()
				void goto("/")
				return
			}
			error = err instanceof Error ? err.message : "Could not load the overview."
		} finally {
			if (!cancelled) loading = false
		}
	}

	void load(false)

	return (): void => {
		cancelled = true
		clearInterval(timer)
	}
})
</script>

<svelte:head>
	<title>Overview · Ire</title>
</svelte:head>

<section class="page-head">
	<div>
		<h1>Overview</h1>
		<p>How Half Life reviews are going.</p>
	</div>
</section>

{#if loading && !overview}
	<div class="ui-card list">
		<LoadingState message="Loading the overview…" />
	</div>
{:else if error && !overview}
	<div class="ui-card ui-empty">
		<strong>{error}</strong>
		<span>Check back after the next ingest.</span>
	</div>
{:else if overview}
	<section aria-label="Programme totals" class="stats">
		<div class="ui-card padded stat" data-tone="info">
			<strong>{overview.needsReview}</strong>
			<span>Needs review</span>
		</div>
		<div class="ui-card padded stat">
			<strong>{overview.newToday}</strong>
			<span>New today</span>
		</div>
		<div class="ui-card padded stat" data-tone="ok">
			<strong>{overview.reviewedToday}</strong>
			<span>Reviewed today</span>
		</div>
		<div class="ui-card padded stat">
			<strong>{overview.wait.count > 0 ? formatWait(overview.wait.p50Ms) : "–"}</strong>
			<span>Median wait{overview.wait.count > 0 ? ` across ${overview.wait.count}` : ""}</span>
		</div>
	</section>

	<section aria-label="Wait times" class="ui-card padded wait">
		<div class="wait-head">
			<h2>Wait time</h2>
			<span>Oldest-first across the open queue</span>
		</div>
		<div class="wait-rows">
			{#each waitRows as row (row.label)}
				<div class="wait-row">
					<span class="wait-label">{row.label}</span>
					<span class="wait-value">{row.value}</span>
				</div>
			{/each}
		</div>
		{#if overview.wait.oldestId && overview.wait.oldestTitle}
			<a class="oldest" href={`/review/${encodeURIComponent(overview.wait.oldestId)}`}>
				Longest waiting: {overview.wait.oldestTitle} · {formatWait(overview.wait.maxMs)}
			</a>
		{/if}
	</section>

	<div class="panels">
		<section aria-label="Queue flow per day" class="ui-card padded panel">
			<h2>Queue flow</h2>
			<p>Ingested vs decided · last 14 days</p>
			<div class="legend" aria-hidden="true">
				<span class="legend-item"><span class="dot decided"></span>Decided</span>
				<span class="legend-item"><span class="dot ingested"></span>Ingested</span>
			</div>
			<div class="bars">
				{#each overview.daily as day (day.date)}
					<div
						class="bar-col"
						title={`${day.date}: ${day.ingested} ingested, ${day.decided} decided`}
					>
						<div class="bar-track">
							<div class="bar-fill decided" style:height={`${(day.decided / dayMax) * 100}%`}></div>
							<div class="bar-fill ingested" style:height={`${(day.ingested / dayMax) * 100}%`}></div>
						</div>
						<span class="bar-tick">{day.date.slice(5)}</span>
					</div>
				{/each}
			</div>
		</section>

		<section aria-label="Open ships by status" class="ui-card padded panel">
			<h2>Open by status</h2>
			<p>{openTotal} {openTotal === 1 ? "ship" : "ships"} waiting</p>
			<ul class="breakdown">
				{#each overview.byStatus as row (row.status)}
					<li>
						<span class="ui-badge" data-tone={statusTone(row.status)}>{humanStatus(row.status)}</span>
						<span class="meter"
							><span style:width={`${openTotal > 0 ? (row.count / openTotal) * 100 : 0}%`}></span></span
						>
						<span class="amount">{row.count}</span>
					</li>
				{/each}
			</ul>
		</section>
	</div>

	<div class="panels flip">
		<section aria-label="Review turnaround" class="ui-card padded panel">
			<h2>Turnaround</h2>
			<p>
				{#if overview.turnaround.decisions > 0}
					Across {overview.turnaround.decisions} decision{overview.turnaround.decisions === 1 ? "" : "s"} in 30 days
				{:else}
					No decisions in the last 30 days
				{/if}
			</p>
			<div class="wait-rows">
				<div class="wait-row">
					<span class="wait-label">To first review</span>
					<span class="wait-value"
						>{overview.turnaround.decisions > 0 ? formatWait(overview.turnaround.medianFirstMs) : "–"}</span
					>
				</div>
				<div class="wait-row">
					<span class="wait-label">To decision</span>
					<span class="wait-value"
						>{overview.turnaround.decisions > 0 ? formatWait(overview.turnaround.medianDecisionMs) : "–"}</span
					>
				</div>
			</div>
		</section>

		<section aria-label="Decisions by outcome" class="ui-card padded panel">
			<h2>Outcomes</h2>
			<p>Final decisions · last 30 days</p>
			{#if overview.outcomes.length === 0}
				<span class="muted">No decisions in the last 30 days.</span>
			{:else}
				<ul class="breakdown">
					{#each overview.outcomes as row (row.decision)}
						<li>
							<span class="ui-badge" data-tone={statusTone(row.decision)}>{decisionLabel(row.decision)}</span>
							<span class="meter"
								><span
									style:width={`${outcomeTotal > 0 ? (row.count / outcomeTotal) * 100 : 0}%`}
								></span></span
							>
							<span class="amount">{row.count}</span>
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	</div>

	<section aria-label="Reviewer leaderboard" class="ui-card padded panel">
		<h2>Reviewer leaderboard</h2>
		<p>Last 30 days</p>
		{#if overview.leaderboard.length === 0}
			<span class="muted">No reviews in the last 30 days.</span>
		{:else}
			<ol class="board">
				{#each overview.leaderboard as row, rank (row.email ?? row.name ?? rank)}
					<li>
						<span class="rank">{rank + 1}</span>
						{#if reviewerHref(row.email)}
							<a class="name" href={reviewerHref(row.email)}>{row.name || row.email}</a>
						{:else}
							<span class="name">{row.name || "Unknown"}</span>
						{/if}
						<span class="meter board-meter"
							><span style:width={`${(row.count / boardMax) * 100}%`}></span></span
						>
						<span class="amount">{row.count}</span>
					</li>
				{/each}
			</ol>
		{/if}
	</section>
{/if}

<style>
.page-head {
	padding-bottom: var(--space-5);
}

h1 {
	font-size: var(--text-2xl);
}

.page-head p,
.panel p {
	padding-top: var(--space-1);
	font-size: var(--text-sm);
	color: var(--text-2);
}

.stats {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
	gap: var(--space-3);
	padding-bottom: var(--space-4);
}

.stat strong {
	display: block;
	font-size: var(--text-2xl);
	font-weight: 800;
	font-variant-numeric: tabular-nums;
}

.stat span {
	font-size: var(--text-sm);
	color: var(--text-3);
}

.wait {
	margin-bottom: var(--space-4);
}

.wait-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: baseline;
	justify-content: space-between;
	padding-bottom: var(--space-2);
}

.wait-head h2,
.panel h2 {
	font-size: var(--text-md);
	font-weight: 700;
}

.wait-head span {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.wait-rows {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(8rem, 1fr));
	gap: var(--space-2) var(--space-4);
}

.wait-row {
	display: flex;
	gap: var(--space-2);
	align-items: baseline;
}

.wait-label {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-3);
	text-transform: uppercase;
	letter-spacing: 0.04em;
}

.wait-value {
	font-size: var(--text-lg);
	font-weight: 800;
	font-variant-numeric: tabular-nums;
}

.oldest {
	display: inline-block;
	padding-top: var(--space-2);
	font-size: var(--text-sm);
	font-weight: 600;
	color: var(--status-pending);
}

.panels {
	display: grid;
	grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
	gap: var(--space-4);
	padding-bottom: var(--space-4);
}

.panels.flip {
	grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
}

.legend {
	display: flex;
	gap: var(--space-3);
	padding-top: var(--space-2);
	font-size: var(--text-xs);
	color: var(--text-2);
}

.legend-item {
	display: inline-flex;
	gap: var(--space-1);
	align-items: center;
}

.dot {
	flex: none;
	width: var(--space-2);
	height: var(--space-2);
	border-radius: 3px;
}

.dot.decided {
	background: var(--primary);
}

.dot.ingested {
	background: var(--accent-blue);
}

.panel {
	min-width: 0;
}

.bars {
	position: relative;
	display: flex;
	gap: var(--space-1);
	align-items: stretch;
	min-height: 7rem;
	padding-top: var(--space-3);
	padding-bottom: var(--space-5);
}

.bar-col {
	position: relative;
	display: flex;
	flex: 1;
	min-width: 0;
}

.bar-track {
	display: flex;
	flex: 1;
	gap: 2px;
	align-items: flex-end;
	width: 100%;
	min-height: 4rem;
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.bar-fill {
	flex: 1;
	min-height: 2px;
	border-radius: var(--radius-sm);
}

.bar-fill.decided {
	background: var(--primary);
}

.bar-fill.ingested {
	background: var(--accent-blue);
}

.bar-tick {
	position: absolute;
	top: calc(100% + var(--space-1));
	left: 0;
	width: 100%;
	font-size: var(--text-xs);
	font-variant-numeric: tabular-nums;
	color: var(--text-3);
	text-align: center;
}

.bar-col:nth-child(even) .bar-tick {
	display: none;
}

.breakdown {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: var(--space-3) 0 0;
	margin: 0;
	list-style: none;
}

.breakdown li {
	display: grid;
	grid-template-columns: 6.5rem minmax(0, 1fr) auto;
	gap: var(--space-2);
	align-items: center;
}

.breakdown .ui-badge {
	justify-self: start;
}

.meter {
	position: relative;
	flex: 1;
	height: 0.5rem;
	overflow: hidden;
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.meter > span {
	position: absolute;
	inset: 0 auto 0 0;
	background: var(--primary);
	border-radius: var(--radius-sm);
}

.amount {
	font-size: var(--text-sm);
	font-variant-numeric: tabular-nums;
	color: var(--text-2);
}

.board {
	display: flex;
	flex-direction: column;
	padding: var(--space-2) 0 0;
	margin: 0;
	list-style: none;
}

.board li {
	display: flex;
	gap: var(--space-3);
	align-items: center;
	padding: var(--space-2) 0;
	border-bottom: 1px solid var(--border);
}

.board li:last-child {
	border-bottom: 0;
}

.rank {
	flex: none;
	width: var(--space-5);
	font-size: var(--text-xs);
	font-variant-numeric: tabular-nums;
	color: var(--text-3);
	text-align: center;
}

.board li:first-child .rank {
	font-weight: 700;
	color: var(--text);
}

.name {
	flex: none;
	width: 9rem;
	overflow: hidden;
	text-overflow: ellipsis;
	font-size: var(--text-sm);
	font-weight: 600;
	white-space: nowrap;
}

a.name {
	color: inherit;
	text-decoration: none;
}

a.name:hover {
	text-decoration: underline;
}

.board-meter {
	flex: 1;
	min-width: 0;
}

.muted {
	font-size: var(--text-sm);
	color: var(--text-3);
}

@media (max-width: 55rem) {
	.panels {
		grid-template-columns: minmax(0, 1fr);
	}
}
</style>
