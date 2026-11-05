<script lang="ts">
import { onMount } from "svelte"
import { goto } from "$app/navigation"
import { page } from "$app/state"
import { fetchMe, logout } from "../lib/api"
import { ApiError } from "../lib/api-error"
import SearchBar from "../lib/components/SearchBar.svelte"
import { session } from "../lib/session.svelte.js"
import { theme } from "../lib/theme.svelte.js"
import type { LayoutProps } from "./$types"
import "../app.css"

let { children }: LayoutProps = $props()

let ready = $state(false)
let signingOut = $state(false)

const readOnly = $derived(session.me?.readOnly ?? false)
const showAdmin = $derived(session.me?.programs.some(item => item.role === "admin") ?? false)
const showAudit = $derived(session.me?.programs.some(item => item.role === "admin" || item.role === "auditor") ?? false)
const accountEmail = $derived(session.me?.user.email ?? "")
const accountName = $derived(session.me?.user.name ?? accountEmail)

// initials makes a short avatar label from the display name, falling back to the email local part.
function initials(value: string): string {
	const parts = value
		.split(/[\s@._-]+/)
		.filter(Boolean)
		.slice(0, 2)

	return parts.map(part => part.charAt(0).toUpperCase()).join("") || "?"
}
const navRoutes = $derived([
	"/queue",
	"/reviewed",
	"/deliveries",
	...(showAdmin ? ["/admin"] : []),
	...(showAudit ? ["/audit"] : []),
])

// isQueueRoute keeps the review screen under the queue tab without catching /reviewed.
function isQueueRoute(path: string): boolean {
	return path.startsWith("/queue") || path === "/review" || path.startsWith("/review/")
}

function isReviewedRoute(path: string): boolean {
	return path === "/reviewed" || path.startsWith("/reviewed/")
}

// tabIndex maps the current route onto the navigation tabs; review counts as part of the queue.
function tabIndex(path: string): number {
	if (isQueueRoute(path)) return 0
	if (isReviewedRoute(path)) return 1
	if (path.startsWith("/deliveries")) return 2
	if (showAdmin && path.startsWith("/admin")) return 3
	if (showAudit && path.startsWith("/audit")) return showAdmin ? 4 : 3

	return -1
}

// onKeydown moves between the main sections with [ and ].
function onKeydown(event: KeyboardEvent): void {
	if (!session.me) return
	if (event.key !== "[" && event.key !== "]") return
	if (event.ctrlKey || event.metaKey || event.altKey) return

	const { target } = event
	if (
		target instanceof HTMLInputElement
		|| target instanceof HTMLTextAreaElement
		|| target instanceof HTMLSelectElement
		|| (target instanceof HTMLElement && target.isContentEditable)
	)
		return

	const current = tabIndex(page.url.pathname)
	const step = event.key === "]" ? 1 : -1
	const next =
		current === -1
			? step === 1
				? 0
				: navRoutes.length - 1
			: (current + step + navRoutes.length) % navRoutes.length
	const destination = navRoutes[next]
	if (!destination) return

	event.preventDefault()
	void goto(destination)
}

onMount(async (): Promise<void> => {
	theme.init()

	try {
		session.me = await fetchMe()
	} catch (err) {
		session.me = null
		if (!(err instanceof ApiError)) console.error(err)
	} finally {
		ready = true
	}
})

$effect((): void => {
	if (ready && !session.me && page.url.pathname !== "/" && page.url.pathname !== "/docs") void goto("/")
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

<svelte:window onkeydown={onKeydown} />

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
				<SearchBar />
				<span class="account" title={accountEmail}>
					<span aria-hidden="true" class="avatar">{initials(accountName)}</span>
					<span class="account-name">{accountName}</span>
				</span>
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
						<circle cx="12" cy="12" r="4" stroke="currentColor" stroke-width="2" />
						<path
							d="M12 2v2m0 16v2M2 12h2m16 0h2M4.9 4.9l1.4 1.4m11.4 11.4 1.4 1.4M19.1 4.9l-1.4 1.4M6.3 17.7l-1.4 1.4"
							stroke="currentColor"
							stroke-linecap="round"
							stroke-width="2"
						/>
					</svg>
				{:else}
					<svg aria-hidden="true" fill="none" height="17" viewBox="0 0 24 24" width="17">
						<path
							d="M20 14.5A8 8 0 0 1 9.5 4 8.5 8.5 0 1 0 20 14.5Z"
							stroke="currentColor"
							stroke-linejoin="round"
							stroke-width="2"
						/>
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
			{#if readOnly}
				<div class="shadow-banner" role="status">
					<strong>Shadow mode</strong>
					Review is read-only. Ships and decisions stay in Ari.
				</div>
			{/if}

			<nav aria-label="Sections">
				<a
					aria-current={isQueueRoute(page.url.pathname) ? "page" : undefined}
					href="/queue"
					class:active={isQueueRoute(page.url.pathname)}
					>Queue</a
				>
				<a
					aria-current={isReviewedRoute(page.url.pathname) ? "page" : undefined}
					href="/reviewed"
					class:active={isReviewedRoute(page.url.pathname)}
					>Reviewed</a
				>
				<a
					aria-current={page.url.pathname.startsWith("/deliveries") ? "page" : undefined}
					href="/deliveries"
					class:active={page.url.pathname.startsWith("/deliveries")}
					>Deliveries</a
				>
				{#if showAdmin}
					<a
						aria-current={page.url.pathname.startsWith("/admin") ? "page" : undefined}
						href="/admin"
						class:active={page.url.pathname.startsWith("/admin")}
						>Admin</a
					>
				{/if}
				{#if showAudit}
					<a
						aria-current={page.url.pathname.startsWith("/audit") ? "page" : undefined}
						href="/audit"
						class:active={page.url.pathname.startsWith("/audit")}
						>Audit</a
					>
				{/if}
				<a
					aria-current={page.url.pathname.startsWith("/docs") ? "page" : undefined}
					href="/docs"
					class:active={page.url.pathname.startsWith("/docs")}
					>Docs</a
				>
			</nav>
		{/if}
	</header>

	<main>
		{@render children()}
	</main>
</div>

<style>
.shadow-banner {
	display: flex;
	gap: var(--space-2);
	align-items: center;
	width: 100%;
	padding: var(--space-2) var(--space-4);
	font-size: var(--text-sm);
	color: var(--warning);
	border-top: 1px solid var(--border);
}

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
	padding-inline: var(--space-4);
}

.top {
	display: flex;
	gap: var(--space-3);
	align-items: center;
	padding-block: var(--space-2);
}

.brand {
	display: inline-flex;
	gap: var(--space-2);
	align-items: center;
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

.account {
	display: inline-flex;
	gap: var(--space-2);
	align-items: center;
	min-width: 0;
}

.avatar {
	display: grid;
	flex: none;
	place-items: center;
	width: 26px;
	height: 26px;
	font-size: 10px;
	font-weight: 800;
	color: var(--text-2);
	background: var(--surface-2);
	border: 1px solid var(--border);
	border-radius: 50%;
}

.account-name {
	overflow: hidden;
	text-overflow: ellipsis;
	font-size: var(--text-sm);
	color: var(--text-2);
	white-space: nowrap;
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
	padding: var(--space-5) var(--space-4) var(--space-7);
}

.visually-hidden {
	position: absolute;
	width: 1px;
	height: 1px;
	overflow: hidden;
	white-space: nowrap;
	clip: rect(0 0 0 0);
}

@media (max-width: 640px) {
	.top,
	nav {
		padding-inline: var(--space-3);
	}

	.top {
		flex-wrap: wrap;
	}

	.account-name {
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
