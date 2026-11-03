<script lang="ts">
import { goto } from "$app/navigation"
import { onMount } from "svelte"
import { ApiError } from "../lib/api-error"
import { login } from "../lib/api"
import { session } from "../lib/session.svelte.js"

let email = $state("reviewer@example.com")
let busy = $state(false)
let error = $state("")

const accounts = [
	{ email: "reviewer@example.com", label: "Rae · reviewer", note: "Stage 1 queue" },
	{ email: "organiser@example.com", label: "Oli · organiser", note: "Second-pass policy" },
	{ email: "admin@example.com", label: "Ada · admin", note: "Full programme" },
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
	<div class="intro">
		<p class="eyebrow">Half Life hardware review</p>
		<h1>Read the board.<br><em>Make the call.</em></h1>
		<p class="lede">
			A familiar queue for the reviewers who came from Ari: claim a ship, read the checks and the evidence, decide
			once, and move to the next board. Stage 2 and fraud stay out of the first pass.
		</p>

		<div class="points">
			<div>
				<span>01</span>
				<p>Fixture ships stand in for the live queue until checks are ported.</p>
			</div>
			<div>
				<span>02</span>
				<p>Warnings need one acknowledgement. A blocking check disables approve.</p>
			</div>
			<div>
				<span>03</span>
				<p>Every decision writes a signed outbox row before it is delivered.</p>
			</div>
		</div>
	</div>

	<div class="panel">
		<div class="panel-head">
			<h2>Sign in</h2>
			<span class="badge">Development only</span>
		</div>
		<p class="hint">
			This prototype uses seeded accounts instead of Hack Club Auth, so the queue can be reviewed before the OAuth
			app exists.
		</p>

		<form onsubmit={submit}>
			<label>
				<span>Email</span>
				<input autocomplete="email" bind:value={email} required type="email">
			</label>
			<button class="primary" disabled={busy} type="submit">
				{busy ? "Signing in…" : "Enter the queue"}
			</button>
		</form>

		{#if error}
			<p class="error" role="alert">{error}</p>
		{/if}

		<div class="accounts">
			<span>Or pick a seeded account</span>
			{#each accounts as account (account.email)}
				<button class="account" disabled={busy} onclick={(): Promise<void> => signIn(account.email)} type="button">
					<strong>{account.label}</strong>
					<small>{account.note}</small>
				</button>
			{/each}
		</div>
	</div>
</section>

<style>
.login {
	display: grid;
	grid-template-columns: minmax(0, 1.15fr) minmax(320px, 0.85fr);
	gap: 48px;
	align-items: start;
	padding: 44px 0 20px;
}

.eyebrow {
	padding-bottom: 16px;
	font-size: 0.68rem;
	font-weight: 750;
	color: var(--accent);
	text-transform: uppercase;
	letter-spacing: 0.13em;
}

h1 {
	max-width: 640px;
	font-size: clamp(2.4rem, 5.4vw, 4.1rem);
}

h1 em {
	font-style: normal;
	color: var(--accent);
}

.lede {
	max-width: 590px;
	padding-top: 18px;
	font-size: 1rem;
	color: var(--muted);
}

.points {
	display: grid;
	gap: 14px;
	max-width: 560px;
	padding-top: 38px;
}

.points div {
	display: grid;
	grid-template-columns: 38px 1fr;
	gap: 14px;
	align-items: start;
	padding-top: 12px;
	border-top: 1px solid var(--line);
}

.points span {
	font-size: 0.7rem;
	font-weight: 800;
	color: var(--accent);
	letter-spacing: 0.08em;
}

.points p {
	font-size: 0.84rem;
	color: var(--muted);
}

.panel {
	padding: 24px;
	background: var(--card);
	border: 1px solid var(--line);
	border-radius: var(--radius);
	box-shadow: var(--shadow);
}

.panel-head {
	display: flex;
	flex-wrap: wrap;
	gap: 12px;
	align-items: baseline;
	justify-content: space-between;
	padding-bottom: 10px;
}

.panel-head h2 {
	font-size: 1.3rem;
}

.badge {
	padding: 4px 9px;
	font-size: 0.64rem;
	font-weight: 750;
	color: var(--accent-deep);
	text-transform: uppercase;
	letter-spacing: 0.08em;
	background: var(--accent-soft);
	border-radius: 999px;
}

.hint {
	padding-bottom: 20px;
	font-size: 0.78rem;
	color: var(--muted);
}

form {
	display: flex;
	flex-direction: column;
	gap: 10px;
}

label {
	display: flex;
	flex-direction: column;
	gap: 6px;
}

label span {
	font-size: 0.66rem;
	font-weight: 700;
	color: var(--muted);
	text-transform: uppercase;
	letter-spacing: 0.1em;
}

input {
	width: 100%;
	padding: 10px 12px;
	color: var(--ink);
	background: #fff;
	border: 1px solid var(--line);
	border-radius: 10px;
	outline: none;
}

input:focus {
	border-color: var(--accent);
	box-shadow: 0 0 3px var(--accent-soft);
}

.primary {
	padding: 11px 16px;
	font-size: 0.86rem;
	font-weight: 750;
	color: #fff;
	background: var(--accent);
	border: 0;
	border-radius: 10px;
}

.primary:hover {
	background: var(--accent-deep);
}

.primary:disabled {
	cursor: wait;
	opacity: 0.65;
}

.error {
	padding: 10px 12px;
	margin-top: 12px;
	font-size: 0.82rem;
	color: #7d2a12;
	background: #fbe5df;
	border: 1px solid #e9b9aa;
	border-radius: 10px;
}

.accounts {
	display: flex;
	flex-direction: column;
	gap: 8px;
	padding-top: 20px;
	margin-top: 20px;
	border-top: 1px solid var(--line);
}

.accounts > span {
	padding-bottom: 2px;
	font-size: 0.66rem;
	font-weight: 700;
	color: var(--muted);
	text-transform: uppercase;
	letter-spacing: 0.1em;
}

.account {
	display: flex;
	gap: 10px;
	align-items: baseline;
	justify-content: space-between;
	padding: 9px 11px;
	text-align: left;
	background: var(--paper);
	border: 1px solid var(--line);
	border-radius: 9px;
}

.account:hover {
	border-color: var(--ink);
}

.account strong {
	font-size: 0.8rem;
}

.account small {
	font-size: 0.7rem;
	color: var(--muted);
}

@media (max-width: 900px) {
	.login {
		grid-template-columns: 1fr;
		gap: 30px;
		padding-top: 24px;
	}
}
</style>
