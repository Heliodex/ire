<script lang="ts">
import { onMount } from "svelte"
import { goto } from "$app/navigation"
import {
	type AuditEntry,
	createRole,
	deleteRole,
	fetchAudit,
	fetchMe,
	fetchMembers,
	fetchRoles,
	inviteMember,
	type Member,
	program,
	type RoleDefinition,
	updateMember,
	updateRole,
} from "../../lib/api"
import { ApiError } from "../../lib/api-error"
import Avatar from "../../lib/components/Avatar.svelte"
import MemberRole from "../../lib/components/admin/MemberRole.svelte"
import LoadingState from "../../lib/components/LoadingState.svelte"
import { formatDateTime } from "../../lib/format"
import {
	hasPermission,
	permissionCatalog,
	permissionRoleManage,
	permissionRosterManage,
	programPermissions,
} from "../../lib/permissions"
import { reviewerHref } from "../../lib/reviewer"
import { session } from "../../lib/session.svelte.js"

const builtinRoleLabels: { [_: string]: string } = {
	admin: "Admin",
	auditor: "Auditor",
	organizer: "Organiser",
	reviewer: "Reviewer",
}

// roleLabel names a role for the roster selects, falling back to the raw name for custom roles.
function roleLabel(name: string): string {
	return builtinRoleLabels[name] ?? name
}

let members = $state<Member[]>([])
let audit = $state<AuditEntry[]>([])
let roleDefs = $state<RoleDefinition[]>([])
let loading = $state(true)
let error = $state("")
let notice = $state("")
let saving = $state("")
let savingRole = $state("")
let email = $state("")
let inviteRole = $state<string>("reviewer")
let newRoleName = $state("")

const permissions = $derived(programPermissions(session.me, program))
const canRoster = $derived(hasPermission(permissions, permissionRosterManage))
const canRoles = $derived(hasPermission(permissions, permissionRoleManage))
const roleNames = $derived(roleDefs.map(def => def.name))

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

	if (!canRoster && !canRoles) {
		loading = false
		return
	}

	await refresh()
}

// refresh reloads everything the member is allowed to see: the roster, the
// role definitions and the most recent audit entries.
async function refresh(): Promise<void> {
	try {
		const [roster, defs, entries] = await Promise.all([
			canRoster ? fetchMembers() : Promise.resolve([] as Member[]),
			canRoles ? fetchRoles() : Promise.resolve([] as RoleDefinition[]),
			fetchAudit().then(
				response => response.entries,
				() => [] as AuditEntry[],
			),
		])
		members = roster
		roleDefs = defs
		if (!roleDefs.some(def => def.name === inviteRole)) inviteRole = roleDefs[0]?.name ?? "reviewer"
		audit = entries
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
		audit = await fetchAudit().then(
			response => response.entries,
			() => audit,
		)
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
		audit = await fetchAudit().then(
			response => response.entries,
			() => audit,
		)
	} catch (err) {
		error = err instanceof Error ? err.message : "Could not invite this email."
	} finally {
		saving = ""
	}
}

// roleHas reports whether a role definition grants the permission, with the locked admin role allowing everything.
function roleHas(def: RoleDefinition, permission: string): boolean {
	return def.locked || def.permissions.includes(permission)
}

async function saveRolePermissions(def: RoleDefinition, permission: string, granted: boolean): Promise<void> {
	const next = granted ? [...def.permissions, permission] : def.permissions.filter(item => item !== permission)
	savingRole = def.name
	error = ""
	notice = ""

	try {
		const updated = await updateRole(def.name, { permissions: next })
		roleDefs = roleDefs.map(item => (item.name === updated.name ? updated : item))
		notice = `Saved the ${updated.name} role.`
	} catch (err) {
		error = err instanceof Error ? err.message : "Could not save this role."
	} finally {
		savingRole = ""
	}
}

async function addRole(): Promise<void> {
	const name = newRoleName.trim().toLowerCase()
	if (name === "") return

	savingRole = "new"
	error = ""
	notice = ""

	try {
		const created = await createRole({ name, permissions: [] })
		roleDefs = [...roleDefs, created]
		newRoleName = ""
		notice = `Created the ${created.name} role. Tick its permissions below.`
	} catch (err) {
		error = err instanceof Error ? err.message : "Could not create this role."
	} finally {
		savingRole = ""
	}
}

async function removeRole(def: RoleDefinition): Promise<void> {
	savingRole = def.name
	error = ""
	notice = ""

	try {
		await deleteRole(def.name)
		roleDefs = roleDefs.filter(item => item.name !== def.name)
		notice = `Deleted the ${def.name} role.`
	} catch (err) {
		error = err instanceof Error ? err.message : "Could not delete this role."
	} finally {
		savingRole = ""
	}
}
</script>

<svelte:head>
	<title>Admin · Ire</title>
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

{#if !loading && !canRoster && !canRoles}
	<div class="ui-card ui-empty">
		<strong>Admins only</strong>
		<span>Your account can review ships, but the roster and roles are limited to admins.</span>
		<a class="ui-button" data-variant="outline" href="/queue">Back to the queue</a>
	</div>
{:else if loading}
	<section aria-labelledby="roster-title" class="ui-card padded">
		<div class="section-head">
			<h2 id="roster-title">Roster</h2>
		</div>
		<LoadingState message="Loading roster…" />
	</section>
{:else}
	{#if canRoster}
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
						{#each roleNames as option (option)}
							<option value={option}>{roleLabel(option)}</option>
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
									<a class="member-link" data-sveltekit-reload href={reviewerHref(member.id) ?? "#"}>
										<Avatar
											email={member.email}
											name={member.name}
											size="md"
											url={member.avatarUrl}
										/>
										<span class="member-copy">
											<strong>{member.name ?? member.email}</strong>
											<span class="muted">{member.email}</span>
										</span>
									</a>
									{#if saving === member.id}
										<span class="saving">Saving…</span>
									{/if}
								</td>
								<MemberRole
									busy={saving === member.id}
									{member}
									onSave={(nextRole, active): Promise<void> => save(member, nextRole, active)}
									{roleLabel}
									roles={roleNames}
								/>
								<td class="active-cell">
									<span class="cell-label">Active</span>
									<input
										aria-label={`Active for ${member.email}`}
										checked={member.active}
										class="ui-toggle"
										onchange={(event): void => {
											const input = event.currentTarget as HTMLInputElement
											void save(member, member.role, input.checked)
										}}
										type="checkbox"
									>
								</td>
								<td class="muted login-cell">
									<span class="cell-label">Last sign-in</span>
									{member.lastLoginAt ? formatDateTime(member.lastLoginAt) : "Never"}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</section>
	{/if}

	{#if canRoles}
		<section aria-labelledby="roles-title" class="ui-card padded audit">
			<div class="section-head">
				<h2 id="roles-title">Roles</h2>
				<span>{roleDefs.length} {roleDefs.length === 1 ? "role" : "roles"}</span>
			</div>
			<p class="muted roles-lead">
				Roles grant permissions to every member holding them. The admin role always allows everything and cannot
				be changed.
			</p>

			<form
				class="invite"
				onsubmit={(event): void => {
					event.preventDefault()
					void addRole()
				}}
			>
				<label class="field">
					<span>New role name</span>
					<input class="ui-input" placeholder="stage2-reviewer" bind:value={newRoleName}>
				</label>
				<button
					class="ui-button primary"
					disabled={savingRole === "new" || newRoleName.trim() === ""}
					type="submit"
				>
					{savingRole === "new" ? "Adding…" : "Add role"}
				</button>
			</form>

			<div class="ui-table roles-matrix">
				<table>
					<thead>
						<tr>
							<th scope="col">Permission</th>
							{#each roleDefs as def (def.name)}
								<th scope="col">
									<span class="role-head">
										{roleLabel(def.name)}
										{#if def.locked}
											<span class="ui-badge" data-tone="ready">Locked</span>
										{:else}
											<button
												aria-label={`Delete the ${def.name} role`}
												class="ui-button quiet sm"
												disabled={savingRole === def.name}
												onclick={(): void => void removeRole(def)}
												title={`Delete the ${def.name} role`}
												type="button"
											>
												Delete
											</button>
										{/if}
									</span>
								</th>
							{/each}
						</tr>
					</thead>
					<tbody>
						{#each permissionCatalog as permission (permission.value)}
							<tr>
								<td>
									<strong>{permission.label}</strong>
									<span class="muted perm-desc">{permission.description}</span>
								</td>
								{#each roleDefs as def (def.name)}
									<td class="perm-cell">
										<input
											aria-label={`${permission.label} for the ${def.name} role`}
											checked={roleHas(def, permission.value)}
											class="ui-toggle"
											disabled={def.locked || savingRole === def.name}
											onchange={(event): void => {
												const input = event.currentTarget as HTMLInputElement
												void saveRolePermissions(def, permission.value, input.checked)
											}}
											title={def.locked ? "The admin role always allows everything" : undefined}
											type="checkbox"
										>
									</td>
								{/each}
							</tr>
						{/each}
					</tbody>
				</table>
			</div>

			<div class="roles-cards">
				{#each roleDefs as def (def.name)}
					<div class="role-card">
						<div class="role-card-head">
							<span class="role-head">
								<strong>{roleLabel(def.name)}</strong>
								{#if def.locked}
									<span class="ui-badge" data-tone="ready">Locked</span>
								{:else}
									<button
										aria-label={`Delete the ${def.name} role`}
										class="ui-button quiet sm"
										disabled={savingRole === def.name}
										onclick={(): void => void removeRole(def)}
										title={`Delete the ${def.name} role`}
										type="button"
									>
										Delete
									</button>
								{/if}
							</span>
						</div>
						{#each permissionCatalog as permission (permission.value)}
							<label class="perm-row">
								<input
									aria-label={`${permission.label} for the ${def.name} role`}
									checked={roleHas(def, permission.value)}
									class="ui-toggle"
									disabled={def.locked || savingRole === def.name}
									onchange={(event): void => {
										const input = event.currentTarget as HTMLInputElement
										void saveRolePermissions(def, permission.value, input.checked)
									}}
									type="checkbox"
								>
								<span class="perm-row-copy">
									<strong>{permission.label}</strong>
									<span class="muted perm-desc">{permission.description}</span>
								</span>
							</label>
						{/each}
					</div>
				{/each}
			</div>
		</section>
	{/if}

	<section aria-labelledby="audit-title" class="ui-card padded audit">
		<div class="section-head">
			<h2 id="audit-title">Recent actions</h2>
			<div class="audit-actions">
				<a class="ui-button quiet sm" href="/audit">Full log</a>
				<button class="ui-button quiet sm" onclick={refresh} type="button">Refresh</button>
			</div>
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

.ui-notice {
	margin-bottom: var(--space-4);
}

.member-link {
	display: flex;
	gap: var(--space-2);
	align-items: center;
	color: var(--text);
	text-decoration: none;
}

.member-copy {
	display: flex;
	flex-direction: column;
	min-width: 0;
}

.section-head {
	display: flex;
	gap: var(--space-3);
	align-items: baseline;
	justify-content: space-between;
	margin-bottom: var(--space-3);
}

.audit-actions {
	display: flex;
	gap: var(--space-2);
	align-items: center;
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
	min-width: 11.25rem;
}

.field span {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-2);
}

.role-field {
	flex: 0 0 10rem;
}

.roster {
	padding-top: var(--space-2);
	border-top: 1px solid var(--border);
}

.roster th:nth-child(1) {
	width: 45%;
}

.roster th:nth-child(2) {
	width: 13.75rem;
}

.roster th:nth-child(3) {
	width: 5.5rem;
}

.roster th:nth-child(4) {
	width: 12rem;
}

.roster td strong {
	display: block;
}

.roster td .muted {
	font-size: var(--text-xs);
}

.member-link:hover strong {
	text-decoration: underline;
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

.roles-lead {
	margin-bottom: var(--space-3);
	font-size: var(--text-sm);
}

.role-head {
	display: inline-flex;
	gap: var(--space-2);
	align-items: center;
}

.perm-desc {
	display: block;
	font-size: var(--text-xs);
	font-weight: 400;
}

.perm-cell {
	text-align: center;
}

code {
	font-family: var(--font-mono);
	font-size: var(--text-xs);
}

.cell-label {
	display: none;
}

.roles-cards {
	display: none;
}

@media (max-width: 47.5rem) {
	.audit table {
		min-width: 40rem;
	}

	/* The roster becomes one card per member instead of a scrolling table. */
	.roster table {
		display: block;
		min-width: 0;
	}

	.roster thead {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		white-space: nowrap;
		clip: rect(0 0 0 0);
	}

	.roster tbody,
	.roster tr,
	.roster :global(td) {
		display: block;
	}

	.roster tr {
		padding: var(--space-3);
		margin-bottom: var(--space-2);
		background: var(--surface-2);
		border: 1px solid var(--border);
		border-radius: var(--radius-md);
	}

	.roster tr:last-child {
		margin-bottom: 0;
	}

	.roster td {
		padding: 0 0 var(--space-2);
		border-bottom: 0;
	}

	.roster td:last-child {
		padding-bottom: 0;
	}

	.roster .member-link {
		padding-bottom: var(--space-1);
	}

	.cell-label {
		display: block;
		padding-bottom: var(--space-1);
		font-size: var(--text-xs);
		font-weight: 700;
		color: var(--text-3);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.roster td.active-cell {
		display: flex;
		gap: var(--space-2);
		align-items: center;
		justify-content: space-between;
	}

	.roster td.active-cell .cell-label {
		padding-bottom: 0;
	}

	.login-cell {
		font-size: var(--text-xs);
	}

	/* The permission matrix becomes one card per role. */
	.roles-matrix {
		display: none;
	}

	.roles-cards {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.role-card {
		padding: var(--space-3);
		background: var(--surface-2);
		border: 1px solid var(--border);
		border-radius: var(--radius-md);
	}

	.role-card-head {
		padding-bottom: var(--space-2);
	}

	.role-card-head .role-head {
		display: flex;
		gap: var(--space-2);
		align-items: center;
		justify-content: space-between;
		width: 100%;
	}

	.perm-row {
		display: flex;
		gap: var(--space-2);
		align-items: flex-start;
		padding: var(--space-2) 0;
		cursor: pointer;
		border-top: 1px solid var(--border);
	}

	.perm-row:has(input:disabled) {
		cursor: default;
	}

	.perm-row .ui-toggle {
		flex: none;
		margin-top: 2px;
	}

	.perm-row-copy {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
		font-size: var(--text-sm);
	}

	.perm-row-copy .perm-desc {
		font-size: var(--text-xs);
	}
}
</style>
