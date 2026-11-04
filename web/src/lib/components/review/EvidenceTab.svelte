<script lang="ts">
import type { Bundle } from "../../api"
import { formatDateTime } from "../../format"
import Markdown from "./Markdown.svelte"

let {
	evidence,
	canRefresh,
	refreshing,
	error,
	onRefresh,
}: {
	evidence: Bundle["evidence"]
	canRefresh: boolean
	refreshing: boolean
	error: string
	onRefresh(): void
} = $props()

let fileQuery = $state("")
let commitQuery = $state("")
let depth = $state("0")

const filteredFiles = $derived(
	(evidence?.files ?? []).filter(file => {
		const needle = fileQuery.trim().toLowerCase()
		const levels = depth === "0" ? 0 : Number(depth)
		const slash = file.path.split("/").length - 1

		return (needle === "" || file.path.toLowerCase().includes(needle)) && (levels === 0 || slash < levels)
	}),
)

const filteredCommits = $derived(
	(evidence?.commits ?? []).filter(commit => {
		const needle = commitQuery.trim().toLowerCase()
		if (needle === "") return true

		return (
			commit.message.toLowerCase().includes(needle)
			|| commit.author.toLowerCase().includes(needle)
			|| commit.sha.toLowerCase().includes(needle)
		)
	}),
)
</script>

{#if !evidence}
	<div class="ui-card ui-empty">
		<strong>No repository snapshot</strong>
		<span>Evidence is fetched for GitHub ships on ingest. Fixtures keep their prescribed checks.</span>
	</div>
{:else}
	<div class="evidence-meta">
		<div>
			<strong>{evidence.fullName}</strong>
			<span>
				{evidence.files.length}
				files · {evidence.commits.length} commits · fetched
				{formatDateTime(evidence.fetchedAt)}
			</span>
		</div>
		{#if canRefresh}
			<button class="ui-button secondary" disabled={refreshing} onclick={onRefresh} type="button">
				{refreshing ? "Refreshing…" : "Refresh"}
			</button>
		{/if}
	</div>

	{#if error}
		<div class="ui-notice" data-tone="bad">{error}</div>
	{/if}

	<div class="evidence-grid">
		<section class="ui-card padded">
			<h3>Files</h3>
			{#if evidence.files.length === 0}
				<p class="muted">The file tree was not fetched.</p>
			{:else}
				<div class="filters">
					<input
						aria-label="Filter files"
						class="ui-input"
						placeholder="Filter files"
						type="search"
						bind:value={fileQuery}
					>
					<select aria-label="File depth" class="ui-input" bind:value={depth}>
						<option value="0">All depths</option>
						<option value="1">Top level</option>
						<option value="2">2 levels</option>
						<option value="3">3 levels</option>
					</select>
				</div>
				{#if filteredFiles.length === 0}
					<p class="muted">No files match that filter.</p>
				{:else}
					<ul class="file-tree">
						{#each filteredFiles.slice(0, 80) as file (file.path)}
							<li><span class="file-kind">{file.kind}</span><code>{file.path}</code></li>
						{/each}
					</ul>
					{#if filteredFiles.length > 80}
						<p class="muted">and {filteredFiles.length - 80} more files</p>
					{/if}
				{/if}
			{/if}
		</section>

		<section class="ui-card padded">
			<h3>Commits</h3>
			{#if evidence.commits.length === 0}
				<p class="muted">No commits were fetched.</p>
			{:else}
				<div class="filters">
					<input
						aria-label="Search commits"
						class="ui-input"
						placeholder="Search message, author or SHA"
						type="search"
						bind:value={commitQuery}
					>
				</div>
				{#if filteredCommits.length === 0}
					<p class="muted">No commits match that search.</p>
				{/if}
				<ul class="commit-list">
					{#each filteredCommits.slice(0, 30) as commit (commit.sha)}
						<li>
							<strong>{commit.message || "(no message)"}</strong>
							<span
								>{commit.author}
								· {commit.date.slice(0, 10)} · <code>{commit.sha.slice(0, 7)}</code></span
							>
						</li>
					{/each}
				</ul>
				{#if filteredCommits.length > 30}
					<p class="muted">and {filteredCommits.length - 30} more commits</p>
				{/if}
			{/if}
		</section>
	</div>

	{#if evidence.readme}
		<section class="ui-card padded readme">
			<h3>README</h3>
			<div class="readme-body"><Markdown text={evidence.readme} /></div>
		</section>
	{/if}
{/if}

<style>
.evidence-meta {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
	justify-content: space-between;
	padding: var(--space-2) var(--space-3);
	margin-top: var(--space-3);
	background: var(--surface-2);
	border-radius: var(--radius-md);
}

.evidence-meta div {
	display: flex;
	flex-direction: column;
	font-size: var(--text-sm);
}

.evidence-meta span {
	color: var(--text-3);
}

.filters {
	display: flex;
	gap: var(--space-2);
	margin-bottom: var(--space-2);
}

.filters .ui-input {
	min-width: 0;
}

.evidence-grid {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
	gap: var(--space-3);
	margin-top: var(--space-3);
}

.evidence-grid h3,
.readme h3 {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-3);
	text-transform: uppercase;
	letter-spacing: 0.04em;
}

.file-tree,
.commit-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	max-height: 320px;
	margin-top: var(--space-2);
	overflow: auto;
	list-style: none;
}

.file-tree li {
	display: flex;
	gap: var(--space-2);
	align-items: baseline;
	min-width: 0;
	font-size: var(--text-sm);
}

.file-tree code {
	overflow-wrap: anywhere;
}

.file-kind {
	flex: 0 0 3.5rem;
	font-size: var(--text-xs);
	color: var(--text-3);
	text-transform: uppercase;
}

.commit-list li {
	display: flex;
	flex-direction: column;
	gap: 2px;
	padding: var(--space-1) 0;
	font-size: var(--text-sm);
}

.commit-list span {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.readme {
	margin-top: var(--space-3);
}

.readme-body {
	max-height: 420px;
	margin-top: var(--space-2);
	overflow: auto;
}
</style>
