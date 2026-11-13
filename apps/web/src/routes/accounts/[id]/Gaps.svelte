<script lang="ts">
import type { AccountDetail } from "#lib/api/client.js";
import {
    fillGap,
    type GapRef,
} from "#lib/api/mutations.js";
import {
    formatAmount,
    sign,
} from "#lib/money.js";

// Where the account's balances do not join up. One whose reports are lost can be filled with the gap's
// amount, nothing to type: the page reloads with the ledger once it is stored, the gap is gone, and the
// fill is among the account's lines (AccountLines), where its memo is written and it is taken back.
let { account, explanation }: { account: AccountDetail; explanation: string; } = $props();

/** The gap whose fill is on its way, and why the last one failed. */
let busy = $state<string>();
let failed = $state<{ key: string; error: string; }>();

const gapKey = (gap: GapRef) => `${gap.unit}:${gap.from}:${gap.to}`;

async function fill(gap: GapRef) {
    if (busy) return;
    const key = gapKey(gap);
    busy = key;
    failed = undefined;
    const { error } = await fillGap(account.id, gap);
    busy = undefined;
    if (error) failed = { key, error };
}
</script>

{#if account.gaps.length > 0}
    <section class="panel border-neg px-3.5 py-2.5">
        <h2 class="label text-neg">GAPS · 餘額接不起來</h2>
        <p class="mt-1 text-dim">{explanation}</p>
        <p class="mt-1 text-mute">找不到那期帳單的話，按「補帳」把差額記成明細裡的一筆人工補帳，這段就算接上。之後帳單找到了，會自動改用帳單。</p>
        <table class="mt-1.5 border-collapse">
            <tbody>
                {#each account.gaps as gap (gapKey(gap))}
                    {@const key = gapKey(gap)}
                    <tr class="border-t border-rule">
                        <td class="py-1.5 pr-4">{gap.from} → {gap.to}</td>
                        <td class="py-1.5 pr-2 text-right {sign(gap.unseen) < 0 ? 'text-neg' : sign(gap.unseen) > 0 ? 'text-pos' : 'text-dim'}">{formatAmount(gap.unseen, { signed: true })}</td>
                        <td class="py-1.5 pr-4 text-mute">{gap.unit} 沒有明細</td>
                        <td class="py-1.5">
                            <button type="button" class="control min-h-8" disabled={busy !== undefined} onclick={() => void fill(gap)}>
                                {busy === key ? "補帳中…" : "補帳"}
                            </button>
                        </td>
                    </tr>
                    {#if failed?.key === key}
                        <tr><td colspan="4" class="pb-1.5 break-words text-neg">{failed.error}</td></tr>
                    {/if}
                {/each}
            </tbody>
        </table>
    </section>
{/if}
