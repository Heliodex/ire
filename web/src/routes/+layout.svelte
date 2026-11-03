<script lang="ts">
import { goto } from "$app/navigation"
import { page } from "$app/state"
import { onMount } from "svelte"
import { fetchMe, logout } from "../lib/api"
import { ApiError } from "../lib/api-error"
import { session } from "../lib/session.svelte.js"
import { theme } from "../lib/theme.svelte.js"
import type { LayoutProps } from "./$types"
import "../app.css"

let { children }: LayoutProps = $props()

let ready = $state(false)
let signingOut = $state(false)

onMount(async (): Promise<void> => {
	theme.init()

	try {
		session.me = await fetchMe()
	} catch (error) {
		session.me = null
		if (!(error instanceof ApiError)) console.error(error)
	} finally {
		ready = true
	}
})

$effect((): void => {
	if (ready && !session.me && page.url.pathname !== "/") void goto("/")
})

async function signOut(): Promise<void> {
	signingOut = true
	try {
		await logout()
	} finally {
		session.me = null
		signingOut = false
		void goto("/")
	}
}
</script>

<svelte:head>
	<link href="/favicon.svg" rel="icon">
	<meta content="Review queue for Half Life hardware ships" name="description">
</svelte:head>

<div class="shell">
	<header>
		<div class="top">
			<a class="brand" href={session.me ? "/queue" : "/"}>
				<span class="mark">HL</span>
				<span class="brand-copy">
					<strong>Review</strong>
					<small>Half Life hardware</small>
				</span>
			</a>

			<div class="spacer"></div>

			{#if session.me}
				<span class="email">{session.me.user.email}</span>
			{/if}

			<button
				class="ui-icon-button theme"
				onclick={(): void => theme.toggle()}
				title={theme.value === "dark" ? "Switch to light theme" : "Switch to dark theme"}
				type="button"
			>
				<span class="visually-hidden">Toggle theme</span>
				{#if theme.value === "dark"}
					<svg aria-hidden="true" fill="none" height="17" viewBox="0 0 24 24" width="17">
						<circle cx="12" cy="12" r="4" stroke="currentColor" stroke-width="2"/>
						<path d="M12 2v2m0 16v2M2 12h2m16 0h2M4.9 4.9l1.4 1.4m11.4 11.4 1.4 1.4M19.1 4.9l-1.4 1.4M6.3 17.7l-1.4 1.4" stroke="currentColor" stroke-linecap="round" stroke-width="2"/>
					</svg>
				{:else}
					<svg aria-hidden="true" fill="none" height="17" viewBox="0 0 24 24" width="17">
						<path d="M20 14.5A8 8 0 0 1 9.5 4 8.5 8.5 0 1 0 20 14.5Z" stroke="currentColor" stroke-linejoin="round" stroke-width="2"/>
					</svg>
				{/if}
			</button>

			{#if session.me}
				<button class="ui-button quiet sm" disabled={signingOut} onclick={signOut} type="button">
					{signingOut ? "Signing out…" : "Sign out"}
				</button>
			{/if}
		</div>

		{#if session.me}
			<nav aria-label="Sections">
				<a class:active={page.url.pathname.startsWith("/queue") || page.url.pathname.startsWith("/review")} href="/queue">
					Queue
				</a>
				<a class:active={page.url.pathname.startsWith("/deliveries")} href="/deliveries">Deliveries</a>
			</nav>
		{/if}
	</header>

	<main>
		{@render children()}
	</main>
</div>

<style>
.shell {
	display: flex;
	flex-direction: column;
	min-height: 100vh;
}

header {
	position: sticky;
	top: 0;
	z-index: var(--layer-header);
	background: var(--surface);
}

.top,
nav {
	width: 100%;
	max-width: 1240px;
	margin: 0 auto;
	padding-inline: var(--space-4);
}

.top {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	padding-block: var(--space-2);
}

.brand {
	display: inline-flex;
	align-items: center;
	gap: var(--space-2);
	color: var(--text);
	text-decoration: none;
}

.mark {
	display: grid;
	place-items: center;
	width: 30px;
	height: 30px;
	font-size: 11px;
	font-weight: 800;
	color: var(--on-primary);
	background: var(--primary);
	border-radius: var(--radius-sm);
}

.brand-copy strong {
	display: block;
	font-size: var(--text-md);
	font-weight: 800;
	line-height: 1.1;
}

.brand-copy small {
	display: block;
	font-size: var(--text-xs);
	color: var(--text-3);
}

.spacer {
	flex: 1;
}

.email {
	font-size: var(--text-sm);
	color: var(--text-2);
}

nav {
	display: flex;
	gap: var(--space-4);
	overflow-x: auto;
}

nav a {
	padding: 0 0 var(--space-2);
	font-size: var(--text-sm);
	font-weight: 600;
	color: var(--text-2);
	text-decoration: none;
	border-bottom: 2px solid transparent;
}

nav a:hover {
	color: var(--text);
}

nav a.active {
	color: var(--text);
	border-bottom-color: var(--primary);
}

main {
	display: flex;
	flex: 1;
	flex-direction: column;
	width: 100%;
	max-width: 1240px;
	margin: 0 auto;
	padding: var(--space-5) var(--space-4) var(--space-7);
}

.visually-hidden {
	position: absolute;
	width: 1px;
	height: 1px;
	overflow: hidden;
	clip: rect(0 0 0 0);
	white-space: nowrap;
}

@media (max-width: 640px) {
	.top,
	nav {
		padding-inline: var(--space-3);
	}

	.email {
		display: none;
	}

	.brand-copy small {
		display: none;
	}

	main {
		padding: var(--space-4) var(--space-3) var(--space-6);
	}
}
</style>
