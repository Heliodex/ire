<script lang="ts">
import { onMount } from "svelte"
import { goto } from "$app/navigation"
import {
	type AuditEntry,
	fetchAudit,
	fetchMe,
	fetchMembers,
	inviteMember,
	type Member,
	program,
	updateMember,
} from "../../lib/api"
import { ApiError } from "../../lib/api-error"
import MemberRole from "../../lib/components/admin/MemberRole.svelte"
import { formatDateTime } from "../../lib/format"
import { session } from "../../lib/session.svelte.js"

const roles = ["reviewer", "organizer", "admin", "auditor"] as const
const roleLabels: { [_: string]: string } = {
	admin: "Admin",
	auditor: "Auditor",
	organizer: "Organiser",
	reviewer: "Reviewer",
}

let members = $state<Member[]>([])
let audit = $state<AuditEntry[]>([])
let loading = $state(true)
let error = $state("")
let notice = $state("")
let saving = $state("")
let email = $state("")
let inviteRole = $state<string>("reviewer")

const role = $derived(session.me?.programs.find(item => item.programSlug === program)?.role ?? "")
const isAdmin = $derived(role === "admin")

onMount((): void => {
	void boot()
})

async function boot(): Promise<void> {
	try {
		session.me ??= await fetchMe()
	} catch {
		void goto("/")
		return
	}

	if (role !== "admin") {
		loading = false
		return
	}

	await refresh()
}

// refresh reloads the roster and the most recent audit entries together.
async function refresh(): Promise<void> {
	try {
		const [roster, entries] = await Promise.all([fetchMembers(), fetchAudit()])
		members = roster
		audit = entries.entries
		error = ""
	} catch (err) {
		if (err instanceof ApiError && err.code === "unauthorized") {
			void goto("/")
			return
		}
		error = err instanceof Error ? err.message : "Could not load the roster."
	} finally {
		loading = false
	}
}

async function save(member: Member, nextRole: string, active: boolean): Promise<void> {
	saving = member.id
	error = ""
	notice = ""

	try {
		const updated = await updateMember(member.id, nextRole, active)
		members = members.map(item => (item.id === updated.id ? updated : item))
		notice = `Saved ${updated.email}.`
		const entries = await fetchAudit()
		audit = entries.entries
	} catch (err) {
		error = err instanceof Error ? err.message : "Could not save this member."
	} finally {
		saving = ""
	}
}

async function invite(): Promise<void> {
	const address = email.trim()
	if (address === "") return

	saving = "invite"
	error = ""
	notice = ""

	try {
		const member = await inviteMember(address, inviteRole)
		members = [...members.filter(item => item.id !== member.id), member]
		email = ""
		notice = `Invited ${member.email}.`
		const entries = await fetchAudit()
		audit = entries.entries
	} catch (err) {
		error = err instanceof Error ? err.message : "Could not invite this email."
	} finally {
		saving = ""
	}
}
</script>

<svelte:head>
	<title>Admin · Half Life Review</title>
</svelte:head>

<section class="page-head">
	<div>
		<h1>Admin</h1>
		<p>Roster, roles and recent actions</p>
	</div>
</section>

{#if error}
	<div class="ui-notice" data-tone="bad" role="alert">{error}</div>
{/if}

{#if notice}
	<div class="ui-notice" data-tone="good" role="status">{notice}</div>
{/if}

{#if !loading && !isAdmin}
	<div class="ui-card ui-empty">
		<strong>Admins only</strong>
		<span>Your account can review ships, but the roster and audit are limited to admins.</span>
		<a class="ui-button" data-variant="outline" href="/queue">Back to the queue</a>
	</div>
{:else if loading}
	<div class="skeleton ui-card">
		<div></div>
		<div></div>
		<div></div>
	</div>
{:else}
	<section aria-labelledby="roster-title" class="ui-card padded">
		<div class="section-head">
			<h2 id="roster-title">Roster</h2>
			<span>{members.length} {members.length === 1 ? "account" : "accounts"}</span>
		</div>

		<h3 class="invite-title">Invite a reviewer</h3>

		<form
			class="invite"
			onsubmit={(event): void => {
				event.preventDefault()
				void invite()
			}}
		>
			<label class="field">
				<span>Email</span>
				<input class="ui-input" placeholder="maker@example.com" required type="email" bind:value={email}>
			</label>
			<label class="field role-field">
				<span>Role</span>
				<select class="ui-input" bind:value={inviteRole}>
					{#each roles as option (option)}
						<option value={option}>{roleLabels[option]}</option>
					{/each}
				</select>
			</label>
			<button class="ui-button primary" disabled={saving === "invite" || email.trim() === ""} type="submit">
				{saving === "invite" ? "Inviting…" : "Invite"}
			</button>
		</form>

		<div class="ui-table roster">
			<table>
				<thead>
					<tr>
						<th scope="col">Account</th>
						<th scope="col">Role</th>
						<th scope="col">Active</th>
						<th scope="col">Last sign-in</th>
					</tr>
				</thead>
				<tbody>
					{#each members as member (member.id)}
						<tr class:inactive={!member.active}>
							<td>
								<strong>{member.name ?? member.email}</strong>
								<span class="muted">{member.email}</span>
								{#if saving === member.id}
									<span class="saving">Saving…</span>
								{/if}
							</td>
							<MemberRole
								busy={saving === member.id}
								{member}
								onSave={(nextRole, active): Promise<void> => save(member, nextRole, active)}
							/>
							<td>
								<input
									aria-label={`Active for ${member.email}`}
									checked={member.active}
									class="ui-switch"
									onchange={(event): void => {
										const input = event.currentTarget as HTMLInputElement
										void save(member, member.role, input.checked)
									}}
									type="checkbox"
								>
							</td>
							<td class="muted">{member.lastLoginAt ? formatDateTime(member.lastLoginAt) : "Never"}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	</section>

	<section aria-labelledby="audit-title" class="ui-card padded audit">
		<div class="section-head">
			<h2 id="audit-title">Recent actions</h2>
			<button class="ui-button quiet sm" onclick={refresh} type="button">Refresh</button>
		</div>

		{#if audit.length === 0}
			<p class="muted">No admin actions yet.</p>
		{:else}
			<div class="ui-table">
				<table>
					<thead>
						<tr>
							<th scope="col">When</th>
							<th scope="col">Action</th>
							<th scope="col">Actor</th>
							<th scope="col">Target</th>
						</tr>
					</thead>
					<tbody>
						{#each audit as entry (entry.id)}
							<tr>
								<td class="muted">{formatDateTime(entry.createdAt)}</td>
								<td><code>{entry.action}</code></td>
								<td>{entry.actorEmail ?? "system"}</td>
								<td class="muted">
									{#if entry.targetType}
										{entry.targetType}
									{/if}
									{#if entry.targetId}
										: {entry.targetId}
									{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>
{/if}

<style>
.page-head {
	display: flex;
	gap: var(--space-3);
	align-items: flex-end;
	justify-content: space-between;
	margin-bottom: var(--space-4);
}

.page-head h1 {
	font-size: var(--text-2xl);
}

.page-head p,
.muted {
	color: var(--text-2);
}

.section-head {
	display: flex;
	gap: var(--space-3);
	align-items: baseline;
	justify-content: space-between;
	margin-bottom: var(--space-3);
}

.invite-title {
	margin-bottom: var(--space-2);
	font-size: var(--text-sm);
	font-weight: 700;
}

.invite {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-3);
	align-items: flex-end;
	margin-bottom: var(--space-4);
}

.field {
	display: flex;
	flex: 1;
	flex-direction: column;
	gap: var(--space-1);
	min-width: 180px;
}

.field span {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-2);
}

.role-field {
	flex: 0 0 160px;
}

.roster {
	padding-top: var(--space-2);
	border-top: 1px solid var(--border);
}

.roster td strong {
	display: block;
}

.roster td .muted {
	font-size: var(--text-xs);
}

.saving {
	display: block;
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--primary);
}

tr.inactive {
	opacity: 0.6;
}

.audit {
	margin-top: var(--space-4);
}

code {
	font-family: var(--font-mono);
	font-size: var(--text-xs);
}
</style>
