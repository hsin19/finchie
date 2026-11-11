<script lang="ts">
import type {
    AccountLine,
    Trade,
} from "#lib/api/client.js";
import Counterpart from "#lib/components/Counterpart.svelte";
import {
    formatAmount,
    negate,
    sign,
} from "#lib/money.js";

/** An account's lines with their running balances; a trade's price and the money paid or received (ADR 0017). */
let { lines, note, trade }: { lines: AccountLine[]; note: string; trade: boolean; } = $props();

/** What a trade paid or received, or what was paid in advance for it. */
function tradeMoney(t: Trade): string {
    if (t.prepaid) return `預繳 ${formatAmount(t.prepaid)}`;
    return sign(t.net) < 0 ? `付 ${formatAmount(negate(t.net))}` : `收 ${formatAmount(t.net)}`;
}

function amountTone(value: string) {
    return sign(value) < 0 ? "text-neg" : sign(value) > 0 ? "text-pos" : "text-dim";
}
</script>

<section class="panel px-3.5 pt-1 pb-2.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
        <h2 class="label">LINES · 明細（{lines.length}）</h2>
        <span class="text-[12px] text-mute">新的在上 · {note}</span>
    </div>
    <div class="overflow-x-auto">
        <table class="w-full min-w-[820px] border-collapse">
            <thead>
                <tr class="text-left text-mute">
                    <th class="py-2 pr-2 font-medium">DATE</th>
                    <th class="px-2 py-2 font-medium">DESCRIPTION</th>
                    <th class="px-2 py-2 font-medium">{trade ? "TRADE" : "MEMO"}</th>
                    <th class="px-2 py-2 font-medium">{trade ? "MONEY" : "COUNTERPARTY"}</th>
                    <th class="px-2 py-2 text-right font-medium">AMOUNT</th>
                    <th class="py-2 pl-2 text-right font-medium">BALANCE</th>
                </tr>
            </thead>
            <tbody>
                {#each lines as line (line.id)}
                    <tr class="border-t border-rule hover:bg-raised">
                        <td class="py-2 pr-2 whitespace-nowrap text-dim">{line.date}</td>
                        <td class="px-2 py-2">
                            {line.description}
                            {#if line.counterpart}<Counterpart other={line.counterpart} out={sign(line.amount) < 0} class="block text-[12px] text-dim hover:text-ink" />{/if}
                        </td>
                        {#if line.trade}
                            <td class="px-2 py-2 text-dim">{line.trade.category} @ {formatAmount(line.trade.price)}</td>
                            <td class="px-2 py-2 whitespace-nowrap text-mute" title={`價金 ${line.trade.amount} · 手續費 ${line.trade.fee} · 稅 ${line.trade.tax}`}>
                                {tradeMoney(line.trade)}
                            </td>
                        {:else}
                            <td class="px-2 py-2 text-dim">{line.memo ?? ""}</td>
                            <td class="px-2 py-2 text-mute">{line.counterparty ?? ""}</td>
                        {/if}
                        <td class="px-2 py-2 text-right whitespace-nowrap {amountTone(line.amount)}">
                            {formatAmount(line.amount, { signed: true })} <span class="text-mute">{line.unit}</span>
                        </td>
                        <td class="py-2 pl-2 text-right whitespace-nowrap text-dim">{line.balance ? formatAmount(line.balance) : ""}</td>
                    </tr>
                {:else}
                    <tr><td colspan="6" class="py-2.5 text-mute">這個帳戶還沒有明細：對帳單只印了它的餘額。</td></tr>
                {/each}
            </tbody>
        </table>
    </div>
</section>
