<script lang="ts">
import type { MonthFlow } from "#lib/cashflow.js";
import { transactionsHref } from "#lib/filters.js";
import {
    approx,
    formatAmount,
    sign,
} from "#lib/money.js";

let { flows, year, others }: {
    /** Oldest first. */
    flows: MonthFlow[];
    /** This year's months so far, added up. */
    year: { name: string; received: string; spent: string; net: string; months: number; };
    /** Units other than the base one, over the months: not drawn, only named. */
    others: { unit: string; received: string; spent: string; }[];
} = $props();

let view = $state<"chart" | "table">("chart");
let active = $state<number>();

// What came in rises from the zero line and what went out hangs below it, on one scale: about three
// round steps (1, 2, or 5 × 10ⁿ) on the longer side.
const scale = $derived.by(() => {
    const up = Math.max(0, ...flows.map(f => approx(f.received)));
    const down = Math.max(0, ...flows.map(f => -approx(f.spent)));
    const longest = Math.max(up, down);
    if (longest <= 0) return { ticks: [0], top: 1, bottom: 0 };
    const power = 10 ** Math.floor(Math.log10(longest / 3));
    const step = [1, 2, 5].map(f => f * power).find(s => s >= longest / 3) ?? 10 * power;
    const top = Math.ceil(up / step) * step;
    const bottom = Math.ceil(down / step) * step;
    const ticks = Array.from({ length: (top + bottom) / step + 1 }, (_, i) => i * step - bottom);
    return { ticks, top, bottom };
});
const span = $derived(scale.top + scale.bottom || 1);
/** Percent of the plot, from its bottom. */
const at = (value: number) => ((value + scale.bottom) / span) * 100;
const zero = $derived(at(0));

function compact(n: number): string {
    return Math.abs(n) >= 1000 ? `${n / 1000}K` : String(n);
}

function describe(f: MonthFlow): string {
    return `${f.month} 收入 ${formatAmount(f.received)}、支出 ${formatAmount(f.spent)}、結餘 ${formatAmount(f.net, { signed: true })}${f.complete ? "" : "（還沒到齊）"}`;
}

const tone = (value: string) => (sign(value) < 0 ? "text-neg" : "text-pos");

const GUTTER = 40;
const views = ["chart", "table"] as const;
</script>

<section class="panel flex flex-col gap-3 p-3 md:p-3.5">
    <div class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-2">
        <h2 class="label">CASHFLOW · 一年收支</h2>
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

    <!-- The year so far: what the owner most wants of it. -->
    <dl class="m-0 flex flex-wrap gap-x-8 gap-y-2">
        <div>
            <dt class="label">{year.name} 年結餘</dt>
            <dd class="m-0 text-[20px] font-bold {sign(year.net) < 0 ? 'text-neg' : 'text-ink-bright'}">{formatAmount(year.net, { signed: true })}</dd>
        </div>
        <div>
            <dt class="label">收入</dt>
            <dd class="m-0">{formatAmount(year.received)}</dd>
        </div>
        <div>
            <dt class="label">支出</dt>
            <dd class="m-0">{formatAmount(year.spent)}</dd>
        </div>
        <div class="self-end text-[12px] text-mute">{year.months} 個月</div>
    </dl>

    {#if view === "chart"}
        <div class="relative">
            <div class="relative h-[200px]">
                {#each scale.ticks as tick (tick)}
                    {@const bottom = `${at(tick)}%`}
                    <div class="absolute right-0 border-t {tick === 0 ? 'border-line-strong' : 'border-rule'}" style:left="{GUTTER}px" style:bottom></div>
                    <span class="absolute left-0 translate-y-1/2 pr-1.5 text-right text-[10px] text-mute tabular-nums" style:width="{GUTTER}px" style:bottom>{compact(tick)}</span>
                {/each}
                <div class="absolute inset-y-0 right-0 flex" style:left="{GUTTER}px">
                    {#each flows as flow, i (flow.month)}
                        {@const received = approx(flow.received)}
                        {@const spent = -approx(flow.spent)}
                        <a
                            href={transactionsHref({ period: flow.month, hideTransfers: true })}
                            aria-label={describe(flow)}
                            class="relative h-full flex-1 rounded-[4px] hover:bg-raised focus-visible:bg-raised"
                            onmouseenter={() => (active = i)}
                            onmouseleave={() => (active = undefined)}
                            onfocus={() => (active = i)}
                            onblur={() => (active = undefined)}
                        >
                            <span class="absolute inset-x-0 top-0 bottom-0 mx-auto w-[60%] max-w-6 {flow.complete ? '' : 'opacity-50'}">
                                <!-- In from the zero line up, out from it down; a hairline of the panel keeps them apart. -->
                                <span class="absolute inset-x-0 rounded-t-[4px] bg-pos" style:bottom="calc({zero}% + 1px)" style:height="{(received / span) * 100}%"></span>
                                <span class="absolute inset-x-0 rounded-b-[4px] bg-neg" style:top="calc({100 - zero}% + 1px)" style:height="{(spent / span) * 100}%"></span>
                                <!-- What was left: the month's net. -->
                                <span class="absolute -inset-x-1 h-[2px] rounded-full bg-ink-bright" style:bottom="calc({at(approx(flow.net))}% - 1px)"></span>
                            </span>
                        </a>
                    {/each}
                </div>
            </div>
            <div class="mt-1 flex text-[10px] text-mute tabular-nums" style:padding-left="{GUTTER}px">
                {#each flows as flow (flow.month)}
                    <span class="flex-1 text-center {flow.complete ? '' : 'text-accent'}">{flow.month.slice(5)}</span>
                {/each}
            </div>

            {#if active !== undefined}
                {@const flow = flows[active]}
                {@const right = active < flows.length / 2}
                <!-- Beside the column, not over it: right of it in the left half, left of it in the right half. -->
                <div
                    role="tooltip"
                    class="pointer-events-none absolute top-0 z-10 min-w-44 rounded-[6px] border border-line-strong bg-ground px-3 py-2 text-[12px] shadow-lg {right ? '' : '-translate-x-full'}"
                    style:left="calc({GUTTER}px + (100% - {GUTTER}px) * {(active + (right ? 1 : 0)) / flows.length} {right ? '+' : '-'} 4px)"
                >
                    <div class="mb-1 text-ink-bright">{flow.month}</div>
                    <div class="flex justify-between gap-4 text-dim"><span><span class="mr-1.5 inline-block size-2 rounded-[2px] bg-pos"></span>收入</span><span class="tabular-nums">{formatAmount(flow.received)}</span></div>
                    <div class="flex justify-between gap-4 text-dim"><span><span class="mr-1.5 inline-block size-2 rounded-[2px] bg-neg"></span>支出</span><span class="tabular-nums">{formatAmount(flow.spent)}</span></div>
                    <div class="flex justify-between gap-4 text-ink"><span><span class="mr-1.5 inline-block h-[2px] w-2 bg-ink-bright align-middle"></span>結餘</span><span class="tabular-nums">{formatAmount(flow.net, { signed: true })}</span></div>
                    {#if !flow.complete}<div class="mt-1 text-mute">帳單或對帳單還沒到齊</div>{/if}
                </div>
            {/if}
        </div>

        <div class="flex flex-wrap gap-x-4 gap-y-1 text-[12px] text-dim">
            <span><span class="mr-1.5 inline-block size-2 rounded-[2px] bg-pos"></span>收入</span>
            <span><span class="mr-1.5 inline-block size-2 rounded-[2px] bg-neg"></span>支出</span>
            <span><span class="mr-1.5 inline-block h-[2px] w-2.5 bg-ink-bright align-middle"></span>結餘</span>
            <span class="text-mute"><span class="text-accent">淡色</span>：帳單或對帳單還沒到齊</span>
        </div>
    {:else}
        <div class="overflow-x-auto">
            <table class="w-full border-collapse text-[12px] tabular-nums">
                <thead>
                    <tr class="text-left text-mute">
                        <th class="py-1.5 pr-2 font-medium">MONTH</th>
                        <th class="px-2 py-1.5 text-right font-medium">收入</th>
                        <th class="px-2 py-1.5 text-right font-medium">支出</th>
                        <th class="py-1.5 pl-2 text-right font-medium">結餘</th>
                    </tr>
                </thead>
                <tbody>
                    {#each flows.toReversed() as flow (flow.month)}
                        <tr class="border-t border-rule">
                            <td class="py-1.5 pr-2">
                                <a href={transactionsHref({ period: flow.month, hideTransfers: true })}>{flow.month}</a>
                                {#if !flow.complete}<span class="text-mute">（未到齊）</span>{/if}
                            </td>
                            <td class="px-2 py-1.5 text-right text-dim">{formatAmount(flow.received)}</td>
                            <td class="px-2 py-1.5 text-right text-dim">{formatAmount(flow.spent)}</td>
                            <td class="py-1.5 pl-2 text-right {tone(flow.net)}">{formatAmount(flow.net, { signed: true })}</td>
                        </tr>
                    {/each}
                </tbody>
            </table>
        </div>
    {/if}

    <p class="m-0 text-[12px] text-mute">
        所有帳戶的進出：不算自己帳戶之間的轉帳、繳卡費、轉進交易所的錢、證券買賣與點數。{#each others as other (other.unit)}另有 {other.unit} 收入 {formatAmount(other.received)}、支出 {formatAmount(other.spent)}。{/each}
    </p>
</section>
