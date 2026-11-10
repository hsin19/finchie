<script lang="ts">
import { lineAccount } from "#lib/accounts.js";
import type { Transaction } from "#lib/api/client.js";
import Counterpart from "#lib/components/Counterpart.svelte";
import {
    shortDate,
    weekday,
} from "#lib/dates.js";
import { breakdown } from "#lib/family.js";
import { flowOf } from "#lib/flows.js";
import { kinds } from "#lib/kinds.js";
import { sign } from "#lib/money.js";
import { tone } from "#lib/tone.js";
import {
    amountText,
    amountTone,
    byDay,
    counterpartLink,
    dayTotal,
    original,
    unitSuffix,
} from "#lib/transactions.js";
import type { Snippet } from "svelte";

/**
 * The rows: a table on wide screens; on narrow ones a list, by day when sorted by date. Both from the
 * same views of a line (transactions.ts). `children` goes below them: the pager.
 */
let { items, selectedId, byDate, onselect, base, children }: {
    items: Transaction[];
    selectedId?: number;
    byDate: boolean;
    onselect: (t: Transaction) => void;
    base: string;
    children: Snippet;
} = $props();
</script>

<section class="panel min-w-0 px-3.5 pt-1 pb-2.5">
    <!-- Desktop: a table. -->
    <div class="hidden overflow-x-auto md:block">
        <table class="w-full border-collapse">
            <thead>
                <tr class="text-left text-mute">
                    <th class="w-4 py-2 pr-1 font-medium"><span class="sr-only">選取</span></th>
                    <th class="px-2 py-2 font-medium">DATE</th>
                    <th class="px-2 py-2 font-medium">DESCRIPTION</th>
                    <th class="px-2 py-2 font-medium">KIND</th>
                    <th class="px-2 py-2 font-medium">ACCOUNT</th>
                    <th class="px-2 py-2 font-medium">CTRY</th>
                    <th class="px-2 py-2 text-right font-medium">ORIG</th>
                    <th class="py-2 pl-2 text-right font-medium">AMOUNT</th>
                </tr>
            </thead>
            <tbody>
                {#each items as t (t.id)}
                    <tr
                        id="tx-{t.id}"
                        class="cursor-pointer border-t border-rule {t.id === selectedId ? 'bg-raised' : 'hover:bg-raised/60'}"
                        onclick={() => onselect(t)}
                    >
                        <td class="py-2 pr-1 text-accent">{t.id === selectedId ? "▶" : ""}</td>
                        <td class="px-2 py-2 whitespace-nowrap">{shortDate(t.transactionDate)}</td>
                        <td class="max-w-[320px] px-2 py-2">
                            <span class="block truncate">{t.description}{#if t.memo}<span class="text-dim"> · {t.memo}</span>{/if}</span>
                            {#if breakdown(t)}<span class="block truncate text-[11px] text-mute">帳單上 {breakdown(t)}</span>{/if}
                            {#if t.counterpart}<Counterpart other={t.counterpart} out={sign(flowOf(t)) < 0} class="block truncate text-[11px] text-mute hover:text-ink" />{/if}
                        </td>
                        <td class="px-2 py-2 {tone[kinds[t.kind].tone]}">{kinds[t.kind].short}</td>
                        <td class="px-2 py-2 whitespace-nowrap text-dim">{lineAccount(t)}</td>
                        <td class="px-2 py-2 text-dim">{t.country ?? "—"}</td>
                        <td class="px-2 py-2 text-right whitespace-nowrap text-dim">{original(t)}</td>
                        <td class="py-2 pl-2 text-right whitespace-nowrap {amountTone(t)}">
                            <!-- A margin, not a space: Svelte drops the whitespace a block starts with. -->
                            {amountText(t)}{#if unitSuffix(t, base)}<span class="ml-1 text-mute">{unitSuffix(t, base)}</span>{/if}
                        </td>
                    </tr>
                {:else}
                    <tr class="border-t border-rule"><td colspan="8" class="py-3 text-mute">沒有符合的交易。</td></tr>
                {/each}
            </tbody>
        </table>
    </div>

    <!-- Mobile: grouped by day when sorted by date; by amount, one list with the date on each row. -->
    {#snippet mobileRow(t: Transaction, withDate: boolean)}
        <button type="button" class="flex min-h-11 w-full flex-col gap-0.5 border-t border-rule py-2.5 text-left text-ink" onclick={() => onselect(t)}>
            <span class="flex w-full justify-between gap-2.5">
                <span class="truncate">{t.description}{t.memo ? ` · ${t.memo}` : ""}</span>
                <span class="shrink-0 {amountTone(t)}">{amountText(t)}{#if unitSuffix(t, base)}<span class="ml-1 text-mute">{unitSuffix(t, base)}</span>{/if}</span>
            </span>
            <span class="text-[12px] text-mute">
                {withDate ? `${shortDate(t.transactionDate)} · ` : ""}{lineAccount(t)} · {kinds[t.kind].short}{original(t) ? ` · ${original(t)}` : ""}{breakdown(t) ? ` · 帳單上 ${breakdown(t)}` : ""}
                <!-- The row is a button, so the account the money went to is named, not linked. -->
                {t.counterpart ? ` · ${sign(flowOf(t)) < 0 ? "→" : "←"} ${counterpartLink(t.counterpart).text}` : ""}
            </span>
        </button>
    {/snippet}
    <div class="md:hidden">
        {#if byDate}
            {#each byDay(items) as day (day.date)}
                <div class="flex justify-between pt-3 pb-1.5 text-[12px] tracking-[0.06em] text-mute">
                    <span>{shortDate(day.date)} {weekday(day.date)}</span>
                    <span>{dayTotal(day.items, base)}</span>
                </div>
                {#each day.items as t (t.id)}
                    {@render mobileRow(t, false)}
                {/each}
            {:else}
                <p class="m-0 py-3 text-mute">沒有符合的交易。</p>
            {/each}
        {:else}
            {#each items as t (t.id)}
                {@render mobileRow(t, true)}
            {:else}
                <p class="m-0 py-3 text-mute">沒有符合的交易。</p>
            {/each}
        {/if}
    </div>

    {@render children()}
</section>
