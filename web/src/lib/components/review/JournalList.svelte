<script lang="ts">
import { type SvelteSet } from "svelte/reactivity"
import { type Journal } from "../../api"
import { formatDateTime, formatHours } from "../../format"
import JournalTime from "./JournalTime.svelte"
import Markdown from "./Markdown.svelte"
import TimelapseClip from "./TimelapseClip.svelte"

interface Props {
	collapsed: SvelteSet<string>
	editable: boolean
	entries: Journal[]
	onEntrySaved(seconds: number): void
	shipId: string
	version: number
}

let { collapsed, editable, entries, onEntrySaved, shipId, version }: Props = $props()

function toggleEntry(id: string): void {
	if (collapsed.has(id)) collapsed.delete(id)
	else collapsed.add(id)
}
</script>

{#if entries.length === 0}
	<div class="ui-card ui-empty">
		<strong>No journal entries</strong>
		<span>No written evidence was sent with this ship.</span>
	</div>
{:else}
	<div class="ui-card journals-card">
		{#each entries as entry (entry.id)}
			<section class="journal-entry">
				<h3 class="journal-heading">
					<button
						aria-expanded={!collapsed.has(entry.id)}
						class="journal-toggle"
						onclick={(): void => toggleEntry(entry.id)}
						type="button"
					>
						<span class="journal-title">{entry.title || "Work session"}</span>
						<span class="journal-hours">{formatHours(entry.seconds)}</span>
						{#if entry.approvedSeconds !== undefined && entry.approvedSeconds < entry.seconds}
							<span class="ui-badge" data-tone="changes"
								>Deflated to {formatHours(entry.approvedSeconds)}</span
							>
						{/if}
						<span class="journal-date">{formatDateTime(entry.at)}</span>
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
					{#if (entry.timelapses ?? []).length > 0}
						<div class="clip-grid">
							{#each entry.timelapses ?? [] as clip, index (clip.externalId ?? index)}
								<TimelapseClip
									{clip}
									clipIndex={index}
									{editable}
									{entry}
									onSaved={onEntrySaved}
									{shipId}
									{version}
								/>
							{/each}
						</div>
					{/if}
					<JournalTime {editable} {entry} onSaved={onEntrySaved} {shipId} {version} />
				</div>
			</section>
		{/each}
	</div>
{/if}

<style>
.journals-card {
	overflow: hidden;
}

.journal-entry + .journal-entry {
	border-top: 1px solid var(--border);
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
	padding: var(--space-2) var(--space-4);
	font: inherit;
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-2);
	text-align: left;
	cursor: pointer;
	background: var(--surface-2);
	border: 0;
}

.journal-toggle:hover {
	color: var(--text);
	background: var(--surface-3);
}

.journal-title {
	font-weight: 700;
	color: var(--text);
}

.journal-hours {
	font-weight: 500;
	color: var(--text-3);
}

.journal-date {
	margin-left: auto;
	font-weight: 500;
	color: var(--text-3);
	white-space: nowrap;
}

.chevron {
	flex: none;
	color: var(--text-3);
	transition: transform 0.15s;
}

.chevron.collapsed {
	transform: rotate(-90deg);
}

.journal-body {
	padding: 0 var(--space-4) var(--space-4);
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

.clip-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(10rem, 1fr));
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

.journal-entry :global(p) {
	padding-top: var(--space-2);
	font-size: var(--text-sm);
	color: var(--text-2);
	overflow-wrap: anywhere;
	white-space: pre-wrap;
}

.journal-entry small {
	display: block;
	padding-top: var(--space-1);
	font-size: var(--text-xs);
	color: var(--text-3);
}

.journal-entry .ui-badge {
	margin-top: 0;
}
</style>
