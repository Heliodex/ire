<script lang="ts">
import { type Bundle } from "../../api"
import { formatMoneyAmount } from "../../format"

interface Props {
	bom: Bundle["bom"]
	totalCents: number
}

let { bom, totalCents }: Props = $props()
</script>

{#if bom.length === 0}
	<div class="ui-card ui-empty">
		<strong>No BOM lines</strong>
		<span>Half Life sent no priced parts with this ship.</span>
	</div>
{:else}
	<div class="ui-card table-wrap">
		<table class="ui-table">
			<thead>
				<tr>
					<th>Part</th>
					<th>Purpose</th>
					<th class="num">Qty</th>
					<th class="num">Unit (US$)</th>
					<th>Vendor</th>
				</tr>
			</thead>
			<tbody>
				{#each bom as line (line.name)}
					<tr>
						<td>
							{#if line.link}
								<a href={line.link} rel="noreferrer" target="_blank">{line.name} ↗</a>
							{:else}
								{line.name}
							{/if}
						</td>
						<td>{line.purpose ?? "–"}</td>
						<td class="num">{line.quantity}</td>
						<td class="num">{formatMoneyAmount(line.unitCostCents)}</td>
						<td>{line.vendor ?? "–"}</td>
					</tr>
				{/each}
			</tbody>
			<tfoot>
				<tr>
					<td colspan="3">Total (US$)</td>
					<td class="num">{formatMoneyAmount(totalCents)}</td>
					<td></td>
				</tr>
			</tfoot>
		</table>
	</div>
{/if}

<style>
.table-wrap {
	overflow-x: auto;
}

.num {
	font-variant-numeric: tabular-nums;
	text-align: right;
}
</style>
