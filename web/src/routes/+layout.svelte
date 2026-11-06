<script lang="ts">
import { onMount } from "svelte"
import { goto } from "$app/navigation"
import { page } from "$app/state"
import { fetchMe, logout } from "../lib/api"
import { ApiError } from "../lib/api-error"
import Avatar from "../lib/components/Avatar.svelte"
import SearchBar from "../lib/components/SearchBar.svelte"
import { hasPermission, permissionAuditRead, permissionRoleManage, permissionRosterManage } from "../lib/permissions"
import { session } from "../lib/session.svelte.js"
import { theme } from "../lib/theme.svelte.js"
import type { LayoutProps } from "./$types"
import "../app.css"

let { children }: LayoutProps = $props()

let ready = $state(false)
let signingOut = $state(false)

const readOnly = $derived(session.me?.readOnly ?? false)
const showAdmin = $derived(
	session.me?.programs.some(
		item =>
			hasPermission(item.permissions, permissionRosterManage)
			|| hasPermission(item.permissions, permissionRoleManage),
	) ?? false,
)
const showAudit = $derived(
	session.me?.programs.some(item => hasPermission(item.permissions, permissionAuditRead)) ?? false,
)
const accountEmail = $derived(session.me?.user.email ?? "")
const accountName = $derived(session.me?.user.name ?? accountEmail)
const accountProfileHref = $derived(session.me ? `/reviewers/${encodeURIComponent(session.me.user.id)}` : "/")

interface NavItem {
	active: boolean
	href: string
	label: string
}

// isQueueRoute keeps the review screen under the queue tab without catching /reviewed.
function isQueueRoute(path: string): boolean {
	return path.startsWith("/queue") || path === "/review" || path.startsWith("/review/")
}

function isReviewedRoute(path: string): boolean {
	return path === "/reviewed" || path.startsWith("/reviewed/")
}

const navItems = $derived.by((): NavItem[] => {
	const path = page.url.pathname
	const sections: NavItem[] = [
		{ active: path === "/overview", href: "/overview", label: "Overview" },
		{ active: isQueueRoute(path), href: "/queue", label: "Queue" },
		{ active: isReviewedRoute(path), href: "/reviewed", label: "Reviewed" },
		{ active: path.startsWith("/deliveries"), href: "/deliveries", label: "Deliveries" },
	]

	if (showAdmin) sections.push({ active: path.startsWith("/admin"), href: "/admin", label: "Admin" })
	if (showAudit) sections.push({ active: path.startsWith("/audit"), href: "/audit", label: "Audit" })

	sections.push({ active: path.startsWith("/docs"), href: "/docs", label: "Docs" })

	return sections
})

// isTyping reports whether a key event belongs to an editable field.
function isTyping(target: EventTarget | null): boolean {
	return (
		target instanceof HTMLInputElement
		|| target instanceof HTMLTextAreaElement
		|| target instanceof HTMLSelectElement
		|| (target instanceof HTMLElement && target.isContentEditable)
	)
}

// onKeydown moves between the main sections with [ and ], or Alt+1-6 for a direct jump.
function onKeydown(event: KeyboardEvent): void {
	if (!session.me) return

	if (event.altKey && !event.ctrlKey && !event.metaKey) {
		const match = /^(?:Digit|Numpad)([1-7])$/.exec(event.code)
		if (!match) return

		const destination = navItems[Number(match[1]) - 1]?.href
		if (!destination) return

		event.preventDefault()
		void goto(destination)

		return
	}

	if (isTyping(event.target)) return
	if (event.key !== "[" && event.key !== "]") return
	if (event.ctrlKey || event.metaKey || event.altKey) return

	const current = navItems.findIndex(item => item.active)
	const step = event.key === "]" ? 1 : -1
	const next =
		current === -1 ? (step === 1 ? 0 : navItems.length - 1) : (current + step + navItems.length) % navItems.length
	const destination = navItems[next]?.href
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
	<meta content="Ire queue for Half Life hardware ships" name="description">
</svelte:head>

<svelte:window onkeydown={onKeydown} />

<a class="skip-link" href="#main-content">Skip to content</a>

{#snippet sectionLinks()}
	{#each navItems as item, index (item.href)}
		<a
			aria-current={item.active ? "page" : undefined}
			aria-keyshortcuts={`Alt+${index + 1}`}
			href={item.href}
			class:active={item.active}
			>{item.label}</a
		>
	{/each}
{/snippet}

<div class="shell">
	<header>
		<div class="top">
			<a class="brand" href={session.me ? "/overview" : "/"}>
				<span class="mark">HL</span>
				<span class="brand-copy">
					<strong>Ire</strong>
					<small>Half Life hardware</small>
				</span>
			</a>

			{#if session.me}
				<nav aria-label="Sections" class="sections-inline">
					{@render sectionLinks()}
				</nav>
			{/if}

			<div class="spacer"></div>

			{#if session.me}
				<SearchBar />
				<a
					aria-label={`Your profile, ${accountName}`}
					class="account"
					data-sveltekit-reload
					href={accountProfileHref}
					title="Your profile"
				>
					<Avatar
						email={accountEmail}
						name={session.me.user.name}
						size="sm"
						url={session.me.user.avatarUrl}
					/>
					<span class="account-name">{accountName}</span>
				</a>
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
					Ire is read-only. Ships and decisions stay in Ari.
				</div>
			{/if}

			<nav aria-label="Sections" class="sections-row">
				{@render sectionLinks()}
			</nav>
		{/if}
	</header>

	<main id="main-content" tabindex="-1">
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

.skip-link {
	position: fixed;
	top: var(--space-2);
	left: var(--space-2);
	z-index: var(--layer-toast);
	padding: var(--space-2) var(--space-3);
	font-size: var(--text-sm);
	font-weight: 700;
	color: var(--text);
	text-decoration: none;
	background: var(--surface);
	border: 1px solid var(--border-2);
	border-radius: var(--radius-md);
	box-shadow: var(--shadow);
	transform: translateY(calc(-100% - var(--space-4)));
	transition: transform 0.15s;
}

.skip-link:focus-visible {
	transform: translateY(0);
}

header {
	position: sticky;
	top: 0;
	z-index: var(--layer-header);
	background: var(--surface);
}

.top,
.sections-row {
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
	width: 2rem;
	height: 2rem;
	font-size: 0.75rem;
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
	color: inherit;
	text-decoration: none;
	border-radius: var(--radius-sm);
}

.account-name {
	overflow: hidden;
	text-overflow: ellipsis;
	font-size: var(--text-sm);
	color: var(--text-2);
	white-space: nowrap;
}

.account:hover .account-name {
	color: var(--text);
}

.sections-row {
	display: flex;
	gap: var(--space-4);
	overflow-x: auto;
}

/* The inline copy takes the empty space beside the brand only on very wide screens. */
.sections-inline {
	display: none;
	gap: var(--space-4);
	align-items: center;
	align-self: flex-end;
	min-width: 0;
	padding-left: var(--space-5);
	margin-bottom: calc(-1 * var(--space-2));
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

@media (max-width: 40rem) {
	.top,
	.sections-row {
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

/* Wide screens have room to lift the section links into the header row. */
@media (min-width: 100rem) {
	.sections-inline {
		display: flex;
	}

	.sections-row {
		display: none;
	}
}
</style>
