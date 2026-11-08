<script lang="ts">
import type { Statement } from "#lib/api/client.js";
import {
    byClosingMonth,
    fxFeeShare,
    pendingInstitutions,
    percentChange,
    totalDue,
} from "#lib/bills.js";
import Bar from "#lib/components/Bar.svelte";
import MonthSelect from "#lib/components/MonthSelect.svelte";
import Tag from "#lib/components/Tag.svelte";
import {
    shiftMonth,
    shortDate,
    thisMonth,
} from "#lib/dates.js";
import { onKeys } from "#lib/keys.svelte.js";
import {
    cardLabel,
    documentStatus,
    institution,
    kindOrder,
    kinds,
    tone,
} from "#lib/labels.js";
import {
    approx,
    formatAmount,
    negate,
    subtract,
} from "#lib/money.js";
import { goto } from "$app/navigation";
import { resolve } from "$app/paths";
import type { PageData } from "./$types";

let { data }: { data: Extract<PageData, { view: "period"; }>; } = $props();

const previousMonth = $derived(shiftMonth(data.month, -1));

// [p] and [n] step through the months that have statements (closingMonths is newest first).
const position = $derived(data.closingMonths.indexOf(data.month));
const older = $derived(data.closingMonths[position + 1]);
const newer = $derived(position > 0 ? data.closingMonths[position - 1] : undefined);
function openMonth(m: string | undefined) {
    if (m) void goto(resolve(`/statements?m=${m}`));
}

onKeys({
    p: () => openMonth(older),
    n: () => openMonth(newer),
});
const byMonth = $derived(byClosingMonth(data.statements));
const statements = $derived(byMonth.get(data.month) ?? []);
const documents = $derived(new Map(data.documents.map(d => [d.id, d])));

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
const rebate = $derived(kindTotal("rebate"));

const cards = $derived([...data.current.byCard].sort((a, b) => approx(b.amount) - approx(a.amount)));
const topCard = $derived(Math.max(...cards.map(c => approx(c.amount)), 1));

const parsedDocuments = $derived(new Set(data.statements.map(s => s.documentId)));
const unparsed = $derived(data.documents.filter(d => !parsedDocuments.has(d.id) && ["pending", "failed"].includes(documentStatus(d))).length);

function changeText(change: number | undefined): string {
    return change === undefined ? "沒有上期可比" : `${change > 0 ? "+" : ""}${change.toFixed(1)}% vs 上期`;
}

function changeTone(change: number | undefined): string {
    return change === undefined ? "text-mute" : change > 0 ? "text-neg" : "text-pos";
}

function reconciliation(s: Statement): string {
    if (s.reconciliation.balanced) return "balanced";
    return `Δ ${formatAmount(subtract(s.reconciliation.stated, s.reconciliation.computed), { signed: true })} · 需確認`;
}

const names = (ids: string[]) => ids.map(i => institution(i).name).join("、");
</script>

<div class="flex flex-wrap items-center gap-2">
    <button type="button" class="control" disabled={!older} onclick={() => openMonth(older)}><span class="text-accent">[p]</span> prev</button>
    <MonthSelect
        months={data.closingMonths}
        value={data.month}
        onchange={m => void goto(m === "all" ? resolve("/statements?all") : resolve(`/statements?m=${m}`))}
    />
    <button type="button" class="control" disabled={!newer} onclick={() => openMonth(newer)}><span class="text-accent">[n]</span> next</button>
    <span class="text-[12px] text-mute">這個月結帳的帳單</span>
    <a href={resolve("/statements?all")} class="ml-auto text-[12px] text-dim hover:text-ink"><span class="text-accent">[l]</span> 全部帳單 →</a>
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
        <div class="text-[20px] font-bold text-neg md:text-[24px]">{formatAmount(fxFee)}</div>
        <div class="text-[11px] text-dim">{fxShare === undefined ? "沒有海外消費" : `${fxShare.percent.toFixed(1)}% of overseas ${formatAmount(fxShare.of)}`}</div>
    </div>
    <div class="panel flex flex-col gap-1 p-3 md:p-3.5">
        <div class="text-[10px] tracking-[0.12em] text-mute">REWARD · 回饋</div>
        <div class="text-[20px] font-bold text-pos md:text-[24px]">{formatAmount(rebate ? negate(rebate.amount) : "0")}</div>
        <div class="text-[11px] text-dim">{rebate ? `${rebate.count} 筆入帳回饋` : "這期沒有回饋入帳"}</div>
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
                        href={resolve(`/transactions?closing=${data.month}&kind=purchase&card=${card.institution}${card.cardLast4 ? `:${card.cardLast4}` : ""}`)}
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
                        <th class="py-2 text-right font-medium">TWD</th>
                    </tr>
                </thead>
                <tbody>
                    {#each kindOrder.map(kindTotal).filter(k => k !== undefined) as k (k.kind)}
                        <tr class="border-t border-rule">
                            <td class="py-2 pr-2">
                                <a class="text-dim hover:text-ink" href={resolve(`/transactions?closing=${data.month}&kind=${k.kind}`)}>{kinds[k.kind].short}
                                    <span class="text-mute">{kinds[k.kind].label}</span></a>
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
            {@const doc = documents.get(statement.documentId)}
            <a href={resolve("/statements/[id]", { id: String(statement.id) })} class="flex min-h-11 flex-col gap-0.5 border-t border-rule py-2.5 text-ink hover:text-ink">
                <span>
                    <Tag status={doc ? documentStatus(doc) : statement.reconciliation.balanced ? "parsed" : "needs_review"} />
                    {institution(statement.institution).name} <span class="text-mute">{shortDate(statement.closingDate).replace("-", "/")} 結帳</span>
                </span>
                <span class="text-[12px] text-mute">
                    {statement.transactionCount} 筆 · {reconciliation(statement)} · {shortDate(statement.dueDate).replace("-", "/")} 到期 {formatAmount(statement.totalDue)}{statement.paidOn ? ` · 已繳 ${shortDate(statement.paidOn).replace("-", "/")}` : ""}
                </span>
            </a>
        {:else}
            <p class="m-0 border-t border-rule py-2.5 text-mute">這個月沒有結帳的帳單。</p>
        {/each}
        {#if missing.length > 0}
            <p class="m-0 border-t border-rule py-2.5 text-[12px] text-mute">
                {closing ? `等 ${names(missing)} 出帳` : `沒有 ${names(missing)} 的帳單（上個月有）`}
            </p>
        {/if}
        {#if unparsed > 0}
            <p class="m-0 border-t border-rule py-2.5 text-[12px] text-mute">
                {unparsed} 份文件還沒產生帳單 · <a href={resolve("/statements?all")}>看全部 →</a>
            </p>
        {/if}
    </section>
</div>
