<script lang="ts">
import type { NetWorthPoint } from "#lib/api/client.js";
import { formatAmount } from "#lib/money.js";
import { trendColumns } from "#lib/networth.js";

/**
 * Net worth at each month end and today (ADR 0023), as columns around zero, or as a table. A point
 * before every account had reported is an estimate: drawn fainter, and said so.
 */
let { points }: { points: NetWorthPoint[]; } = $props();

let view = $state<"chart" | "table">("chart");
let active = $state<number>();
const chart = $derived(trendColumns(points));
const views = ["chart", "table"] as const;

/** "10" for a month end, "11/11" for a day within a month (today). */
function label(on: string, i: number): string {
    const last = i === points.length - 1;
    return last ? on.slice(5).replace("-", "/") : on.slice(5, 7);
}
</script>

<section class="panel flex flex-col gap-3 p-3 md:p-3.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2">
        <h2 class="label">TREND · 淨額</h2>
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

    {#if points.length === 0}
        <p class="m-0 text-mute">還沒有可以算的日子。</p>
    {:else if view === "chart"}
        <div class="relative">
            <div class="relative h-[160px]">
                <div class="absolute inset-x-0 border-t border-line-strong" style:bottom="{chart.zero}%"></div>
                <div class="absolute inset-0 flex">
                    {#each chart.columns as column, i (column.point.on)}
                        <div
                            role="img"
                            aria-label="{column.point.on} 淨額 {formatAmount(column.point.net)}{column.point.complete ? '' : '（估計）'}"
                            class="relative h-full flex-1 rounded-t-[4px] hover:bg-raised"
                            onmouseenter={() => (active = i)}
                            onmouseleave={() => (active = undefined)}
                        >
                            <span
                                class="absolute left-1/2 w-[60%] max-w-6 -translate-x-1/2 rounded-[2px] {column.negative ? 'bg-neg' : 'bg-accent'} {column.point.complete ? '' : 'opacity-40'}"
                                style:bottom="{column.bottom}%"
                                style:height="{Math.max(column.height, 0.5)}%"
                            ></span>
                        </div>
                    {/each}
                </div>
            </div>
            <div class="mt-1 flex text-[10px] text-mute tabular-nums">
                {#each points as point, i (point.on)}
                    <span class="flex-1 text-center {point.complete ? '' : 'text-accent'}">{label(point.on, i)}</span>
                {/each}
            </div>
            {#if active !== undefined}
                {@const point = points[active]}
                {@const right = active < points.length / 2}
                <div
                    role="tooltip"
                    class="pointer-events-none absolute top-0 z-10 min-w-44 rounded-[6px] border border-line-strong bg-ground px-3 py-2 text-[12px] shadow-lg {right ? '' : '-translate-x-full'}"
                    style:left="calc(100% * {(active + (right ? 1 : 0)) / points.length} {right ? '+' : '-'} 4px)"
                >
                    <div class="mb-1 flex justify-between gap-4"><span class="text-ink-bright">{point.on}</span><span class="text-ink-bright">{formatAmount(point.net)}</span></div>
                    <div class="flex justify-between gap-4 text-dim"><span>資產</span><span class="tabular-nums">{formatAmount(point.assets)}</span></div>
                    <div class="flex justify-between gap-4 text-dim"><span>負債</span><span class="tabular-nums">{formatAmount(point.liabilities)}</span></div>
                    {#if point.unvalued > 0}<div class="text-mute">{point.unvalued} 個帳戶沒有價格，不計</div>{/if}
                    {#if !point.complete}<div class="mt-1 text-accent">估計：還有帳戶沒回報到這天</div>{/if}
                </div>
            {/if}
        </div>
        <div class="text-[12px] text-mute">淡色的是估計：那天之後還有帳戶沒回報（ADR 0023）</div>
    {:else}
        <div class="overflow-x-auto">
            <table class="w-full border-collapse text-[12px] tabular-nums">
                <thead>
                    <tr class="text-left text-mute">
                        <th class="py-1.5 pr-2 font-medium">DAY</th>
                        <th class="px-2 py-1.5 text-right font-medium">ASSETS</th>
                        <th class="px-2 py-1.5 text-right font-medium">LIABILITIES</th>
                        <th class="py-1.5 pl-2 text-right font-medium">NET</th>
                    </tr>
                </thead>
                <tbody>
                    {#each points.toReversed() as point (point.on)}
                        <tr class="border-t border-rule">
                            <td class="py-1.5 pr-2">{point.on}{point.complete ? "" : " · 估計"}</td>
                            <td class="px-2 py-1.5 text-right text-dim">{formatAmount(point.assets)}</td>
                            <td class="px-2 py-1.5 text-right text-dim">{formatAmount(point.liabilities)}</td>
                            <td class="py-1.5 pl-2 text-right {point.complete ? '' : 'text-mute'}">{formatAmount(point.net)}</td>
                        </tr>
                    {/each}
                </tbody>
            </table>
        </div>
    {/if}
</section>
