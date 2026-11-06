<script lang="ts">
import { formatHours } from "../../format"

interface Props {
	segments: { label: string; tone: string; value: number }[]
}

let { segments }: Props = $props()

// The cut (deflated time) subtracts from the evidence, so it overlays the end of the bar instead of extending it.
const kept = $derived(segments.filter(segment => segment.tone !== "cut"))
const cut = $derived(segments.filter(segment => segment.tone === "cut"))
const keptTotal = $derived(kept.reduce((sum, segment) => sum + segment.value, 0))
const cutTotal = $derived(cut.reduce((sum, segment) => sum + segment.value, 0))
const cutPercent = $derived(keptTotal > 0 ? Math.min(100, (cutTotal / keptTotal) * 100) : 0)
const description = $derived(
	[
		...kept.map(segment => `${segment.label} ${formatHours(segment.value)}`),
		...(cutTotal > 0 ? [`Deflated ${formatHours(cutTotal)} removed`] : []),
	].join(", "),
)

// legendValue marks the cut as a subtraction in the legend.
function legendValue(segment: { label: string; tone: string; value: number }): string {
	return segment.tone === "cut" ? `−${formatHours(segment.value)}` : formatHours(segment.value)
}
</script>

{#if segments.length > 0}
	<div class="time-evidence">
		<span class="time-title">Time evidence</span>
		<div aria-label={description} class="time-bar" role="img">
			{#each kept as segment (segment.tone)}
				<span class="time-segment" data-tone={segment.tone} style:flex-grow={segment.value}></span>
			{/each}
			{#if cutTotal > 0 && keptTotal > 0}
				<span class="time-cut" style:width={`${cutPercent}%`}></span>
			{/if}
		</div>
		<ul class="time-legend">
			{#each segments as segment (segment.tone)}
				<li>
					<span class="time-dot" data-tone={segment.tone}></span>
					<span>{segment.label}</span>
					<strong>{legendValue(segment)}</strong>
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
	position: relative;
	display: flex;
	gap: 2px;
	height: 0.75rem;
	overflow: hidden;
	background: var(--surface-3);
	border-radius: var(--radius-full);
}

.time-segment {
	flex-basis: 0;
	min-width: 2px;
}

.time-cut {
	position: absolute;
	top: 0;
	right: 0;
	bottom: 0;
	background: repeating-linear-gradient(-45deg, var(--text-3) 0 4px, transparent 4px 8px);
	border-left: 2px solid var(--text-3);
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
	width: 0.5rem;
	height: 0.5rem;
	border-radius: 2px;
}

.time-legend strong {
	font-weight: 700;
	font-variant-numeric: tabular-nums;
	color: var(--text-2);
}
</style>
