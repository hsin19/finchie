<script lang="ts">
import type { MonthBill } from "#lib/bills.js";
import type { Month } from "#lib/dates.js";
import {
    institution,
    sortInstitutions,
} from "#lib/institutions.js";
import {
    approx,
    formatAmount,
} from "#lib/money.js";
import { statementsHref } from "#lib/urls.js";

let { bills, partial }: {
    /** Oldest first. */
    bills: MonthBill[];
    /** A month some banks have not closed yet: drawn fainter. */
    partial?: Month;
} = $props();

let view = $state<"chart" | "table">("chart");
let active = $state<number>();

// About four round steps (1, 2, or 5 × 10ⁿ) up to the tallest column as drawn: a bank's credit
// balance lowers the month's total but not its stack.
const ticks = $derived.by(() => {
    const max = Math.max(0, ...bills.map(b => stack(b).total));
    if (max <= 0) return [0];
    const power = 10 ** Math.floor(Math.log10(max / 4));
    const step = [1, 2, 5].map(f => f * power).find(s => s >= max / 4) ?? 10 * power;
    return Array.from({ length: Math.ceil(max / step) + 1 }, (_, i) => i * step);
});
const top = $derived(ticks.at(-1) || 1);

const banks = $derived(sortInstitutions([...new Set(bills.flatMap(b => b.parts.map(p => p.institution)))]));

function compact(n: number): string {
    return n >= 1000 ? `${n / 1000}K` : String(n);
}

// A credit balance has nothing to stack; its amount still shows in the tooltip and the table.
function stack(bill: MonthBill) {
    const parts = bill.parts.filter(p => approx(p.amount) > 0);
    return { parts, total: parts.reduce((n, p) => n + approx(p.amount), 0) };
}

function describe(bill: MonthBill): string {
    const parts = bill.parts.map(p => `${institution(p.institution).name} ${formatAmount(p.amount)}`).join("、");
    return `${bill.month} 帳單 ${formatAmount(bill.total)}${parts ? `：${parts}` : ""}`;
}

const GUTTER = 36;
const views = ["chart", "table"] as const;
</script>

<section class="panel flex flex-col gap-3 p-3 md:p-3.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2">
        <h2 class="label">TREND · 每月帳單</h2>
        <div role="group" aria-label="檢視方式" class="flex gap-1.5 text-[12px]">
            {#each views as option, i (option)}
                {#if i > 0}<span class="text-mute">·</span>{/if}
                <button
                    type="button"
                    aria-pressed={view === option}
                    class={view === option ? "text-accent underline underline-offset-4" : "text-mute hover:text-ink"}
                    onclick={() => (view = option)}
                >
                    {option}
                </button>
            {/each}
        </div>
    </div>

    {#if view === "chart"}
        <div class="relative">
            <div class="relative h-[200px]">
                {#each ticks as tick (tick)}
                    {@const bottom = `${(tick / top) * 100}%`}
                    <div class="absolute right-0 border-t {tick === 0 ? 'border-line-strong' : 'border-rule'}" style:left="{GUTTER}px" style:bottom></div>
                    <span class="absolute left-0 translate-y-1/2 pr-1.5 text-right text-[10px] text-mute tabular-nums" style:width="{GUTTER}px" style:bottom>{compact(tick)}</span>
                {/each}
                <div class="absolute inset-y-0 right-0 flex" style:left="{GUTTER}px">
                    {#each bills as bill, i (bill.month)}
                        {@const { parts, total } = stack(bill)}
                        <a
                            href={statementsHref(bill.month)}
                            aria-label={describe(bill)}
                            class="flex h-full flex-1 flex-col justify-end rounded-t-[4px] hover:bg-raised focus-visible:bg-raised"
                            onmouseenter={() => (active = i)}
                            onmouseleave={() => (active = undefined)}
                            onfocus={() => (active = i)}
                            onblur={() => (active = undefined)}
                        >
                            <span class="mx-auto flex w-[60%] max-w-6 flex-col-reverse {bill.month === partial ? 'opacity-55' : ''}" style:height="{Math.min(100, (total / top) * 100)}%">
                                {#each parts as part, j (part.institution)}
                                    <span
                                        class="block {j > 0 ? 'border-b-2 border-panel' : ''} {j === parts.length - 1 ? 'rounded-t-[4px]' : ''}"
                                        style:height="{(approx(part.amount) / total) * 100}%"
                                        style:background={institution(part.institution).color}
                                    ></span>
                                {/each}
                            </span>
                        </a>
                    {/each}
                </div>
            </div>
            <div class="mt-1 flex text-[10px] text-mute tabular-nums" style:padding-left="{GUTTER}px">
                {#each bills as bill (bill.month)}
                    <span class="flex-1 text-center {bill.month === partial ? 'text-accent' : ''}">{bill.month.slice(5)}</span>
                {/each}
            </div>

            {#if active !== undefined}
                {@const bill = bills[active]}
                {@const right = active < bills.length / 2}
                <!-- Beside the column, not over it: right of it in the left half, left of it in the right half. -->
                <div
                    role="tooltip"
                    class="pointer-events-none absolute top-0 z-10 min-w-40 rounded-[6px] border border-line-strong bg-ground px-3 py-2 text-[12px] shadow-lg {right ? '' : '-translate-x-full'}"
                    style:left="calc({GUTTER}px + (100% - {GUTTER}px) * {(active + (right ? 1 : 0)) / bills.length} {right ? '+' : '-'} 4px)"
                >
                    <div class="mb-1 flex justify-between gap-4"><span class="text-ink-bright">{bill.month}</span><span class="text-ink-bright">{formatAmount(bill.total)}</span></div>
                    {#each bill.parts as part (part.institution)}
                        <div class="flex items-center justify-between gap-4 text-dim">
                            <span><span class="mr-1.5 inline-block size-2 rounded-[2px]" style:background={institution(part.institution).color}></span>{institution(part.institution).name}</span>
                            <span class="tabular-nums">{formatAmount(part.amount)}</span>
                        </div>
                    {:else}
                        <div class="text-mute">沒有帳單</div>
                    {/each}
                    {#if bill.month === partial}<div class="mt-1 text-mute">還有銀行沒出帳</div>{/if}
                </div>
            {/if}
        </div>

        {#if banks.length > 0}
            <div class="flex flex-wrap gap-x-4 gap-y-1 text-[12px] text-dim">
                {#each banks as bank (bank)}
                    <span><span class="mr-1.5 inline-block size-2 rounded-[2px]" style:background={institution(bank).color}></span>{institution(bank).name}</span>
                {/each}
            </div>
        {/if}
    {:else}
        <div class="overflow-x-auto">
            <table class="w-full border-collapse text-[12px] tabular-nums">
                <thead>
                    <tr class="text-left text-mute">
                        <th class="py-1.5 pr-2 font-medium">MONTH</th>
                        {#each banks as bank (bank)}<th class="px-2 py-1.5 text-right font-medium">{institution(bank).name}</th>{/each}
                        <th class="py-1.5 pl-2 text-right font-medium">TOTAL</th>
                    </tr>
                </thead>
                <tbody>
                    {#each bills.toReversed() as bill (bill.month)}
                        <tr class="border-t border-rule">
                            <td class="py-1.5 pr-2"><a href={statementsHref(bill.month)}>{bill.month}</a></td>
                            {#each banks as bank (bank)}
                                {@const part = bill.parts.find(p => p.institution === bank)}
                                <td class="px-2 py-1.5 text-right {part ? 'text-dim' : 'text-mute'}">{part ? formatAmount(part.amount) : "—"}</td>
                            {/each}
                            <td class="py-1.5 pl-2 text-right">{formatAmount(bill.total)}</td>
                        </tr>
                    {/each}
                </tbody>
            </table>
        </div>
    {/if}
</section>
