<script lang="ts">
import { formatHours } from "../../format"

interface Props {
	segments: { label: string; tone: string; value: number }[]
}

let { segments }: Props = $props()
</script>

{#if segments.length > 0}
	<div class="time-evidence">
		<span class="time-title">Time evidence</span>
		<div
			aria-label={segments.map(segment => `${segment.label} ${formatHours(segment.value)}`).join(", ")}
			class="time-bar"
			role="img"
		>
			{#each segments as segment (segment.tone)}
				<span class="time-segment" data-tone={segment.tone} style:flex-grow={segment.value}></span>
			{/each}
		</div>
		<ul class="time-legend">
			{#each segments as segment (segment.tone)}
				<li>
					<span class="time-dot" data-tone={segment.tone}></span>
					<span>{segment.label}</span>
					<strong>{formatHours(segment.value)}</strong>
				</li>
			{/each}
		</ul>
	</div>
{/if}

<style>
.time-evidence {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding-top: var(--space-3);
}

.time-title {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-3);
	text-transform: uppercase;
	letter-spacing: 0.04em;
}

.time-bar {
	display: flex;
	gap: 0.125rem;
	height: 0.625rem;
	overflow: hidden;
	background: var(--surface-3);
	border-radius: var(--radius-full);
}

.time-segment {
	flex-basis: 0;
	min-width: 0.125rem;
}

.time-segment[data-tone="journals"],
.time-dot[data-tone="journals"] {
	background: var(--accent-purple);
}

.time-segment[data-tone="lapse"],
.time-dot[data-tone="lapse"] {
	background: var(--accent-orange);
}

.time-segment[data-tone="hackatime"],
.time-dot[data-tone="hackatime"] {
	background: var(--accent-blue);
}

.time-segment[data-tone="cut"],
.time-dot[data-tone="cut"] {
	background: var(--text-3);
}

.time-legend {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1) var(--space-3);
	padding: 0;
	margin: 0;
	font-size: var(--text-xs);
	color: var(--text-3);
	list-style: none;
}

.time-legend li {
	display: inline-flex;
	gap: 0.25rem;
	align-items: center;
}

.time-dot {
	flex: none;
	width: 0.4375rem;
	height: 0.4375rem;
	border-radius: 0.125rem;
}

.time-legend strong {
	font-weight: 700;
	font-variant-numeric: tabular-nums;
	color: var(--text-2);
}
</style>
