<script lang="ts">
interface Props {
	email?: string | null
	name?: string | null
	size?: "xs" | "sm" | "md" | "lg"
	url?: string | null
}

let { email, name, size = "sm", url }: Props = $props()

let failed = $state(false)

const initials = $derived.by(() => {
	const source = name?.trim() || email?.split("@")[0]?.trim() || ""
	const parts = source.split(/[\s._-]+/).filter(Boolean)
	if (parts.length === 0) return "?"

	const letters = parts.length > 1 ? parts[0].charAt(0) + parts[1].charAt(0) : source.slice(0, 2)

	return letters.toUpperCase()
})

// A broken picture falls back to the initials; a new URL gets another chance.
$effect((): void => {
	void url
	failed = false
})
</script>

{#if url && !failed}
	<img
		alt=""
		class="avatar {size}"
		decoding="async"
		loading="lazy"
		onerror={(): void => {
			failed = true
		}}
		referrerpolicy="no-referrer"
		src={url}
	>
{:else}
	<span aria-hidden="true" class="avatar {size} placeholder">{initials}</span>
{/if}

<style>
.avatar {
	display: block;
	flex: none;
	width: 1.625rem;
	height: 1.625rem;
	overflow: hidden;
	font-size: 0.625rem;
	font-weight: 800;
	color: var(--text-2);
	object-fit: cover;
	background: var(--surface-2);
	border: 0.0625rem solid var(--border);
	border-radius: 0.4375rem;
}

.avatar.placeholder {
	display: grid;
	place-items: center;
	line-height: 1;
}

.avatar.xs {
	width: 1.25rem;
	height: 1.25rem;
	font-size: 0.53125rem;
	border-radius: 0.3125rem;
}

.avatar.sm {
	width: 1.625rem;
	height: 1.625rem;
	font-size: 0.625rem;
	border-radius: 0.4375rem;
}

.avatar.md {
	width: 1.875rem;
	height: 1.875rem;
	font-size: 0.6875rem;
	border-radius: 0.5rem;
}

.avatar.lg {
	width: 3.625rem;
	height: 3.625rem;
	font-size: 1.125rem;
	border-radius: 0.75rem;
}
</style>
