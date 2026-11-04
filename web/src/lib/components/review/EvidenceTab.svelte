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

type EvidenceFile = NonNullable<Bundle["evidence"]>["files"][number]

interface FileTreeNode {
	children: FileTreeNode[]
	directory: boolean
	kind: string
	name: string
	path: string
}

interface FileTreeRow {
	depth: number
	directory: boolean
	kind: string
	name: string
	path: string
}

// compareFileNodes puts directories first, then sorts both groups by name.
function compareFileNodes(a: FileTreeNode, b: FileTreeNode): number {
	if (a.directory !== b.directory) return a.directory ? -1 : 1

	return a.name.localeCompare(b.name)
}

// flattenFileTree walks the directory nodes into indented rows.
function flattenFileTree(nodes: FileTreeNode[], rows: FileTreeRow[], level: number): void {
	for (const node of nodes.toSorted(compareFileNodes)) {
		rows.push({ depth: level, directory: node.directory, kind: node.kind, name: node.name, path: node.path })
		if (node.directory) flattenFileTree(node.children, rows, level + 1)
	}
}

// buildFileRows turns the flat path list into indented directory and file rows.
function buildFileRows(files: EvidenceFile[]): FileTreeRow[] {
	const roots: FileTreeNode[] = []
	const directories = new Map<string, FileTreeNode>()

	for (const file of files) {
		const parts = file.path.split("/").filter(Boolean)
		if (parts.length === 0) continue

		let siblings = roots
		let path = ""

		for (let index = 0; index < parts.length; index++) {
			const name = parts[index]
			if (!name) continue

			path = path === "" ? name : `${path}/${name}`

			if (index === parts.length - 1) {
				siblings.push({ children: [], directory: false, kind: file.kind, name, path })
				continue
			}

			let directory = directories.get(path)
			if (!directory) {
				directory = { children: [], directory: true, kind: "directory", name, path }
				directories.set(path, directory)
				siblings.push(directory)
			}

			siblings = directory.children
		}
	}

	const rows: FileTreeRow[] = []
	flattenFileTree(roots, rows, 0)

	return rows
}

const fileRows = $derived(buildFileRows(filteredFiles.slice(0, 80)))
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
		<div class="ui-notice evidence-error" data-tone="bad">{error}</div>
	{/if}

	<div class="evidence-grid">
		<section class="ui-card padded">
			<div class="card-head">
				<h3>Files</h3>
				<span class="card-count">{evidence.files.length} file{evidence.files.length === 1 ? "" : "s"}</span>
			</div>
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
						{#each fileRows as row (row.path)}
							<li
								style={`padding-left: calc(${row.depth} * var(--space-3))`}
								title={row.path}
								class:directory={row.directory}
							>
								<span class="file-kind">{row.directory ? "dir" : row.kind}</span>
								<code>{row.name}{row.directory ? "/" : ""}</code>
							</li>
						{/each}
					</ul>
					{#if filteredFiles.length > 80}
						<p class="muted">and {filteredFiles.length - 80} more files</p>
					{/if}
				{/if}
			{/if}
		</section>

		<section class="ui-card padded">
			<div class="card-head">
				<h3>Commits</h3>
				<span class="card-count"
					>{evidence.commits.length}
					commit{evidence.commits.length === 1 ? "" : "s"}</span
				>
			</div>
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

.evidence-error {
	margin-top: var(--space-3);
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

.card-head {
	display: flex;
	gap: var(--space-2);
	align-items: baseline;
	justify-content: space-between;
}

.card-count {
	font-size: var(--text-xs);
	font-variant-numeric: tabular-nums;
	color: var(--text-3);
	white-space: nowrap;
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

.file-tree .directory code {
	font-weight: 700;
	color: var(--text);
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
