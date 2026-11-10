<script lang="ts">
import { onMount } from "svelte"
import { goto } from "$app/navigation"
import { fetchMe } from "../../lib/api"
import { fetchStations } from "../../lib/api.gen"
import { ApiError } from "../../lib/api-error"
import LoadingState from "../../lib/components/LoadingState.svelte"
import { dropSignIn, session } from "../../lib/session.svelte.js"

type StationSummary = Awaited<ReturnType<typeof fetchStations>>["stations"][number]

let firstPass = $state<StationSummary[]>([])
let secondPass = $state<StationSummary[]>([])
let loading = $state(true)
let error = $state("")

onMount((): void => {
	void boot()
})

async function boot(): Promise<void> {
	try {
		session.me ??= await fetchMe()
	} catch {
		void goto("/")
		return
	}

	try {
		const [one, two] = await Promise.all([fetchStations(1), fetchStations(2)])
		firstPass = one.stations
		secondPass = two.stations
	} catch (err) {
		if (err instanceof ApiError && err.code === "unauthorized") {
			dropSignIn()
			void goto("/")
			return
		}

		error = err instanceof Error ? err.message : "Could not load the stations."
	} finally {
		loading = false
	}
}
</script>

<svelte:head>
	<title>Stations · Ire</title>
</svelte:head>

<section class="page-head">
	<div>
		<h1>Stations</h1>
		<p>Each station reviews one part of a ship; open a station to work its queue.</p>
	</div>
</section>

{#if error}
	<div class="ui-notice" data-tone="bad" role="alert">{error}</div>
{/if}

{#if loading}
	<div class="ui-card">
		<LoadingState message="Loading stations…" />
	</div>
{:else if firstPass.length === 0 && secondPass.length === 0}
	<div class="ui-card ui-empty">
		<strong>No stations configured</strong>
		<span>This programme has no station workflow yet.</span>
	</div>
{:else}
	{#each [
		{ items: firstPass, label: "First pass", stage: 1 },
		{ items: secondPass, label: "Second pass", stage: 2 },
	] as group (group.stage)}
		{#if group.items.length > 0}
			<section class="stage">
				<h2>{group.label}</h2>
				<div class="grid">
					{#each group.items as station (station.key)}
						<a
							class="ui-card station-card"
							href={`/station/${encodeURIComponent(station.key)}?stage=${group.stage}`}
						>
							<span class="station-copy">
								<strong>{station.label}</strong>
								<span>{station.description || "No description for this station yet."}</span>
							</span>
							<span class="waiting">
								<strong>{station.count}</strong>
								<span>waiting</span>
							</span>
						</a>
					{/each}
				</div>
			</section>
		{/if}
	{/each}
{/if}

<style>
.page-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-4);
	align-items: flex-end;
	justify-content: space-between;
	padding-bottom: var(--space-5);
}

h1 {
	font-size: var(--text-2xl);
}

.page-head p {
	padding-top: var(--space-1);
	font-size: var(--text-sm);
	color: var(--text-2);
}

.stage {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	padding-top: var(--space-4);
}

.stage h2 {
	font-size: var(--text-lg);
}

.grid {
	display: grid;
	grid-template-columns: 1fr;
	gap: var(--space-3);
}

.station-card {
	display: flex;
	gap: var(--space-3);
	align-items: center;
	justify-content: space-between;
	padding: var(--space-2) var(--space-4);
	color: inherit;
	text-decoration: none;
	transition:
		background 0.15s,
		border-color 0.15s,
		transform 0.12s;
}

.station-card:hover {
	background: var(--surface-2);
	border-color: var(--border-2);
}

.station-card:active {
	transform: translateY(1px);
}

.station-copy {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	min-width: 0;
}

.station-copy strong {
	font-size: var(--text-md);
	font-weight: 700;
}

.station-copy span {
	font-size: var(--text-sm);
	color: var(--text-2);
	overflow-wrap: anywhere;
}

.waiting {
	display: flex;
	flex: none;
	flex-direction: column;
	align-items: flex-end;
}

.waiting strong {
	font-size: var(--text-2xl);
	font-weight: 800;
	font-variant-numeric: tabular-nums;
}

.waiting span {
	font-size: var(--text-xs);
	color: var(--text-3);
}
</style>
