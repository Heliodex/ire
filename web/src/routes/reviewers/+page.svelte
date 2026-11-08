<script lang="ts">
import { goto } from "$app/navigation"
import { page } from "$app/state"
import { fetchReviewerProfile } from '../../lib/api';
import type { ReviewerProfile } from '../../lib/api';
import { ApiError } from "../../lib/api-error"
import Avatar from "../../lib/components/Avatar.svelte"
import LoadingState from "../../lib/components/LoadingState.svelte"
import { formatDateTime, formatHours, statusTone } from "../../lib/format"
import { dropSignIn } from "../../lib/session.svelte.js"

const roleInfo: { [_: string]: { detail: string; label: string } } = {
	admin: {
		detail: "Manages the roster, sessions and audit log, and can review every stage.",
		label: "Admin",
	},
	auditor: {
		detail: "Read-only access to the queue, reviews and audit log.",
		label: "Auditor",
	},
	organizer: {
		detail: "Reviews first pass and confirms, overrides or returns held second-pass ships.",
		label: "Organiser",
	},
	reviewer: {
		detail: "Claims first-pass ships, acknowledges warnings and records stage 1 decisions.",
		label: "Reviewer",
	},
}

let key = ""
let profile = $state<ReviewerProfile | null>(undefined)
let loading = $state(true)
let error = $state("")
let request = 0

$effect((): void => {
	const path = page.url.pathname
	const next = path.startsWith("/reviewers/")
		? decodeURIComponent(path.slice("/reviewers/".length)).replace(/\/$/, "")
		: ""
	if (next === "") {
		void goto("/queue")
		return
	}

	if (next === key) return

	key = next
	void load(next)
})

async function load(target: string): Promise<void> {
	const current = ++request
	loading = true
	error = ""
	profile = undefined

	try {
		profile = await fetchReviewerProfile(target)
	} catch (err) {
		if (current !== request) return

		if (err instanceof ApiError && err.code === "unauthorized") {
			dropSignIn()
			void goto("/")
			return
		}

		if (err instanceof ApiError && err.code === "not_found")
			error = "Reviewer not found. They may not have reviewed for this programme."
		else error = err instanceof Error ? err.message : "Could not load this reviewer."
	} finally {
		if (current === request) loading = false
	}
}

function roleLabel(role: string | undefined): string {
	return role ? (roleInfo[role]?.label ?? role) : "Not on the roster"
}

function roleDetail(role: string | undefined): string {
	return role
		? (roleInfo[role]?.detail ?? "This account has historical reviews.")
		: "This reviewer has historical reviews but is not on the current roster."
}

function activeLabel(active: boolean | undefined): string {
	if (active === undefined) return "Former reviewer"
	return active ? "Active" : "Inactive"
}

function activeTone(active: boolean | undefined): string {
	if (active === undefined) return "pending"
	return active ? "approved" : "changes"
}
</script>

<svelte:head>
	<title>{profile ? `${profile.name ?? profile.email} · Reviewer` : "Reviewer"} · Ire</title>
</svelte:head>

{#if loading}
	<div class="ui-card padded">
		<LoadingState message="Loading reviewer…" />
	</div>
{:else if error || !profile}
	<div class="ui-card ui-empty">
		<strong>Cannot open this reviewer</strong>
		<span>{error || "This reviewer does not exist."}</span>
		<a class="ui-button" data-variant="outline" href="/queue">Back to the queue</a>
	</div>
{:else}
	<section class="profile-head">
		<div>
			<p class="crumbs"><a href="/queue">Queue</a> / Reviewer</p>
			<div class="identity-summary">
				<Avatar email={profile.email} name={profile.name} size="lg" url={profile.avatarUrl} />
				<div>
					<div class="title-line">
						<h1>{profile.name ?? profile.email}</h1>
						<span class="ui-badge" data-tone={activeTone(profile.active)}
							>{activeLabel(profile.active)}</span
						>
					</div>
					<p class="email">{profile.email}</p>
				</div>
			</div>
		</div>
		<div class="identity">
			<div>
				<span>Role</span>
				<strong>{roleLabel(profile.role)}</strong>
			</div>
			<div>
				<span>Joined</span>
				<strong>{profile.joinedAt ? formatDateTime(profile.joinedAt) : "—"}</strong>
			</div>
			<div>
				<span>Last sign-in</span>
				<strong>{profile.lastLoginAt ? formatDateTime(profile.lastLoginAt) : "Never"}</strong>
			</div>
			<div>
				<span>Last review</span>
				<strong>{profile.stats.lastReviewAt ? formatDateTime(profile.stats.lastReviewAt) : "—"}</strong>
			</div>
		</div>
	</section>

	<div class="grid">
		<section class="ui-card padded">
			<h2>Permissions</h2>
			<p class="role-detail">{roleDetail(profile.role)}</p>
			<dl class="permissions">
				<div>
					<dt>Role</dt>
					<dd>{roleLabel(profile.role)}</dd>
				</div>
				<div>
					<dt>Tracks</dt>
					<dd>{profile.tracks.length > 0 ? profile.tracks.join(", ") : "—"}</dd>
				</div>
				<div>
					<dt>Extra permissions</dt>
					<dd>{profile.permissions.length > 0 ? profile.permissions.join(", ") : "None beyond the role"}</dd>
				</div>
			</dl>
		</section>

		<section class="ui-card padded">
			<h2>Review stats</h2>
			<dl class="stats">
				<div>
					<dt>Total reviews</dt>
					<dd>{profile.stats.total}</dd>
				</div>
				<div>
					<dt>Approved</dt>
					<dd>{profile.stats.approved}</dd>
				</div>
				<div>
					<dt>Changes</dt>
					<dd>{profile.stats.changes}</dd>
				</div>
				<div>
					<dt>Rejected</dt>
					<dd>{profile.stats.rejected}</dd>
				</div>
				<div>
					<dt>Held</dt>
					<dd>{profile.stats.held}</dd>
				</div>
				<div>
					<dt>Second pass</dt>
					<dd>{profile.stats.secondPass}</dd>
				</div>
				<div>
					<dt>Invalidated</dt>
					<dd>{profile.stats.invalidated}</dd>
				</div>
				<div>
					<dt>Hours approved</dt>
					<dd>{formatHours(profile.stats.hoursApprovedSeconds)}</dd>
				</div>
			</dl>
		</section>
	</div>

	<section class="ui-card padded reviews">
		<div class="section-head">
			<h2>Reviews</h2>
			<span>{profile.reviewTotal} {profile.reviewTotal === 1 ? "decision" : "decisions"}</span>
		</div>

		{#if profile.reviews.length === 0}
			<div class="ui-empty">
				<strong>No decisions yet</strong>
				<span>This reviewer has not recorded a stage 1 or second-pass decision.</span>
			</div>
		{:else}
			<ol class="review-list">
				{#each profile.reviews as review (review.id)}
					<li class:invalidated={review.invalidatedAt}>
						<div class="review-head">
							<a class="ship" href={`/review/${encodeURIComponent(review.submissionId)}`}>
								{review.submissionTitle}
							</a>
							<span class="ui-badge" data-tone={statusTone(review.decision)}>{review.decision}</span>
							<span class="ui-badge">Stage {review.stage}</span>
							{#if review.held === 1}
								<span class="ui-badge" data-tone="secondpass">Held</span>
							{/if}
							{#if review.invalidatedAt}
								<span class="ui-badge" data-tone="changes">Invalidated</span>
							{/if}
							<time class="when" datetime={review.createdAt}>{formatDateTime(review.createdAt)}</time>
						</div>
						{#if review.hoursApprovedSeconds !== undefined}
							<p class="hours">{formatHours(review.hoursApprovedSeconds)} approved</p>
						{/if}
						{#if review.invalidatedAt}
							<p class="muted">Invalidated {formatDateTime(review.invalidatedAt)}.</p>
						{/if}
					</li>
				{/each}
			</ol>
			{#if profile.reviewTotal > profile.reviews.length}
				<p class="muted note">
					Showing the latest {profile.reviews.length} of {profile.reviewTotal} decisions.
				</p>
			{/if}
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
	font-size: var(--text-2xl);
}

.email {
	padding-top: var(--space-1);
	font-size: var(--text-sm);
	color: var(--text-2);
}

.identity {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-4);
}

.identity div {
	display: flex;
	flex-direction: column;
	gap: 2px;
}

.identity span {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.identity strong {
	font-size: var(--text-sm);
	font-weight: 600;
	color: var(--text);
}

.grid {
	display: grid;
	grid-template-columns: repeat(2, minmax(0, 1fr));
	gap: var(--space-3);
	margin-bottom: var(--space-3);
}

.grid h2,
.reviews h2 {
	padding-bottom: var(--space-2);
	font-size: var(--text-md);
}

.role-detail {
	font-size: var(--text-sm);
	line-height: 1.5;
	color: var(--text-2);
}

.permissions {
	display: grid;
	gap: var(--space-2);
	padding-top: var(--space-3);
}

.permissions div {
	display: flex;
	gap: var(--space-3);
	align-items: baseline;
	justify-content: space-between;
	padding-bottom: var(--space-1);
	border-bottom: 1px solid var(--border);
}

.permissions div:last-child {
	border-bottom: 0;
}

.permissions dt {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.permissions dd {
	font-size: var(--text-sm);
	font-weight: 600;
	color: var(--text);
	text-align: right;
}

.stats {
	display: grid;
	grid-template-columns: repeat(4, minmax(0, 1fr));
	gap: var(--space-2);
	padding-top: var(--space-3);
}

.stats div {
	display: flex;
	flex-direction: column;
	gap: 2px;
	padding: var(--space-2) var(--space-3);
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.stats dt {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.stats dd {
	margin: 0;
	font-size: var(--text-md);
	font-weight: 700;
	font-variant-numeric: tabular-nums;
	color: var(--text);
}

.reviews {
	margin-bottom: var(--space-4);
}

.section-head {
	display: flex;
	gap: var(--space-3);
	align-items: baseline;
	justify-content: space-between;
}

.section-head span {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.review-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding-top: var(--space-2);
	list-style: none;
}

.review-list li {
	padding: var(--space-2) var(--space-3);
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.review-list li.invalidated {
	opacity: 0.72;
}

.review-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
}

.ship {
	font-size: var(--text-sm);
	font-weight: 600;
	color: var(--text);
	text-decoration: none;
}

.ship:hover {
	text-decoration: underline;
}

.when {
	margin-left: auto;
	font-size: var(--text-xs);
	color: var(--text-3);
	white-space: nowrap;
}

.hours,
.muted {
	font-size: var(--text-sm);
	color: var(--text-2);
}

.note {
	padding-top: var(--space-3);
	font-size: var(--text-xs);
	color: var(--text-3);
}

@media (max-width: 47.5rem) {
	.grid {
		grid-template-columns: minmax(0, 1fr);
	}

	.stats {
		grid-template-columns: repeat(2, minmax(0, 1fr));
	}

	.identity {
		width: 100%;
	}

	.review-head .when {
		margin-left: 0;
	}
}
</style>
