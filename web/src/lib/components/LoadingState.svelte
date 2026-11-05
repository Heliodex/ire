<script lang="ts">
interface Props {
	message?: string
}

let { message = "Loading…" }: Props = $props()
</script>

<div class="loading-state" role="status">
	<span aria-hidden="true" class="spinner"></span>
	<span>{message}</span>
</div>

<style>
.loading-state {
	display: flex;
	gap: var(--space-3);
	align-items: center;
	justify-content: center;
	padding: var(--space-6) var(--space-4);
	font-size: var(--text-sm);
	color: var(--text-2);
}

.spinner {
	flex: none;
	width: 1.125rem;
	height: 1.125rem;
	border: 0.125rem solid var(--border-2);
	border-top-color: var(--primary);
	border-radius: 50%;
	animation: spin 0.8s linear infinite;
}

@keyframes spin {
	to {
		transform: rotate(360deg);
	}
}
</style>
