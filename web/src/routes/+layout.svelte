<script lang="ts">
import { goto } from "$app/navigation"
import { page } from "$app/state"
import { onMount } from "svelte"
import { ApiError } from "../lib/api-error"
import { fetchMe, logout } from "../lib/api"
import { session } from "../lib/session.svelte.js"
import type { LayoutProps } from "./$types"
import "../app.css"

let { children }: LayoutProps = $props()

let ready = $state(false)
let signingOut = $state(false)

onMount(async (): Promise<void> => {
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

<div class="page">
	<header class="site-header">
		<a class="brand" href="/queue">
			<span class="brand-mark">HL</span>
			<span>
				<strong>Review</strong>
				<small>Half Life hardware</small>
			</span>
		</a>

		{#if session.me}
			<nav aria-label="Sections">
				<a class:active={page.url.pathname.startsWith("/queue")} href="/queue">Queue</a>
				<a class:active={page.url.pathname.startsWith("/review")} href="/queue">Review</a>
				<a class:active={page.url.pathname.startsWith("/deliveries")} href="/deliveries">Deliveries</a>
			</nav>
			<div class="who">
				<span>{session.me.user.email}</span>
				<button class="quiet" disabled={signingOut} onclick={signOut} type="button">
					{signingOut ? "Signing out…" : "Sign out"}
				</button>
			</div>
		{:else}
			<span class="proto">Prototype · fixture data</span>
		{/if}
	</header>

	<main>
		{@render children()}
	</main>

	<footer>
		<span>Stage 1 review · Ari-compatible edge</span>
		<span>{session.me ? "Signed in" : "Development sign-in"}</span>
	</footer>
</div>

<style>
.page {
	display: flex;
	flex-direction: column;
	width: min(1240px, calc(100% - 36px));
	min-height: 100vh;
}

.site-header {
	display: flex;
	flex-wrap: wrap;
	gap: 18px;
	align-items: center;
	justify-content: space-between;
	padding: 22px 0 18px;
	border-bottom: 1px solid var(--line);
}

.brand {
	display: flex;
	gap: 11px;
	align-items: center;
	color: var(--ink);
}

.brand:hover {
	text-decoration: none;
}

.brand-mark {
	display: grid;
	place-items: center;
	width: 38px;
	height: 38px;
	font-size: 0.8rem;
	font-weight: 800;
	color: var(--card);
	letter-spacing: 0.02em;
	background: var(--ink);
	border-radius: 11px;
}

.brand strong {
	display: block;
	font-size: 1.12rem;
	font-weight: 800;
	letter-spacing: -0.02em;
}

.brand small {
	display: block;
	font-size: 0.68rem;
	color: var(--muted);
	text-transform: uppercase;
	letter-spacing: 0.1em;
}

nav {
	display: flex;
	gap: 6px;
	padding: 4px;
	background: var(--card);
	border: 1px solid var(--line);
	border-radius: 999px;
}

nav a {
	padding: 7px 14px;
	font-size: 0.78rem;
	font-weight: 700;
	color: var(--muted);
	border-radius: 999px;
}

nav a:hover {
	color: var(--ink);
	text-decoration: none;
}

nav a.active {
	color: var(--card);
	background: var(--ink);
}

.who {
	display: flex;
	gap: 12px;
	align-items: center;
	font-size: 0.76rem;
	color: var(--muted);
}

.who button {
	padding: 6px 10px;
	font-size: 0.74rem;
	font-weight: 700;
	color: var(--ink);
	background: transparent;
	border: 1px solid var(--line);
	border-radius: 8px;
}

.who button:hover {
	border-color: var(--ink);
}

.proto {
	font-size: 0.7rem;
	color: var(--muted);
	text-transform: uppercase;
	letter-spacing: 0.1em;
}

main {
	flex: 1;
	padding: 28px 0 64px;
}

footer {
	display: flex;
	flex-wrap: wrap;
	gap: 10px 20px;
	justify-content: space-between;
	padding: 16px 0 24px;
	font-size: 0.7rem;
	color: var(--muted);
	text-transform: uppercase;
	letter-spacing: 0.09em;
	border-top: 1px solid var(--line);
}

@media (max-width: 760px) {
	.who span {
		display: none;
	}
}
</style>
