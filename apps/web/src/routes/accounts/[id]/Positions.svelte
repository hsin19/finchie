<script lang="ts">
import {
    describePrice,
    shownPositions,
} from "#lib/accounts.js";
import type { AccountDetail } from "#lib/api/client.js";
import {
    approx,
    formatAmount,
    sign,
} from "#lib/money.js";

/** A securities account's positions, with their cost and gains where known (ADR 0017). */
let { account, base }: { account: AccountDetail; base: string; } = $props();

const positions = $derived(shownPositions(account.positions));
const closeOf = $derived(new Map(account.balances.map(b => [b.unit, b])));

/** Gain as a share of cost: a ratio, for showing only. */
function percent(gain: string, cost: string): string {
    return approx(cost) > 0 ? `${((approx(gain) / approx(cost)) * 100).toFixed(1)}%` : "";
}

function gainTone(value: string | undefined): string {
    return value ? (sign(value) < 0 ? "text-neg" : "text-pos") : "text-mute";
}

const unknownCost = "最舊那期之前就有，或之後有缺口：成本不知道";
</script>

<section class="panel px-3.5 pt-1 pb-2.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
        <h2 class="label">POSITIONS · 持股與損益</h2>
        <span class="text-[12px] text-mute">平均成本，含手續費；賣出扣手續費和稅</span>
    </div>
    <div class="overflow-x-auto">
        <table class="w-full min-w-[760px] border-collapse">
            <thead>
                <tr class="text-left text-mute">
                    <th class="py-2 pr-2 font-medium">SECURITY</th>
                    <th class="px-2 py-2 text-right font-medium">SHARES</th>
                    <th class="px-2 py-2 text-right font-medium">CLOSE</th>
                    <th class="px-2 py-2 text-right font-medium">WORTH</th>
                    <th class="px-2 py-2 text-right font-medium">COST</th>
                    <th class="px-2 py-2 text-right font-medium">GAIN</th>
                    <th class="py-2 pl-2 text-right font-medium">REALIZED</th>
                </tr>
            </thead>
            <tbody>
                {#each positions as position (position.unit)}
                    {@const close = closeOf.get(position.unit)}
                    {@const soldOut = sign(position.shares) === 0}
                    <tr class="border-t border-rule">
                        <td class="py-2 pr-2">{position.unit} <span class="text-mute">{position.code ?? ""}</span></td>
                        <td class="px-2 py-2 text-right">{soldOut ? "出清" : formatAmount(position.shares)}</td>
                        <td class="px-2 py-2 text-right text-dim" title={close?.price ? describePrice(close.unit, close.price, base) : ""}>
                            {close?.price ? formatAmount(close.price.quoted) : ""}
                        </td>
                        <td class="px-2 py-2 text-right">{soldOut ? "" : position.worth !== undefined ? formatAmount(position.worth) : "—"}</td>
                        <td class="px-2 py-2 text-right text-dim" title={position.cost !== undefined ? `${position.since} 起的成交` : unknownCost}>
                            {position.cost !== undefined ? formatAmount(position.cost) : soldOut ? "" : "不知道"}
                        </td>
                        <td class="px-2 py-2 text-right {gainTone(position.gain)}">
                            {#if position.gain !== undefined && position.cost !== undefined}
                                {formatAmount(position.gain, { signed: true })} <span class="text-[12px]">{percent(position.gain, position.cost)}</span>
                            {/if}
                        </td>
                        <td class="py-2 pl-2 text-right {gainTone(position.realized)}">
                            {position.realized !== undefined && sign(position.realized) !== 0 ? formatAmount(position.realized, { signed: true }) : ""}
                        </td>
                    </tr>
                {:else}
                    <tr><td colspan="7" class="py-2.5 text-mute">目前沒有持股。</td></tr>
                {/each}
            </tbody>
        </table>
    </div>
</section>
