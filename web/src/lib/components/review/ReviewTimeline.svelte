<script lang="ts">
import { type Bundle } from "../../api"
import { formatDateTime, humanEvent } from "../../format"

interface Props {
	events: Bundle["events"]
}

let { events }: Props = $props()
let showAll = $state(false)
const visible = $derived(showAll ? events : events.slice(-6))
</script>

<div class="section">
	<h3>Timeline</h3>
	{#each visible as event (event.id)}
		<div class="event">
			<span class="dot"></span>
			<div>
				<strong>{humanEvent(event.kind)}</strong>
				{#if event.actorEmail}
					<span> · {event.actorEmail}</span>
				{/if}
				<small>{formatDateTime(event.createdAt)}</small>
			</div>
		</div>
	{/each}
	{#if events.length > 6}
		<button
			class="link more-events"
			onclick={(): void => {
				showAll = !showAll
			}}
			type="button"
		>
			{showAll ? "Show fewer events" : `Show all ${events.length} events`}
		</button>
	{/if}
</div>

<style>
.section {
	padding-top: var(--space-4);
	margin-top: var(--space-4);
	border-top: 1px solid var(--border);
}
.section h3 {
	padding-bottom: var(--space-2);
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-3);
}
.event {
	display: flex;
	gap: var(--space-2);
	padding: var(--space-1) 0;
	font-size: var(--text-sm);
}
.event .dot {
	flex: none;
	width: 0.5rem;
	height: 0.5rem;
	margin-top: 0.5rem;
	background: var(--primary);
	border-radius: 50%;
}
.event small {
	display: block;
	font-size: var(--text-xs);
	color: var(--text-3);
}
.more-events {
	margin-top: var(--space-1);
}
</style>
