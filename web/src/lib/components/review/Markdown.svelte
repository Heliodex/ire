<script lang="ts">
	import { parseMarkdown } from "../../markdown"

	let { imageBase = "", text }: { imageBase?: string; text: string } = $props()

	const blocks = $derived(parseMarkdown(text, imageBase))
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
		{:else if block.kind === "image" && block.image}
			<a class="md-image" href={block.image.url} rel="noreferrer" target="_blank" title="Open full image">
				<img
					alt={block.image.alt || "Journal image"}
					loading="lazy"
					referrerpolicy="no-referrer"
					src={block.image.url}
				>
			</a>
		{:else}
			<p>
				{#if block.inline}
					{#each block.inline as node, nodeIndex (nodeIndex)}
						{#if typeof node === "string"}
							{node}
						{:else}
							<a
								class="md-image inline"
								href={node.url}
								rel="noreferrer"
								target="_blank"
								title="Open full image"
							>
								<img
									alt={node.alt || "Journal image"}
									loading="lazy"
									referrerpolicy="no-referrer"
									src={node.url}
								>
							</a>
						{/if}
					{/each}
				{:else}
					{block.text}
				{/if}
			</p>
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
	border-left: 2px solid var(--border);
}

.markdown pre {
	padding: var(--space-2);
	overflow: auto;
	font-family: var(--font-mono);
	font-size: var(--text-xs);
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.md-image {
	display: block;
	width: fit-content;
	max-width: 100%;
}

.md-image img {
	display: block;
	max-width: 100%;
	height: auto;
	border: 1px solid var(--border);
	border-radius: var(--radius-md);
}

.md-image.inline {
	display: inline-block;
	vertical-align: middle;
}

.md-image.inline img {
	max-height: 20rem;
}
</style>
