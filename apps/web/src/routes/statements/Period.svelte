<script lang="ts">
import {
    byClosingMonth,
    fxFeeShare,
    monthlyBills,
    pendingInstitutions,
    percentChange,
    reward,
    rewardText,
    totalDue,
} from "#lib/bills.js";
import Bar from "#lib/components/Bar.svelte";
import MonthSelect from "#lib/components/MonthSelect.svelte";
import StatementRow from "#lib/components/StatementRow.svelte";
import TrendChart from "#lib/components/TrendChart.svelte";
import {
    type Month,
    monthsBetween,
    shiftMonth,
    thisMonth,
} from "#lib/dates.js";
import { documentsById } from "#lib/documents.js";
import { transactionsHref } from "#lib/filters.js";
import {
    cardLabel,
    institution,
    institutionNames,
} from "#lib/institutions.js";
import { onKeys } from "#lib/keys.svelte.js";
import {
    kindOrder,
    kinds,
} from "#lib/kinds.js";
import {
    approx,
    formatAmount,
    roundWhole,
    sign,
    subtract,
} from "#lib/money.js";
import { tone } from "#lib/tone.js";
import { statementsHref } from "#lib/urls.js";
import { goto } from "$app/navigation";
import type { PageData } from "./$types";

let { data }: { data: Extract<PageData, { view: "period"; }>; } = $props();

const previousMonth = $derived(shiftMonth(data.month, -1));

// [p] and [n] step through the months that have statements (closingMonths is newest first).
const position = $derived(data.closingMonths.indexOf(data.month));
const older = $derived(data.closingMonths[position + 1]);
const newer = $derived(position > 0 ? data.closingMonths[position - 1] : undefined);
function openMonth(m: Month | "all" | undefined) {
    if (m) void goto(statementsHref(m));
}

onKeys("statements", {
    p: () => openMonth(older),
    n: () => openMonth(newer),
});
const byMonth = $derived(byClosingMonth(data.statements));
const statements = $derived(byMonth.get(data.month) ?? []);
const documents = $derived(documentsById(data.documents, data.pending));

const due = $derived(totalDue(statements));
const dueChange = $derived(percentChange(due, totalDue(byMonth.get(previousMonth) ?? [])));
// A bank with a statement the month before but none in this one: not closed yet, or not uploaded.
const missing = $derived(pendingInstitutions(data.statements, data.month, previousMonth));
const closing = $derived(data.month === thisMonth());

const kindTotal = (kind: string) => data.current.byKind.find(k => k.kind === kind);
const spend = $derived(data.current.sum);
const spendChange = $derived(percentChange(spend, data.previous.sum));
const fxFee = $derived(kindTotal("foreign_transaction_fee")?.amount ?? "0");
const fxShare = $derived(fxFeeShare(data.current, data.overseas));
// Cash rebates and the points the bills gave, at each program's price.
const rewarded = $derived(reward(kindTotal("rebate"), data.pointLines, data.accounts));
const rewardWorth = $derived(roundWhole(rewarded.total));

const cards = $derived([...data.current.byCard].sort((a, b) => sign(subtract(b.amount, a.amount))));
const topCard = $derived(Math.max(...cards.map(c => approx(c.amount)), 1));

// This month while some bank has not closed yet: a part of a bill against a whole one says nothing.
const partial = $derived(closing && missing.length > 0);

// The year of bills up to this one.
const trend = $derived(monthlyBills(data.statements, monthsBetween(shiftMonth(data.month, -11), data.month).toReversed()));

function changeText(change: number | undefined): string {
    if (partial) return `還沒出齊，先不比`;
    return change === undefined ? "沒有上期可比" : `${change > 0 ? "+" : ""}${change.toFixed(1)}% vs 上期`;
}

function changeTone(change: number | undefined): string {
    return partial || change === undefined ? "text-mute" : change > 0 ? "text-neg" : "text-pos";
}
</script>

<div class="flex flex-wrap items-center gap-2">
    <button type="button" class="control" disabled={!older} onclick={() => openMonth(older)}><span class="text-accent">[p]</span> prev</button>
    <MonthSelect
        months={data.closingMonths}
        value={data.month}
        onchange={openMonth}
    />
    <button type="button" class="control" disabled={!newer} onclick={() => openMonth(newer)}><span class="text-accent">[n]</span> next</button>
    <span class="text-[12px] text-mute">這個月結帳的帳單</span>
    <a href={statementsHref("all")} class="ml-auto text-[12px] text-dim hover:text-ink"><span class="text-accent">[l]</span> 全部帳單 →</a>
</div>

<div class="grid grid-cols-2 gap-2 md:grid-cols-4 md:gap-3">
    <div class="panel flex flex-col gap-1 p-3 md:p-3.5">
        <div class="text-[10px] tracking-[0.12em] text-mute">DUE · 應繳</div>
        <div class="text-[20px] font-bold text-ink-bright md:text-[24px]">{formatAmount(due)}</div>
        <div class="text-[11px] {changeTone(dueChange)}">{statements.length} 張 · {changeText(dueChange)}</div>
    </div>
    <div class="panel flex flex-col gap-1 p-3 md:p-3.5">
        <div class="text-[10px] tracking-[0.12em] text-mute" title="消費加上它的海外手續費，扣掉刷退與點數折抵">SPEND · 淨消費</div>
        <div class="text-[20px] font-bold text-ink-bright md:text-[24px]">{formatAmount(spend)}</div>
        <div class="text-[11px] {changeTone(spendChange)}">{changeText(spendChange)}</div>
    </div>
    <div class="panel flex flex-col gap-1 p-3 md:p-3.5">
        <div class="text-[10px] tracking-[0.12em] text-mute">FX_FEE · 海外手續費</div>
        <div class="text-[20px] font-bold md:text-[24px] {sign(fxFee) > 0 ? 'text-neg' : 'text-mute'}">{formatAmount(fxFee)}</div>
        <div class="text-[11px] text-dim">{fxShare === undefined ? "沒有海外消費" : `${fxShare.percent.toFixed(1)}% of overseas ${formatAmount(fxShare.of)}`}</div>
    </div>
    <div class="panel flex flex-col gap-1 p-3 md:p-3.5">
        <div class="text-[10px] tracking-[0.12em] text-mute" title="帳單入帳的現金回饋，加上這期帳單印的獲得點數（含活動與調整，不含使用）；點數照它目前的價格換成 NT$">REWARD · 回饋</div>
        <div class="text-[20px] font-bold md:text-[24px] {sign(rewardWorth) > 0 ? 'text-pos' : 'text-mute'}">
            {rewarded.points.some(p => p.worth !== undefined) ? "≈ " : ""}{formatAmount(rewardWorth)}
        </div>
        <div class="text-[11px] text-dim">{rewardText(rewarded)}</div>
    </div>
</div>

<div class="grid items-start gap-3 md:grid-cols-[1.35fr_1fr]">
    <div class="flex flex-col gap-3">
        <section class="panel p-3 md:p-3.5">
            <h2 class="label mb-3">BY_CARD · 各卡消費</h2>
            {#if cards.length === 0}
                <p class="m-0 text-mute">這期沒有消費。</p>
            {/if}
            <div class="flex flex-col gap-2.5">
                {#each cards as card (`${card.institution}-${card.cardLast4}`)}
                    <a
                        class="flex flex-col gap-1 text-ink hover:text-ink"
                        href={transactionsHref({ closing: data.month, kinds: ["purchase"], institution: card.institution, card: card.cardLast4 })}
                    >
                        <span class="flex justify-between gap-2">
                            <span>{cardLabel(card.institution, card.cardLast4)} <span class="text-mute">{institution(card.institution).name}</span></span>
                            <span>{formatAmount(card.amount)} <span class="text-mute">{approx(spend) > 0 ? Math.round((approx(card.amount) / approx(spend)) * 100) : 0}%</span></span>
                        </span>
                        <Bar percent={(approx(card.amount) / topCard) * 100} />
                    </a>
                {/each}
            </div>
        </section>

        <section class="panel p-3 md:p-3.5">
            <h2 class="label mb-1">BY_KIND · 種類</h2>
            <table class="w-full border-collapse">
                <thead>
                    <tr class="text-left text-mute">
                        <th class="py-2 pr-2 font-medium">KIND</th>
                        <th class="py-2 pr-2 text-right font-medium">ROWS</th>
                        <th class="py-2 text-right font-medium">{data.settings.baseCurrency}</th>
                    </tr>
                </thead>
                <tbody>
                    {#each kindOrder.map(kindTotal).filter(k => k !== undefined) as k (k.kind)}
                        <tr class="border-t border-rule">
                            <td class="py-2 pr-2">
                                <a class="text-dim hover:text-ink" href={transactionsHref({ closing: data.month, kinds: [k.kind] })}>{kinds[k.kind].label}</a>
                            </td>
                            <td class="py-2 pr-2 text-right text-dim">{k.count}</td>
                            <td class="py-2 text-right {tone[kinds[k.kind].tone]}">{formatAmount(k.amount)}</td>
                        </tr>
                    {:else}
                        <tr class="border-t border-rule"><td colspan="3" class="py-2 text-mute">沒有資料</td></tr>
                    {/each}
                </tbody>
            </table>
        </section>
    </div>

    <section class="panel p-3 md:p-3.5">
        <h2 class="label mb-1">STATEMENTS · {data.month} 的帳單</h2>
        {#each statements as statement (statement.id)}
            <StatementRow {statement} doc={documents.get(statement.documentId)} closing="day" />
        {:else}
            <p class="m-0 border-t border-rule py-2.5 text-mute">這個月沒有結帳的帳單。</p>
        {/each}
        {#if missing.length > 0}
            <p class="m-0 border-t border-rule py-2.5 text-[12px] text-mute">
                {closing ? `等 ${institutionNames(missing)} 出帳` : `沒有 ${institutionNames(missing)} 的帳單（上個月有）`}
            </p>
        {/if}
    </section>
</div>

<TrendChart bills={trend} partial={partial ? data.month : undefined} />
