<script lang="ts">
import type { Review } from "../../api"
import { formatDateTime, formatHours } from "../../format"
import { reviewerHref } from "../../reviewer"

let { reviews }: { reviews: Review[] } = $props()
</script>

{#if reviews.length === 0}
	<div class="ui-card ui-empty">
		<strong>No decisions yet</strong>
		<span>Stage 1 and the 2nd pass will appear here.</span>
	</div>
{:else}
	<ol class="history">
		{#each reviews as review (review.id)}
			{const href = reviewerHref(review.reviewerEmail)}
			<li class="ui-card padded" class:invalidated={review.invalidatedAt}>
				<div class="history-head">
					<span class="ui-badge">Stage {review.stage}</span>
					<strong>{review.decision}</strong>
					{#if review.held === 1}
						<span class="ui-badge" data-tone="secondpass">Held</span>
					{/if}
					{#if review.invalidatedAt}
						<span class="ui-badge" data-tone="warn">Invalidated</span>
					{/if}
					<span class="history-when">
						{#if href}
							<a data-sveltekit-reload {href} title={review.reviewerEmail}
								>{review.reviewerName ?? review.reviewerEmail}</a
							>
						{:else}
							{review.reviewerName ?? review.reviewerEmail}
						{/if}
						· {formatDateTime(review.createdAt)}
					</span>
				</div>
				{#if review.hoursApprovedSeconds !== undefined}
					<p>{formatHours(review.hoursApprovedSeconds)} approved</p>
				{/if}
				{#if review.heldReason}
					<p class="muted">Held because {review.heldReason}.</p>
				{/if}
				{#if review.noteToMaker}
					<p>{review.noteToMaker}</p>
				{/if}
				{#if review.overrideReason}
					<p class="muted">Override: {review.overrideReason}</p>
				{/if}
				{#if review.acknowledgements.length > 0}
					<p class="muted">Acknowledged: {review.acknowledgements.join(", ")}</p>
				{/if}
				{#if review.invalidatedAt}
					<p class="muted">Sent back {formatDateTime(review.invalidatedAt)}.</p>
				{/if}
			</li>
		{/each}
	</ol>
{/if}

<style>
.history {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	margin-top: var(--space-3);
	list-style: none;
}

.history-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: baseline;
	padding-bottom: var(--space-1);
}

.history-when {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.history-when a {
	color: var(--text-2);
	text-decoration: none;
}

.history-when a:hover {
	color: var(--text);
	text-decoration: underline;
}

.history p {
	padding-bottom: var(--space-1);
	font-size: var(--text-sm);
}

.history .invalidated {
	opacity: 0.72;
}
</style>
