<script lang="ts">
import type { Member } from "../../api"

interface Props {
	busy: boolean
	member: Member
	onSave(role: string, active: boolean): Promise<void>
	roleLabel(name: string): string
	roles: string[]
}

let { busy, member, onSave, roleLabel, roles }: Props = $props()

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
			<option value={option}>{roleLabel(option)}</option>
		{/each}
	</select>
</td>
