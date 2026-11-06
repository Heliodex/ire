<script lang="ts">
import { goto } from "$app/navigation"
import { page } from "$app/state"
import { fetchMakerProfile, type MakerProfile, type MakerSubmission } from "../../lib/api"
import { ApiError } from "../../lib/api-error"
import Avatar from "../../lib/components/Avatar.svelte"
import LoadingState from "../../lib/components/LoadingState.svelte"
import { decisionLabel, formatDateTime, formatHours, humanStatus, statusTone } from "../../lib/format"
import { dropSignIn } from "../../lib/session.svelte.js"

let email = ""
let profile = $state<MakerProfile | null>(null)
let loading = $state(true)
let error = $state("")
let request = 0

// emailFromURL reads the maker email from /makers/{email}, keeping the query form for old links.
function emailFromURL(): string {
	const parts = page.url.pathname.split("/").filter(Boolean)
	if (parts[0] === "makers" && parts[1]) return decodeURIComponent(parts[1])

	return page.url.searchParams.get("email") ?? ""
}

$effect((): void => {
	const next = emailFromURL().trim()
	if (next === "") {
		void goto("/queue")
		return
	}

	if (next === email) return

	email = next
	void load(next)
})

async function load(target: string): Promise<void> {
	const current = ++request
	loading = true
	error = ""
	profile = null

	try {
		profile = await fetchMakerProfile(target)
	} catch (err) {
		if (current !== request) return

		if (err instanceof ApiError && err.code === "unauthorized") {
			dropSignIn()
			void goto("/")
			return
		}

		if (err instanceof ApiError && err.code === "not_found")
			error = "Maker not found. They may not have a ship in this programme."
		else error = err instanceof Error ? err.message : "Could not load this maker."
	} finally {
		if (current === request) loading = false
	}
}

const openShips = $derived((profile?.submissions ?? []).filter(ship => ship.decision === undefined).length)

function decidedCount(submissions: MakerSubmission[], decision: string): number {
	return submissions.filter(ship => ship.decision?.toLowerCase() === decision).length
}
</script>

<svelte:head>
	<title>{profile ? `${profile.name ?? profile.email} · Maker` : "Maker"} · Ire</title>
</svelte:head>

{#if loading}
	<div class="ui-card padded">
		<LoadingState message="Loading maker…" />
	</div>
{:else if error || !profile}
	<div class="ui-card ui-empty">
		<strong>Cannot open this maker</strong>
		<span>{error || "This maker does not exist."}</span>
		<a class="ui-button" data-variant="outline" href="/queue">Back to the queue</a>
	</div>
{:else}
	<section class="profile-head">
		<div>
			<p class="crumbs"><a href="/queue">Queue</a> / Maker</p>
			<div class="identity-summary">
				<Avatar email={profile.email} name={profile.name} size="lg" url={profile.avatarUrl} />
				<div>
					<div class="title-line">
						<h1>{profile.name ?? profile.email}</h1>
					</div>
					<p class="email">{profile.email}</p>
				</div>
			</div>
		</div>
		<div class="identity">
			<div>
				<span>Ships</span>
				<strong>{profile.submissions.length}</strong>
			</div>
			<div>
				<span>Open</span>
				<strong>{openShips}</strong>
			</div>
			<div>
				<span>Approved</span>
				<strong>{decidedCount(profile.submissions, "approved")}</strong>
			</div>
		</div>
	</section>

	<section class="ui-card padded ships">
		<div class="section-head">
			<h2>Submissions</h2>
			<span
				>{profile.submissions.length}
				{profile.submissions.length === 1 ? "project" : "projects"}</span
			>
		</div>

		{#if profile.submissions.length === 0}
			<div class="ui-empty">
				<strong>No ships yet</strong>
				<span>This maker has not handed in.</span>
			</div>
		{:else}
			<ol class="ship-list">
				{#each profile.submissions as ship (ship.id)}
					<li>
						<div class="ship-head">
							<a class="ship" href={`/review/${encodeURIComponent(ship.id)}`}>
								{ship.title}
							</a>
							<span class="ui-badge" data-tone={statusTone(ship.status)}>{humanStatus(ship.status)}</span>
							{#if ship.decision !== undefined}
								<span class="ui-badge" data-tone={statusTone(ship.decision)}
									>{decisionLabel(ship.decision)}</span
								>
							{/if}
							<span class="ui-badge">Stage {ship.stage}</span>
							<time class="when" datetime={ship.updatedAt}>{formatDateTime(ship.updatedAt)}</time>
						</div>
						<p class="muted">
							v{ship.version}
							· {formatHours(ship.hoursClaimedSeconds)} claimed
							{#if ship.hoursApprovedSeconds !== undefined}
								· {formatHours(ship.hoursApprovedSeconds)} approved
							{/if}
						</p>
					</li>
				{/each}
			</ol>
		{/if}
	</section>
{/if}

<style>
.profile-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-4);
	align-items: flex-end;
	justify-content: space-between;
	padding-bottom: var(--space-5);
}

.crumbs {
	font-size: var(--text-sm);
	color: var(--text-3);
}

.crumbs a {
	color: var(--text-2);
	text-decoration: none;
}

.crumbs a:hover {
	color: var(--text);
}

.identity-summary {
	display: flex;
	gap: var(--space-3);
	align-items: center;
	padding-top: var(--space-2);
}

.title-line {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: baseline;
	padding-top: var(--space-1);
}

.identity-summary .title-line {
	padding-top: 0;
}

h1 {
	margin: 0;
	font-size: var(--text-xl);
}

.email {
	margin: 0;
	color: var(--text-3);
}

.identity {
	display: flex;
	gap: var(--space-4);
}

.identity > div {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.identity span {
	font-size: var(--text-sm);
	color: var(--text-3);
}

.section-head {
	display: flex;
	align-items: baseline;
	justify-content: space-between;
	padding-bottom: var(--space-3);
}

.section-head h2 {
	margin: 0;
}

.section-head span {
	color: var(--text-3);
}

.ship-list {
	display: flex;
	flex-direction: column;
	padding: 0;
	margin: 0;
	list-style: none;
}

.ship-list > li + li {
	padding-top: var(--space-3);
	margin-top: var(--space-3);
	border-top: 1px solid var(--border);
}

.ship-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
}

.ship {
	font-weight: 700;
	color: var(--text);
	text-decoration: none;
}

.ship:hover {
	text-decoration: underline;
}

.when {
	margin-left: auto;
	color: var(--text-3);
}

.muted {
	color: var(--text-3);
}
</style>
