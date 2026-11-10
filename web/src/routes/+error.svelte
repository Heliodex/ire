<script lang="ts">
import { page } from "$app/state"

const missing = $derived(page.status === 404)
const title = $derived(missing ? "Page not found" : "Something went wrong")
const detail = $derived(
	missing
		? "The link may be old, or the ship may have been withdrawn."
		: (page.error?.message ?? "The review service could not load this page."),
)
</script>

<svelte:head>
	<title>{title} · Ire</title>
</svelte:head>

<section class="error-page">
	<div class="ui-card padded panel">
		<span class="code">{page.status}</span>
		<h1>{title}</h1>
		<p>{detail}</p>
		<div class="actions">
			<a class="ui-button primary" href="/stations">Back to stations</a>
			<a class="ui-button ghost" href="/docs">Integration docs</a>
		</div>
	</div>
</section>

<style>
.error-page {
	display: flex;
	flex: 1;
	align-items: center;
	justify-content: center;
	padding: var(--space-6) 0 var(--space-7);
}

.panel {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	width: 100%;
	max-width: 28.75rem;
}

.code {
	font-family: var(--font-mono);
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-3);
	letter-spacing: 0.08em;
}

h1 {
	font-size: var(--text-xl);
}

p {
	font-size: var(--text-sm);
	color: var(--text-2);
}

.actions {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	padding-top: var(--space-3);
}
</style>
