<script lang="ts">
import type { Member } from "../../api"

interface Props {
	busy: boolean
	member: Member
	onSave(role: string, active: boolean): Promise<boolean>
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
		onchange={(): void => {
			const next = role
			void onSave(next, member.active).then(saved => {
				if (!saved) role = member.role
			})
		}}
		bind:value={role}
	>
		{#each roles as option (option)}
			<option value={option}>{roleLabel(option)}</option>
		{/each}
	</select>
</td>

<style>
select {
	width: 100%;
}
</style>
