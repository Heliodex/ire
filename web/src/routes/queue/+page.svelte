<script lang="ts">
import { goto } from "$app/navigation"
import { onMount } from "svelte"
import { ApiError } from "../../lib/api-error"
import { claimShip, fetchMe, fetchQueue, type Queue, type QueueItem } from "../../lib/api"
import { formatHours, humanStatus, statusTone, timeAgo } from "../../lib/format"
import { session } from "../../lib/session.svelte.js"

type Filter = "all" | "fast" | "attention" | "mine"

let queue = $state<Queue>({ counts: { ready: 0, claimed: 0, needsAttention: 0, stage2Hold: 0 }, items: [] })
let filter = $state<Filter>("all")
let loading = $state(true)
let opening = $state("")
let error = $state("")
let selected = $state(0)
let search = $state("")

const filters: { key: Filter; label: string }[] = [
	{ key: "all", label: "All open" },
	{ key: "fast", label: "Fast lane" },
	{ key: "attention", label: "Needs a look" },
	{ key: "mine", label: "Mine" },
]

const visible = $derived(
	queue.items.filter((item) => {
		if (search.trim() === "") return true
		const needle = search.trim().toLowerCase()
		return item.title.toLowerCase().includes(needle) || item.makerName.toLowerCase().includes(needle)
	}),
)

onMount((): (() => void) => {
	void boot()
	const timer = setInterval((): void => void load(), 10000)

	function onKeydown(event: KeyboardEvent): void {
		if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return

		if (event.key === "j") {
			selected = Math.min(selected + 1, visible.length - 1)
			event.preventDefault()
		} else if (event.key === "k") {
			selected = Math.max(selected - 1, 0)
			event.preventDefault()
		} else if (event.key === "Enter" && visible[selected]) {
			void open(visible[selected])
			event.preventDefault()
		}
	}

	window.addEventListener("keydown", onKeydown)

	return (): void => {
		clearInterval(timer)
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
		queue = await fetchQueue(filter)
		selected = Math.min(selected, Math.max(queue.items.length - 1, 0))
		error = ""
	} catch (err) {
		if (err instanceof ApiError && err.code === "unauthorized") {
			void goto("/")
			return
		}
		error = err instanceof Error ? err.message : "Could not load the queue."
	} finally {
		loading = false
	}
}

async function choose(next: Filter): Promise<void> {
	filter = next
	loading = true
	selected = 0
	await load()
}

async function open(item: QueueItem): Promise<void> {
	opening = item.id
	error = ""
	try {
		if (!item.claimedBy || item.claimedBy !== session.me?.user.email) await claimShip(item.id)
		await goto(`/review?id=${encodeURIComponent(item.id)}`)
	} catch (err) {
		error = err instanceof Error ? err.message : "Could not claim this ship."
		await load()
	} finally {
		opening = ""
	}
}
</script>

<svelte:head>
	<title>Queue · Half Life Review</title>
</svelte:head>

<section class="head">
	<div>
		<p class="eyebrow">Stage 1 · first pass</p>
		<h1>Queue</h1>
		<p class="lede">
			Clear the boards that are ready, give the warnings one look, and send the blocked ones back with a note.
		</p>
	</div>
	<div class="counts" aria-label="Queue counts">
		<div class="count ready"><strong>{queue.counts.ready}</strong><span>Ready</span></div>
		<div class="count claimed"><strong>{queue.counts.claimed}</strong><span>In review</span></div>
		<div class="count attention"><strong>{queue.counts.needsAttention}</strong><span>Needs a look</span></div>
		<div class="count hold"><strong>{queue.counts.stage2Hold}</strong><span>Second pass</span></div>
	</div>
</section>

<div class="toolbar">
	<div class="tabs" role="tablist" aria-label="Filters">
		{#each filters as option (option.key)}
			<button
				aria-selected={filter === option.key}
				class:active={filter === option.key}
				onclick={(): Promise<void> => choose(option.key)}
				role="tab"
				type="button"
			>
				{option.label}
			</button>
		{/each}
	</div>
	<label class="search">
		<span class="visually-hidden">Search the queue</span>
		<input bind:value={search} placeholder="Search project or maker…" type="search">
	</label>
</div>

{#if error}
	<div class="error" role="alert">{error}</div>
{/if}

<section aria-busy={loading} aria-labelledby="list-title" class="list">
	<div class="list-head">
		<h2 id="list-title">Open ships</h2>
		<span>{visible.length} {visible.length === 1 ? "ship" : "ships"} · <kbd>J</kbd><kbd>K</kbd> to move · <kbd>Enter</kbd> to open</span>
	</div>

	{#if loading}
		<div class="skeletons">
			<div></div>
			<div></div>
			<div></div>
		</div>
	{:else if visible.length === 0}
		<div class="empty">
			<p>{search ? "No ships match that search." : "Nothing in this filter."}</p>
			<span>{filter === "attention" ? "The checks found nothing blocking." : "Try another filter or check back after the next ingest."}</span>
		</div>
	{:else}
		<div class="rows">
			{#each visible as item, index (item.id)}
				<button
					aria-current={index === selected}
					class="row"
					class:selected={index === selected}
					class:blocked={item.status === "needs_attention"}
					disabled={opening !== ""}
					onclick={(): Promise<void> => open(item)}
					type="button"
				>
					<div class="main">
						<div class="title-line">
							<h3>{item.title}</h3>
							<span class="status {statusTone(item.status)}">{humanStatus(item.status)}</span>
							{#if item.theme}
								<span class="theme">{item.theme}</span>
							{/if}
						</div>
						<p class="meta">
							<span>{item.makerName}</span>
							<span>v{item.version}</span>
							<span>{formatHours(item.hoursClaimedSeconds)} claimed</span>
							<span>{timeAgo(item.ingestedAt)}</span>
							{#if item.claimedBy}
								<span class="claim">{item.claimedBy === session.me?.user.email ? "Yours" : `Claimed by ${item.claimedBy}`}</span>
							{/if}
						</p>
						<div class="checks" aria-label="Check summary">
							{#if item.checkSummary.pass > 0}<span class="pip good">{item.checkSummary.pass} pass</span>{/if}
							{#if item.checkSummary.warn > 0}<span class="pip warn">{item.checkSummary.warn} warn</span>{/if}
							{#if item.checkSummary.fail > 0}<span class="pip bad">{item.checkSummary.fail} fail</span>{/if}
							{#if item.checkSummary.error > 0}<span class="pip error">{item.checkSummary.error} error</span>{/if}
							{#if item.checkSummary.overall === "pass"}<span class="pip quiet">Fast lane</span>{/if}
						</div>
					</div>
					<div class="action">
						<span>{opening === item.id ? "Claiming…" : "Open"}</span>
						<svg aria-hidden="true" fill="none" height="16" viewBox="0 0 16 16" width="16">
							<path d="M3 8h9M8.5 3.5 13 8l-4.5 4.5" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"/>
						</svg>
					</div>
				</button>
			{/each}
		</div>
	{/if}
</section>

<style>
.head {
	display: flex;
	flex-wrap: wrap;
	gap: 28px;
	align-items: flex-end;
	justify-content: space-between;
	padding-bottom: 26px;
}

.eyebrow {
	padding-bottom: 9px;
	font-size: 0.68rem;
	font-weight: 750;
	color: var(--accent);
	text-transform: uppercase;
	letter-spacing: 0.13em;
}

h1 {
	font-size: clamp(2.1rem, 4.5vw, 3.2rem);
}

.lede {
	max-width: 640px;
	padding-top: 10px;
	font-size: 0.94rem;
	color: var(--muted);
}

.counts {
	display: grid;
	grid-template-columns: repeat(4, minmax(78px, 1fr));
	gap: 8px;
}

.count {
	padding: 10px 12px;
	background: var(--card);
	border: 1px solid var(--line);
	border-radius: 12px;
}

.count strong {
	display: block;
	font-size: 1.3rem;
	font-weight: 800;
	letter-spacing: -0.03em;
}

.count span {
	font-size: 0.64rem;
	color: var(--muted);
	text-transform: uppercase;
	letter-spacing: 0.07em;
}

.count.ready strong {
	color: var(--teal);
}

.count.attention strong {
	color: var(--accent);
}

.toolbar {
	display: flex;
	flex-wrap: wrap;
	gap: 14px;
	align-items: center;
	justify-content: space-between;
	padding-bottom: 16px;
}

.tabs {
	display: flex;
	gap: 4px;
	padding: 4px;
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

.tabs button:hover {
	color: var(--ink);
}

.tabs button.active {
	color: var(--ink);
	background: var(--card);
	box-shadow: 0 2px 8px rgba(60, 45, 20, 0.08);
}

.search input {
	width: min(280px, 70vw);
	padding: 9px 12px;
	font-size: 0.82rem;
	color: var(--ink);
	background: var(--card);
	border: 1px solid var(--line);
	border-radius: 9px;
	outline: none;
}

.search input:focus {
	border-color: var(--accent);
}

.list-head {
	display: flex;
	flex-wrap: wrap;
	gap: 12px;
	align-items: baseline;
	justify-content: space-between;
	padding-bottom: 10px;
}

.list-head h2 {
	font-size: 1.06rem;
}

.list-head span {
	font-size: 0.72rem;
	color: var(--muted);
}

kbd {
	padding: 1px 5px;
	margin: 0 1px;
	font-family: inherit;
	font-size: 0.66rem;
	font-weight: 700;
	background: var(--paper-deep);
	border: 1px solid var(--line);
	border-radius: 4px;
}

.rows {
	display: grid;
	gap: 8px;
}

.row {
	display: grid;
	grid-template-columns: 1fr auto;
	gap: 18px;
	align-items: center;
	padding: 15px 16px;
	text-align: left;
	background: var(--card);
	border: 1px solid var(--line);
	border-radius: 13px;
	transition: border-color 120ms ease, box-shadow 120ms ease, transform 120ms ease;
}

.row:hover:not(:disabled) {
	border-color: var(--ink);
	box-shadow: 0 6px 18px rgba(60, 45, 20, 0.06);
}

.row.selected {
	border-color: var(--accent);
	box-shadow: 0 0 0 1px var(--accent);
}

.row.blocked {
	border-left: 3px solid var(--accent);
}

.row:disabled {
	cursor: wait;
}

.title-line {
	display: flex;
	flex-wrap: wrap;
	gap: 8px;
	align-items: baseline;
}

.title-line h3 {
	font-size: 1.02rem;
	font-weight: 750;
}

.status {
	padding: 2px 7px;
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

.theme {
	font-size: 0.68rem;
	color: var(--muted);
}

.meta {
	display: flex;
	flex-wrap: wrap;
	gap: 6px 14px;
	padding-top: 6px;
	font-size: 0.73rem;
	color: var(--muted);
}

.claim {
	color: var(--accent-deep);
}

.checks {
	display: flex;
	flex-wrap: wrap;
	gap: 6px;
	padding-top: 9px;
}

.pip {
	padding: 2px 7px;
	font-size: 0.64rem;
	font-weight: 700;
	border-radius: 999px;
}

.pip.good {
	color: var(--teal);
	background: var(--teal-soft);
}

.pip.warn {
	color: var(--accent-deep);
	background: var(--accent-soft);
}

.pip.bad,
.pip.error {
	color: #7d2a12;
	background: #fbe5df;
}

.pip.quiet {
	color: var(--muted);
	background: var(--paper-deep);
}

.action {
	display: flex;
	gap: 6px;
	align-items: center;
	font-size: 0.74rem;
	font-weight: 700;
	color: var(--muted);
}

.row:hover .action {
	color: var(--accent);
}

.skeletons {
	display: grid;
	gap: 8px;
}

.skeletons div {
	height: 88px;
	background: linear-gradient(90deg, var(--card), var(--paper-deep), var(--card));
	background-size: 200% 100%;
	border: 1px solid var(--line);
	border-radius: 13px;
	animation: shimmer 1.4s infinite;
}

.empty {
	padding: 42px;
	text-align: center;
	background: var(--card);
	border: 1px dashed var(--line);
	border-radius: 13px;
}

.empty p {
	font-weight: 750;
}

.empty span {
	display: block;
	padding-top: 5px;
	font-size: 0.82rem;
	color: var(--muted);
}

.error {
	padding: 10px 13px;
	margin-bottom: 14px;
	font-size: 0.84rem;
	color: #7d2a12;
	background: #fbe5df;
	border: 1px solid #e9b9aa;
	border-radius: 10px;
}

.visually-hidden {
	position: absolute;
	width: 1px;
	height: 1px;
	overflow: hidden;
	clip: rect(0 0 0 0);
	white-space: nowrap;
}

@keyframes shimmer {
	to {
		background-position: -200% 0;
	}
}

@media (max-width: 760px) {
	.counts {
		grid-template-columns: repeat(2, minmax(90px, 1fr));
		width: 100%;
	}

	.row {
		grid-template-columns: 1fr;
	}

	.action {
		justify-content: flex-end;
	}
}
</style>
