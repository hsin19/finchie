<script lang="ts">
import type { AccountDetail } from "#lib/api/client.js";
import { yearlyDate } from "#lib/dates.js";
import { formatAmount } from "#lib/money.js";

/**
 * When a points account's points lapse (ADR 0019): worked out by each program's rule from what its bills
 * added and used, then what the newest bill says, and what lapsed between bills. An account the owner keeps
 * has only what they entered.
 */
let { account, today, owned }: { account: AccountDetail; today: string; owned: boolean; } = $props();

const lots = $derived(account.lots ?? []);
// A day already gone: the rule says they lapsed, but the balance still holds them.
const overdue = $derived(lots.filter(l => l.by < today));
const ahead = $derived(lots.filter(l => l.by >= today));
const lapsed = $derived((account.lapsed ?? []).toReversed());
</script>

<section class="panel px-3.5 pt-1 pb-2.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
        <h2 class="label">EXPIRING · 到期時程</h2>
        <span class="text-[12px] text-mute">{owned ? "你輸入的" : "照各點數的效期規則，從每期帳單入的點推算；兌換先扣最快到期的"}</span>
    </div>

    {#if !owned}
        <table class="border-collapse">
            <tbody>
                {#each ahead as lot (`${lot.unit}:${lot.by}:${lot.unaged}`)}
                    <tr class="border-t border-rule">
                        <td class="py-1.5 pr-4 text-dim">{yearlyDate(lot.by, today)} {lot.unaged ? "前（最晚）" : "前"}</td>
                        <td class="py-1.5 pr-2 text-right text-ink">{formatAmount(lot.amount)}</td>
                        <td class="py-1.5 pr-4 text-mute">{lot.unit}</td>
                        <td class="py-1.5 text-[12px] text-mute">{lot.unaged ? "第一期帳單之前就有的點，到期日不明" : ""}</td>
                    </tr>
                {:else}
                    <tr><td class="py-2.5 text-mute">{lots.length > 0 ? "" : "不知道這些點數的效期規則，只能看帳單上寫的。"}</td></tr>
                {/each}
                {#each overdue as lot (`overdue:${lot.unit}:${lot.by}:${lot.unaged}`)}
                    <tr class="border-t border-rule">
                        <td class="py-1.5 pr-4 text-accent">{yearlyDate(lot.by, today)} 前</td>
                        <td class="py-1.5 pr-2 text-right text-ink">{formatAmount(lot.amount)}</td>
                        <td class="py-1.5 pr-4 text-mute">{lot.unit}</td>
                        <td class="py-1.5 text-[12px] text-accent">照規則該過期了，餘額卻還在：規則可能不對</td>
                    </tr>
                {/each}
            </tbody>
        </table>
    {/if}

    <h3 class="label mt-3 mb-1">{owned ? "" : "帳單上寫的"}</h3>
    <table class="border-collapse">
        <tbody>
            {#each account.expiries as expiry (`${expiry.unit}:${expiry.by}`)}
                <tr class="border-t border-rule">
                    <td class="py-1.5 pr-4 text-dim">{yearlyDate(expiry.by, today)} 前</td>
                    <td class="py-1.5 pr-2 text-right text-ink">{formatAmount(expiry.amount)}</td>
                    <td class="py-1.5 text-mute">{expiry.unit}</td>
                </tr>
            {:else}
                <tr><td class="py-2 text-mute">{owned ? "沒有輸入到期的點數。" : "最新一期帳單沒有寫到期的點數。"}</td></tr>
            {/each}
        </tbody>
    </table>
    {#if !owned && account.expiries.length > 0}<p class="m-0 mt-1 text-[12px] text-mute">同一行可能是幾個月加起來</p>{/if}

    {#if lapsed.length > 0}
        <h3 class="label mt-3 mb-1">LAPSED · 過期紀錄</h3>
        <table class="border-collapse">
            <tbody>
                {#each lapsed as l (`${l.unit}:${l.to}`)}
                    <tr class="border-t border-rule">
                        <td class="py-1.5 pr-4 text-dim">{yearlyDate(l.from, today)} → {yearlyDate(l.to, today)}</td>
                        <td class="py-1.5 pr-2 text-right text-neg">{formatAmount(l.amount)}</td>
                        <td class="py-1.5 text-mute">{l.unit}</td>
                    </tr>
                {/each}
            </tbody>
        </table>
    {/if}
</section>
