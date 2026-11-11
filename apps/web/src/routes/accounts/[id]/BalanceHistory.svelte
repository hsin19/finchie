<script lang="ts">
import { historyByDay } from "#lib/accounts.js";
import type { AccountDetail } from "#lib/api/client.js";
import {
    formatAmount,
    sign,
} from "#lib/money.js";

/** Every balance the account has had: one per statement, or an exchange's by day (ADR 0018). */
let { account, by }: { account: AccountDetail; by: "days" | "statements"; } = $props();

const days = $derived(by === "days" ? historyByDay(account.history) : []);
</script>

<section class="panel px-3.5 pt-1 pb-2.5">
    <h2 class="label py-2.5">HISTORY · {by === "days" ? "每天的快照" : "每一期的餘額"}</h2>
    <table class="border-collapse">
        <tbody>
            {#if by === "days"}
                {#each days as day (day.asOf)}
                    <tr class="border-t border-rule align-top">
                        <td class="py-1.5 pr-4 whitespace-nowrap text-dim">{day.asOf}</td>
                        <td class="py-1.5">
                            {#each day.balances as balance, i (balance.unit)}
                                {i > 0 ? " · " : ""}<span class={sign(balance.amount) === 0 ? "text-mute" : "text-ink"}>{formatAmount(balance.amount)}</span>
                                <span class="text-mute">{balance.unit}</span>
                            {/each}
                        </td>
                    </tr>
                {/each}
            {:else}
                {#each account.history as balance (`${balance.statementId ?? ""}:${balance.unit}:${balance.asOf}`)}
                    <tr class="border-t border-rule">
                        <td class="py-1.5 pr-4 text-dim">{balance.asOf}</td>
                        <td class="py-1.5 pr-2 text-right {sign(balance.amount) < 0 ? 'text-neg' : 'text-ink'}">{formatAmount(balance.amount)}</td>
                        <td class="py-1.5 pr-4 text-mute">{balance.unit}</td>
                    </tr>
                {/each}
            {/if}
        </tbody>
    </table>
</section>
