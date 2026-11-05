<script lang="ts">
interface Props {
	name: string
	url?: string | null
}

let { name, url }: Props = $props()
let failed = $state(false)
const initials = $derived.by(() => {
	const parts = name.split(/\s+/).filter(Boolean)
	const letters = parts.length > 1 ? parts.map(part => part[0]).join("") : name.slice(0, 2)
	return letters.slice(0, 2).toUpperCase() || "?"
})
</script>

{#if url && !failed}
	<img
		alt=""
		class="avatar"
		decoding="async"
		loading="lazy"
		onerror={(): void => {
			failed = true
		}}
		referrerpolicy="no-referrer"
		src={url}
	>
{:else}
	<span aria-hidden="true" class="avatar placeholder">{initials}</span>
{/if}

<style>
.avatar {
	display: block;
	flex: none;
	width: 1.25rem;
	height: 1.25rem;
	overflow: hidden;
	object-fit: cover;
	object-position: center;
	background: var(--surface-2);
	border: 1px solid var(--border);
	border-radius: 0.35rem;
}

.placeholder {
	display: grid;
	place-items: center;
	font-size: 0.55rem;
	font-weight: 700;
	color: var(--text-3);
}
</style>
