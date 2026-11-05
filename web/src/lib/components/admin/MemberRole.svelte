<script lang="ts">
import type { Member } from "../../api"

interface Props {
	busy: boolean
	member: Member
	onSave(role: string, active: boolean): Promise<void>
}

let { busy, member, onSave }: Props = $props()

const roles = ["reviewer", "organizer", "admin", "auditor"] as const
const roleLabels: { [_: string]: string } = {
	admin: "Admin",
	auditor: "Auditor",
	organizer: "Organiser",
	reviewer: "Reviewer",
}

let role = $state("")

$effect((): void => {
	;({ role } = member)
})
</script>

<td>
	<select
		aria-label={`Role for ${member.email}`}
		class="ui-input"
		disabled={busy}
		onchange={(): void => void onSave(role, member.active)}
		bind:value={role}
	>
		{#each roles as option (option)}
			<option value={option}>{roleLabels[option]}</option>
		{/each}
	</select>
</td>
