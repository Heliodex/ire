<script lang="ts">
import type { Journal } from "../../api"
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
				<div class="journal-head">
					<strong>{entry.title || "Work session"}</strong>
					<span>{formatHours(entry.seconds)} · {formatDateTime(entry.at)}</span>
				</div>
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

.journal-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: baseline;
	justify-content: space-between;
}

.journal-head span {
	font-size: var(--text-xs);
	color: var(--text-3);
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
