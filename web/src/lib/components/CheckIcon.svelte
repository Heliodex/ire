<script lang="ts">
interface Props {
	status: string
}

let { status }: Props = $props()
</script>

<svg aria-hidden="true" class="check-icon" fill="none" viewBox="0 0 12 12">
	{#if status === "pass"}
		<path
			d="M2.4 6.2 4.8 8.5 9.6 3.5"
			stroke="currentColor"
			stroke-linecap="round"
			stroke-linejoin="round"
			stroke-width="2"
		/>
	{:else if status === "warn"}
		<path d="M6 2.4v4.2" stroke="currentColor" stroke-linecap="round" stroke-width="2" />
		<circle cx="6" cy="9.4" fill="currentColor" r="1" />
	{:else if status === "fail"}
		<path d="M3 6h6" stroke="currentColor" stroke-linecap="round" stroke-width="2" />
	{:else if status === "error"}
		<path d="m3.2 3.2 5.6 5.6M8.8 3.2 3.2 8.8" stroke="currentColor" stroke-linecap="round" stroke-width="1.8" />
	{:else}
		<circle cx="6" cy="6" fill="currentColor" r="1.3" />
	{/if}
</svg>

<style>
.check-icon {
	display: block;
	width: 8px;
	height: 8px;
}
</style>
