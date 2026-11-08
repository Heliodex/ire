<script lang="ts">
interface Props {
	above: boolean
	group: string
	label: string
	status: string
	tone: string
	x: number
	y: number
}

let { above, group, label, status, tone, x, y }: Props = $props()
</script>

<div class="check-tip" role="tooltip" style={`left:${x}px; top:${y}px`} class:above class:below={!above}>
	<div class="tip-head">
		<span class="tip-group">{group}</span>
		<span class="ui-badge" data-tone={tone}>{status}</span>
	</div>
	<span class="tip-label">{label}</span>
</div>

<style>
.check-tip {
	position: fixed;
	z-index: var(--layer-toast);
	display: flex;
	flex-direction: column;
	gap: 0.25rem;
	width: max-content;
	max-width: 16.5rem;
	padding: var(--space-2) var(--space-3);
	pointer-events: none;
	background: var(--surface);
	border: 1px solid var(--border-2);
	border-radius: var(--radius-md);
	box-shadow: var(--shadow);
}
.check-tip.above { transform: translate(-50%, calc(-100% - 0.5rem)); }
.check-tip.below { transform: translate(-50%, 0.5rem); }
.tip-head {
	display: flex;
	gap: var(--space-3);
	align-items: center;
	justify-content: space-between;
}
.tip-group {
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-3);
	text-transform: uppercase;
	letter-spacing: 0.04em;
}
.tip-label {
	font-size: var(--text-sm);
	font-weight: 600;
	line-height: 1.35;
	color: var(--text);
}
</style>
