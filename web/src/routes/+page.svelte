<script lang="ts">
import { onMount } from "svelte"
import { goto } from "$app/navigation"
import { page } from "$app/state"
import { type AuthConfig, fetchAuthConfig, login } from "../lib/api"
import { ApiError } from "../lib/api-error"
import { session } from "../lib/session.svelte.js"

let email = $state("reviewer@example.com")
let busy = $state(false)
let error = $state("")
let authError = $state("")
let authConfig = $state<AuthConfig | null>(null)

const accounts = [
	{ email: "reviewer@example.com", label: "Rae", note: "Reviewer" },
	{ email: "organiser@example.com", label: "Oli", note: "Organiser" },
	{ email: "admin@example.com", label: "Ada", note: "Admin" },
]

onMount((): void => {
	if (session.me) {
		void goto("/queue")
		return
	}

	const reason = page.url.searchParams.get("error") ?? ""
	if (reason !== "") authError = authMessage(reason)

	void loadAuthConfig()
})

// loadAuthConfig falls back to development sign-in when method discovery fails.
async function loadAuthConfig(): Promise<void> {
	try {
		authConfig = await fetchAuthConfig()
	} catch {
		authConfig = { dev: true, hca: false }
	}
}

// authMessage turns the short callback error into a sentence.
function authMessage(reason: string): string {
	switch (reason) {
		case "state":
			return "The sign-in attempt expired. Try again."
		case "code":
			return "Hack Club did not return a sign-in code."
		case "exchange":
			return "Could not complete sign-in with Hack Club."
		case "identity":
			return "Hack Club did not return an email address."
		case "identity_conflict":
			return "That Hack Club account is already linked to a different reviewer."
		case "not_member":
			return "That Hack Club account is not on the Half Life roster."
		default:
			return "Sign-in failed. Try again."
	}
}

async function submit(event: SubmitEvent): Promise<void> {
	event.preventDefault()
	await signIn(email)
}

async function signIn(address: string): Promise<void> {
	busy = true
	error = ""
	try {
		session.me = await login(address)
		void goto("/queue")
	} catch (err) {
		error = err instanceof ApiError || err instanceof Error ? err.message : "Could not sign in."
	} finally {
		busy = false
	}
}
</script>

<svelte:head>
	<title>Sign in · Half Life Review</title>
</svelte:head>

<section class="login">
	<div class="panel ui-card padded">
		<div class="intro">
			<span class="mark">HL</span>
			<h1>Half Life Review</h1>
			<p>Stage 1 queue for hardware ships.</p>
		</div>

		{#if authConfig?.hca}
			<a class="ui-button primary block" data-sveltekit-reload href="/auth/login">Continue with Hack Club</a>
		{/if}

		{#if authConfig?.dev !== false}
			{#if authConfig?.hca}
				<p class="divider">Or use a seeded account</p>
			{/if}

			<form onsubmit={submit}>
				<label class="field">
					<span>Email</span>
					<input autocomplete="email" class="ui-input" required type="email" bind:value={email}>
				</label>
				<button class="ui-button primary block" disabled={busy} type="submit">
					{busy ? "Signing in…" : "Sign in"}
				</button>
			</form>

			<div class="accounts">
				{#each accounts as account (account.email)}
					<button
						class="account"
						disabled={busy}
						onclick={(): Promise<void> => signIn(account.email)}
						type="button"
					>
						<span class="account-name">{account.label}</span>
						<span class="account-note">{account.note}</span>
					</button>
				{/each}
			</div>
		{:else if !authConfig?.hca}
			<p class="hint">Sign-in is not configured on this deployment.</p>
		{/if}

		{#if authError || error}
			<p class="error" role="alert">{authError || error}</p>
		{/if}
	</div>
</section>

<style>
.login {
	display: flex;
	flex: 1;
	align-items: center;
	justify-content: center;
	padding-bottom: var(--space-7);
}

.panel {
	display: flex;
	flex-direction: column;
	gap: var(--space-4);
	width: 100%;
	max-width: 360px;
}

.intro {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.mark {
	display: grid;
	place-items: center;
	width: 34px;
	height: 34px;
	margin-bottom: var(--space-1);
	font-size: 12px;
	font-weight: 800;
	color: var(--on-primary);
	background: var(--primary);
	border-radius: var(--radius-md);
}

h1 {
	font-size: var(--text-2xl);
}

.intro p {
	font-size: var(--text-sm);
	color: var(--text-2);
}

form {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
}

.field {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.field span {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-2);
}

.error {
	padding: var(--space-2) var(--space-3);
	font-size: var(--text-sm);
	color: var(--primary);
	background: var(--primary-soft);
	border-radius: var(--radius-sm);
}

.divider {
	display: flex;
	gap: var(--space-2);
	align-items: center;
	font-size: var(--text-xs);
	color: var(--text-3);
}

.divider::before,
.divider::after {
	flex: 1;
	height: 1px;
	content: "";
	background: var(--border);
}

.accounts {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.account {
	display: flex;
	gap: var(--space-2);
	align-items: baseline;
	justify-content: space-between;
	padding: var(--space-2) var(--space-3);
	text-align: left;
	background: transparent;
	border: 1px solid var(--border);
	border-radius: var(--radius-sm);
}

.account:hover {
	background: var(--surface-2);
}

.account:disabled {
	opacity: 0.5;
}

.account-name {
	font-size: var(--text-sm);
	font-weight: 700;
}

.account-note {
	font-size: var(--text-xs);
	color: var(--text-2);
}
</style>
