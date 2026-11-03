<script lang="ts">
import { goto } from "$app/navigation"
import { onMount } from "svelte"
import { login } from "../lib/api"
import { ApiError } from "../lib/api-error"
import { session } from "../lib/session.svelte.js"

let email = $state("reviewer@example.com")
let busy = $state(false)
let error = $state("")

const accounts = [
	{ email: "reviewer@example.com", label: "Rae", note: "Reviewer" },
	{ email: "organiser@example.com", label: "Oli", note: "Organiser" },
	{ email: "admin@example.com", label: "Ada", note: "Admin" },
]

onMount((): void => {
	if (session.me) void goto("/queue")
})

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
			<p>Stage 1 queue for hardware ships. Development sign-in.</p>
		</div>

		<form onsubmit={submit}>
			<label class="field">
				<span>Email</span>
				<input autocomplete="email" bind:value={email} class="ui-input" required type="email">
			</label>
			<button class="ui-button primary block" disabled={busy} type="submit">
				{busy ? "Signing in…" : "Sign in"}
			</button>
		</form>

		{#if error}
			<p class="error" role="alert">{error}</p>
		{/if}

		<p class="divider">Or a seeded account</p>

		<div class="accounts">
			{#each accounts as account (account.email)}
				<button class="account" disabled={busy} onclick={(): Promise<void> => signIn(account.email)} type="button">
					<span class="account-name">{account.label}</span>
					<span class="account-note">{account.note}</span>
				</button>
			{/each}
		</div>
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
	align-items: center;
	gap: var(--space-2);
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
	align-items: baseline;
	justify-content: space-between;
	gap: var(--space-2);
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
