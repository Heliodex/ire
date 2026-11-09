<script lang="ts">
import { SvelteSet } from "svelte/reactivity"
import { type Journal } from "../../api"
import { formatDateTime, formatHours } from "../../format"
import JournalTime from "./JournalTime.svelte"
import Markdown from "./Markdown.svelte"

interface Props {
	editable: boolean
	entries: Journal[]
	onEntrySaved(seconds: number): void
	shipId: string
	version: number
}

let { editable, entries, onEntrySaved, shipId, version }: Props = $props()

const collapsed = new SvelteSet<string>()

function toggleEntry(id: string): void {
	if (collapsed.has(id)) collapsed.delete(id)
	else collapsed.add(id)
}
</script>

{#if entries.length === 0}
	<div class="ui-card ui-empty">
		<strong>No journal entries</strong>
		<span>Half Life sent no written evidence with this ship.</span>
	</div>
{:else}
	<div class="journals">
		{#each entries as entry (entry.id)}
			<article class="ui-card padded journal">
				<h3 class="journal-heading">
					<button
						aria-expanded={!collapsed.has(entry.id)}
						class="journal-toggle"
						onclick={(): void => toggleEntry(entry.id)}
						type="button"
					>
						<span class="journal-title">{entry.title || "Work session"}</span>
						<span class="journal-meta">{formatHours(entry.seconds)} · {formatDateTime(entry.at)}</span>
						<svg
							aria-hidden="true"
							class="chevron"
							fill="none"
							height="14"
							viewBox="0 0 24 24"
							width="14"
							class:collapsed={collapsed.has(entry.id)}
						>
							<path
								d="m6 9 6 6 6-6"
								stroke="currentColor"
								stroke-linecap="round"
								stroke-linejoin="round"
								stroke-width="2"
							/>
						</svg>
					</button>
				</h3>
				<div class="journal-body" class:hidden={collapsed.has(entry.id)}>
					{#if entry.clipCount > 0}
						<span class="ui-badge" data-tone="approved"
							>{entry.clipCount}
							clip{entry.clipCount === 1 ? "" : "s"}
							· {formatHours(entry.coveredSeconds)} covered</span
						>
					{/if}
					{#if entry.markdown || entry.text}
						<Markdown text={entry.markdown || entry.text || ""} />
					{:else}
						<p>No written entry.</p>
					{/if}
					{#if entry.media.length > 0}
						<div class="media-grid">
							{#each entry.media as image (image.url)}
								<a href={image.url} rel="noreferrer" target="_blank" title="Open full image">
									<img
										alt={image.alt ?? "Journal image"}
										loading="lazy"
										referrerpolicy="no-referrer"
										src={image.url}
									>
								</a>
							{/each}
						</div>
					{:else if entry.mediaCount > 0}
						<small
							>{entry.mediaCount}
							image{entry.mediaCount === 1 ? "" : "s"}
							attached</small
						>
					{/if}
					<JournalTime {editable} {entry} onSaved={onEntrySaved} {shipId} {version} />
				</div>
			</article>
		{/each}
	</div>
{/if}

<style>
.journals {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
}

.journal-heading {
	margin: 0;
	font: inherit;
}

.journal-toggle {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: baseline;
	width: 100%;
	padding: 0;
	font: inherit;
	color: inherit;
	text-align: left;
	cursor: pointer;
	background: none;
	border: 0;
}

.journal-title {
	font-weight: 700;
}

.journal-meta {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.chevron {
	flex: none;
	margin-left: auto;
	color: var(--text-3);
	transition: transform 0.15s;
}

.chevron.collapsed {
	transform: rotate(-90deg);
}

.journal-body {
	padding-top: var(--space-2);
}

.journal-body.hidden {
	display: none;
}

.journal-body > :global(.markdown:first-child > .md-image:first-child) {
	margin-top: var(--space-2);
}

.media-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(8rem, 1fr));
	gap: var(--space-2);
	padding-top: var(--space-2);
}

.media-grid a {
	display: block;
	overflow: hidden;
	border: 1px solid var(--border);
	border-radius: var(--radius-md);
}

.media-grid img {
	display: block;
	width: 100%;
	aspect-ratio: 4 / 3;
	object-fit: cover;
	background: var(--surface-2);
}

.journal :global(p) {
	padding-top: var(--space-2);
	font-size: var(--text-sm);
	color: var(--text-2);
	overflow-wrap: anywhere;
	white-space: pre-wrap;
}

.journal small {
	display: block;
	padding-top: var(--space-1);
	font-size: var(--text-xs);
	color: var(--text-3);
}

.journal .ui-badge {
	margin-top: var(--space-2);
}
</style>
