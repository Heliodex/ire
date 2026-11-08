<script lang="ts">
import { iconPaths } from './icon-paths';
import type { IconName } from './icon-paths';

interface Props {
	class?: string
	name: IconName
	size?: number
	strokeWidth?: number
}

let { class: className = "", name, size = 20, strokeWidth = 1.9 }: Props = $props()
</script>

<svg
	aria-hidden="true"
	class={className}
	fill="none"
	height={size}
	stroke="currentColor"
	stroke-linecap="round"
	stroke-linejoin="round"
	stroke-width={strokeWidth}
	viewBox="0 0 24 24"
	width={size}
>
	{#each iconPaths[name] as path (path)}
		<path d={path} />
	{/each}
</svg>
