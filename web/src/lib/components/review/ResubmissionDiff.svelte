<script lang="ts">
import { type Bundle } from "../../api"
import { decisionLabel, formatDateTime, formatMoneyAmount, statusTone } from "../../format"

interface Props {
	diff: NonNullable<Bundle["resubmission"]>
}

let { diff }: Props = $props()
</script>

<div class="section resubmission">
	<h3>Since v{diff.previousVersion}</h3>
	{#if diff.previousDecision || diff.previousNote}
		<div class="prior-row">
			{#if diff.previousDecision}
				<span
					class="ui-badge"
					data-tone={statusTone(diff.previousDecision === "approved" ? "approved" : "changes")}
					>{decisionLabel(diff.previousDecision)}</span
				>
			{/if}
			{#if diff.previousNote}
				<p>{diff.previousNote}</p>
			{/if}
			{#if diff.previousReviewer || diff.previousReviewedAt}
				<small>
					{diff.previousReviewer ?? "Reviewer"}
					{#if diff.previousReviewedAt}
						· {formatDateTime(diff.previousReviewedAt)}
					{/if}
				</small>
			{/if}
		</div>
	{/if}
	<div class="stats">
		<div><span>Commits</span><strong>{diff.commits.length}</strong></div>
		<div><span>Files</span><strong>+{diff.addedFiles.length} / -{diff.removedFiles.length}</strong></div>
		<div>
			<span>New entries</span
			><strong
				>{diff.newEntries.length}{diff.changedEntries.length > 0
					? ` · ${diff.changedEntries.length} edited`
					: ""}</strong
			>
		</div>
		<div>
			<span>BOM</span
			><strong
				>{diff.bomAdded.length}
				added{diff.bomPriceChanges.length > 0 ? ` · ${diff.bomPriceChanges.length} repriced` : ""}</strong
			>
		</div>
	</div>
	{#if diff.commits.length > 0}
		<details>
			<summary>{diff.commits.length} new commit{diff.commits.length === 1 ? "" : "s"}</summary>
			<ul class="change-list">
				{#each diff.commits.slice(0, 10) as commit (commit.sha)}
					<li><code class="ui-mono">{commit.sha.slice(0, 7)}</code> {commit.message}</li>
				{/each}
			</ul>
		</details>
	{/if}
	{#if diff.addedFiles.length + diff.removedFiles.length > 0}
		<details>
			<summary>{diff.addedFiles.length} added, {diff.removedFiles.length} removed</summary>
			<ul class="change-list">
				{#each diff.addedFiles.slice(0, 20) as file (file)}
					<li class="added">+ {file}</li>
				{/each}
				{#each diff.removedFiles.slice(0, 20) as file (file)}
					<li class="removed">- {file}</li>
				{/each}
			</ul>
		</details>
	{/if}
	{#if diff.bomAdded.length + diff.bomRemoved.length + diff.bomPriceChanges.length > 0}
		<details>
			<summary>BOM changes</summary>
			<ul class="change-list">
				{#each diff.bomAdded as part (part)}
					<li class="added">+ {part}</li>
				{/each}
				{#each diff.bomRemoved as part (part)}
					<li class="removed">- {part}</li>
				{/each}
				{#each diff.bomPriceChanges as change (change.name)}
					<li>
						{change.name}: {formatMoneyAmount(change.beforeCents)} → {formatMoneyAmount(change.afterCents)}
					</li>
				{/each}
			</ul>
		</details>
	{/if}
</div>

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
@media (max-width: 40rem) {
	.stats {
		grid-template-columns: repeat(2, minmax(0, 1fr));
	}
}
</style>
