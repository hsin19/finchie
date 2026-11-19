<script lang="ts">
import type { InvestmentPoint } from "#lib/api/client.js";
import ViewSwitch from "#lib/components/ViewSwitch.svelte";
import {
    investmentLines,
    investmentX,
} from "#lib/investments.js";
import {
    formatAmount,
    roundWhole,
} from "#lib/money.js";
import { signTone } from "#lib/tone.js";

/**
 * What securities and exchange accounts are worth against what went into them, by day, as two lines or
 * as a table: all of them, or one of a kind on its page. What went into an exchange is every deposit and
 * withdrawal it reported; into a securities account, its worth on its first day here, then its trades'
 * money. A day before every account had reported is an estimate.
 */
let { points, kind }: {
    points: InvestmentPoint[];
    /** One account's, of this kind; all of them when absent. */
    kind?: "securities" | "exchange";
} = $props();

let view = $state<"chart" | "table">("chart");
let active = $state<number>();
const lines = $derived(investmentLines(points));
const first = $derived(points[0]);
const last = $derived(points.at(-1));

const percent = (p: InvestmentPoint) => (p.returnPercent === undefined ? "—" : `${formatAmount(p.returnPercent, { signed: true })}%`);
</script>

<section class="panel flex flex-col gap-3 p-3 md:p-3.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2">
        <h2 class="label">INVEST · 投資</h2>
        {#if points.length > 0}
            <ViewSwitch bind:view />
        {/if}
    </div>

    {#if !first || !last}
        <p class="m-0 text-mute">證券和交易所還沒有市值，有了之後從那天開始算。</p>
    {:else}
        <div class="flex flex-wrap items-baseline gap-x-8 gap-y-2">
            <div>
                <div class="label">WORTH · 市值</div>
                <div class="text-[20px] font-bold text-ink-bright">≈ NT${formatAmount(roundWhole(last.worth))}</div>
            </div>
            <div>
                <div class="label">IN · 淨投入</div>
                <div>NT${formatAmount(roundWhole(last.invested))}</div>
            </div>
            <div>
                <div class="label">GAIN · 損益</div>
                <div class={signTone(last.gain)}>{formatAmount(roundWhole(last.gain), { signed: true })}</div>
            </div>
            <div>
                <div class="label">RETURN · 報酬率</div>
                <div class={signTone(last.gain)}>{percent(last)}</div>
            </div>
        </div>

        {#if view === "chart"}
            <div class="relative">
                {#if active !== undefined}<span class="absolute inset-y-0 w-px bg-line-strong" style:left="{investmentX(active, points.length)}%"></span>{/if}
                <svg viewBox="0 0 100 100" preserveAspectRatio="none" class="relative block h-[160px] w-full overflow-visible" role="img" aria-label="市值與淨投入，{first.on} 到 {last.on}">
                    <polyline points={lines.invested} fill="none" class="stroke-dim" stroke-width="1.5" stroke-dasharray="4 3" vector-effect="non-scaling-stroke" />
                    <polyline points={lines.worth} fill="none" class="stroke-accent" stroke-width="2" vector-effect="non-scaling-stroke" />
                </svg>
                <div class="absolute inset-0 flex">
                    {#each points as point, i (point.on)}
                        <div
                            role="presentation"
                            class="h-full flex-1"
                            onmouseenter={() => (active = i)}
                            onmouseleave={() => (active = undefined)}
                        >
                        </div>
                    {/each}
                </div>
                {#if active !== undefined}
                    {@const point = points[active]}
                    {@const right = active < points.length / 2}
                    <div
                        role="tooltip"
                        class="pointer-events-none absolute top-0 z-10 min-w-44 rounded-[6px] border border-line-strong bg-ground px-3 py-2 text-[12px] shadow-lg {right ? '' : '-translate-x-full'}"
                        style:left="calc({investmentX(active, points.length)}% {right ? '+' : '-'} 8px)"
                    >
                        <div class="mb-1 text-ink-bright">{point.on}</div>
                        <div class="flex justify-between gap-4 text-dim"><span>市值</span><span class="tabular-nums">{formatAmount(point.worth)}</span></div>
                        <div class="flex justify-between gap-4 text-dim"><span>淨投入</span><span class="tabular-nums">{formatAmount(point.invested)}</span></div>
                        <div class="flex justify-between gap-4 {signTone(point.gain)}"><span>損益</span><span class="tabular-nums">{formatAmount(point.gain, { signed: true })}</span></div>
                        <div class="flex justify-between gap-4 {signTone(point.gain)}"><span>報酬率</span><span class="tabular-nums">{percent(point)}</span></div>
                        {#if point.unvalued > 0}<div class="text-mute">{point.unvalued} 個帳戶沒有價格，兩邊都不計</div>{/if}
                        {#if point.unpriced > 0}<div class="text-mute">{point.unpriced} 筆轉入轉出沒有價格，不計</div>{/if}
                        {#if !point.complete}<div class="mt-1 text-accent">估計：還有帳戶沒回報到這天</div>{/if}
                    </div>
                {/if}
            </div>
            <div class="flex flex-wrap justify-between gap-x-4 gap-y-1 text-[12px] text-mute">
                <span>
                    <span class="mr-1.5 inline-block h-0.5 w-3 bg-accent align-middle"></span>市值
                    <span class="mr-1.5 ml-3 inline-block w-3 border-t-2 border-dashed border-dim align-middle"></span>淨投入
                </span>
                <span class="tabular-nums">{first.on} – {last.on}</span>
            </div>
        {:else}
            <div class="max-h-80 overflow-auto">
                <table class="w-full border-collapse text-[12px] tabular-nums">
                    <thead>
                        <tr class="text-left text-mute">
                            <th class="py-1.5 pr-2 font-medium">DAY</th>
                            <th class="px-2 py-1.5 text-right font-medium">WORTH</th>
                            <th class="px-2 py-1.5 text-right font-medium">IN</th>
                            <th class="py-1.5 pl-2 text-right font-medium">GAIN</th>
                        </tr>
                    </thead>
                    <tbody>
                        {#each points.toReversed() as point (point.on)}
                            <tr class="border-t border-rule">
                                <td class="py-1.5 pr-2">{point.on}{point.complete ? "" : " · 估計"}</td>
                                <td class="px-2 py-1.5 text-right text-dim">{formatAmount(point.worth)}</td>
                                <td class="px-2 py-1.5 text-right text-dim">{formatAmount(point.invested)}</td>
                                <td class="py-1.5 pl-2 text-right {signTone(point.gain)}">{formatAmount(point.gain, { signed: true })} · {percent(point)}</td>
                            </tr>
                        {/each}
                    </tbody>
                </table>
            </div>
        {/if}
        <!-- What went into a brokerage before Finchie saw it is unknown, so its first day's worth counts as put in. -->
        <div class="text-[12px] text-mute">
            {#if kind === "exchange"}
                算進讀到的每一筆入金、出金、充幣、提幣（第一次讀時回溯兩年）；Finchie 開始有價格之前的外幣和幣，用最早的價格估。
            {:else if kind === "securities"}
                從 {first.on} 起算：那天的市值當作本金，之後加減買賣成交。現金股利進交割戶，看不到，不計。
            {:else}
                交易所算進讀到的每一筆出入金（第一次讀時回溯兩年），Finchie 開始有價格之前的外幣和幣用最早的價格估；證券從第一天有收盤價起，把那天的市值當作本金，之後加減買賣成交，現金股利看不到，不計。
            {/if}
        </div>
    {/if}
</section>
