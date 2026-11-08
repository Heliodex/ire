<script lang="ts">
import { type Bundle } from "../../api"
import { decisionLabel, formatDateTime, statusTone } from "../../format"
import { reviewerHref } from "../../reviewer"

interface Props {
	reviews: Bundle["priorReviews"]
}

let { reviews }: Props = $props()
</script>

{#if reviews.length > 0}
	<div class="section">
		<h3>Prior review</h3>
		{#each reviews as prior (prior.at)}
			{const priorHref = reviewerHref(prior.reviewer?.includes("@") ? prior.reviewer : null)}
			<div class="prior-row">
				<span
					class="ui-badge"
					data-tone={statusTone(prior.result.toLowerCase().includes("approved") ? "approved" : "changes")}
					>{decisionLabel(prior.result)}</span
				>
				<p>{prior.feedback}</p>
				<small>
					{#if priorHref}
						<a data-sveltekit-reload href={priorHref}>{prior.reviewer}</a>
					{:else}
						{prior.reviewer ?? "Reviewer"}
					{/if}
					· {formatDateTime(prior.at)}
				</small>
			</div>
		{/each}
	</div>
{/if}

<style>
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
.prior-row a {
	color: inherit;
	text-decoration: underline;
	text-decoration-color: var(--border-2);
}
.prior-row a:hover {
	text-decoration-color: currentColor;
}
</style>
