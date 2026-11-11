<script lang="ts">
import {
    describePrice,
    holdingsByWorth,
} from "#lib/accounts.js";
import type { AccountDetail } from "#lib/api/client.js";
import { formatAmount } from "#lib/money.js";

/**
 * An exchange's coins and each one's worth (ADR 0018). Read every `intervalHours` hours, as the
 * backend's settings say.
 */
let { account, base, intervalHours }: { account: AccountDetail; base: string; intervalHours: number; } = $props();

const holdings = $derived(holdingsByWorth(account.balances));
</script>

<section class="panel px-3.5 pt-1 pb-2.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
        <h2 class="label">HOLDINGS · 持有（{holdings.length}）</h2>
        <span class="text-[12px] text-mute">每 {intervalHours} 小時從交易所 API 同步 · 參考價為每日匯率</span>
    </div>
    <table class="border-collapse">
        <thead>
            <tr class="text-left text-mute">
                <th class="py-2 pr-4 font-medium">COIN</th>
                <th class="px-2 py-2 text-right font-medium">AMOUNT</th>
                <th class="py-2 pl-4 text-right font-medium">≈ NT$</th>
            </tr>
        </thead>
        <tbody>
            {#each holdings as balance (balance.unit)}
                <tr class="border-t border-rule">
                    <td class="py-1.5 pr-4">{balance.unit}</td>
                    <td class="px-2 py-1.5 text-right">{formatAmount(balance.amount)}</td>
                    <td class="py-1.5 pl-4 text-right text-dim" title={balance.price ? describePrice(balance.unit, balance.price, base) : ""}>
                        {balance.worth !== undefined ? formatAmount(balance.worth) : "沒有價格"}
                    </td>
                </tr>
            {:else}
                <tr><td colspan="3" class="py-2.5 text-mute">目前沒有持有任何幣。</td></tr>
            {/each}
        </tbody>
    </table>
</section>
