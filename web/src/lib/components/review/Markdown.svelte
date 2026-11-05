<script lang="ts">
import { parseMarkdown } from "../../markdown"

let { text }: { text: string } = $props()

const blocks = $derived(parseMarkdown(text))
</script>

<div class="markdown">
	{#each blocks as block, index (index)}
		{#if block.kind === "heading"}
			<svelte:element this={`h${Math.min(block.level + 2, 6)}`}>{block.text}</svelte:element>
		{:else if block.kind === "code"}
			<pre><code>{block.text}</code></pre>
		{:else if block.kind === "list"}
			<ul>
				{#each block.items as item, itemIndex (itemIndex)}
					<li>{item}</li>
				{/each}
			</ul>
		{:else if block.kind === "quote"}
			<blockquote>{block.text}</blockquote>
		{:else}
			<p>{block.text}</p>
		{/if}
	{/each}
</div>

<style>
.markdown {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.markdown h3,
.markdown h4,
.markdown h5,
.markdown h6 {
	font-size: var(--text-md);
	font-weight: 700;
}

.markdown p,
.markdown li,
.markdown blockquote {
	font-size: var(--text-sm);
	line-height: 1.6;
}

.markdown ul {
	padding-left: var(--space-4);
	list-style: disc;
}

.markdown blockquote {
	padding-left: var(--space-3);
	color: var(--text-2);
	border-left: 0.125rem solid var(--border);
}

.markdown pre {
	padding: var(--space-2);
	overflow: auto;
	font-family: var(--font-mono);
	font-size: var(--text-xs);
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}
</style>
