<script lang="ts">
import {
    byClosingMonth,
    dueBills,
    monthlyBills,
    pendingInstitutions,
    percentChange,
    totalDue,
} from "#lib/bills.js";
import StatementRow from "#lib/components/StatementRow.svelte";
import Tag from "#lib/components/Tag.svelte";
import TrendChart from "#lib/components/TrendChart.svelte";
import {
    formatBytes,
    type Month,
    monthsBetween,
    shiftMonth,
    slashDate,
    thisMonth,
    today,
} from "#lib/dates.js";
import { documentsById } from "#lib/documents.js";
import { institutionNames } from "#lib/institutions.js";
import {
    divide,
    formatAmount,
    sum,
} from "#lib/money.js";
import { statementsHref } from "#lib/urls.js";
import { resolve } from "$app/paths";
import type { PageProps } from "./$types";

let { data }: PageProps = $props();

const now = thisMonth();
const lastMonth = shiftMonth(now, -1);
const monthNumber = (m: Month) => Number(m.slice(5));

const bills = $derived(byClosingMonth(data.statements));
const billOf = (m: Month) => bills.get(m) ?? [];

const current = $derived(billOf(now));
const pending = $derived(pendingInstitutions(data.statements, now, lastMonth));
const last = $derived(billOf(lastMonth));
const lastChange = $derived(percentChange(totalDue(last), totalDue(billOf(shiftMonth(now, -2)))));

// The page is not open across days.
const due = $derived(dueBills(data.statements, today()));

// This year so far. The average leaves out this month, which is still closing.
const year = now.slice(0, 4);
const ytd = $derived.by(() => {
    const months = [...bills.keys()].filter(m => m.startsWith(year));
    const complete = months.filter(m => m < now);
    const completeTotal = sum(complete.map(m => totalDue(billOf(m))));
    return {
        total: sum(months.map(m => totalDue(billOf(m)))),
        average: complete.length > 0 ? divide(completeTotal, complete.length) : undefined,
        months: complete.length,
    };
});

const trend = $derived(monthlyBills(data.statements, monthsBetween(shiftMonth(now, -11), now).toReversed()));

// Documents still parsing or that failed first, so they get noticed; then the newest statements.
const documents = $derived(documentsById(data.documents, data.pending));
const parsedDocuments = $derived(new Set(data.statements.map(s => s.documentId)));
const waiting = $derived([...documents.values()].filter(d => !parsedDocuments.has(d.id) && (d.status === "pending" || d.status === "failed")).sort((a, b) => b.id - a.id));
const recent = $derived([
    ...waiting.map(doc => ({ doc, statement: undefined })),
    ...data.statements.map(statement => ({ doc: documents.get(statement.documentId), statement })),
].slice(0, 6));
// Of every document, not the layout's newest.
const counts = $derived(new Map(data.documentsByState.map(c => [c.state, c.count])));
</script>

<div class="flex flex-wrap gap-x-4 gap-y-0.5 rounded-[6px] border border-dashed border-line px-2.5 py-2 text-[12px] text-dim">
    <span><span class="text-pos">●</span> ok {counts.get("parsed") ?? 0}</span>
    <span><span class="text-accent">●</span> run {counts.get("pending") ?? 0}</span>
    <span><span class="text-neg">●</span> chk {counts.get("needs_review") ?? 0}</span>
    {#if counts.get("failed")}<span><span class="text-neg">●</span> err {counts.get("failed")}</span>{/if}
</div>

<div class="grid grid-cols-2 gap-2 md:grid-cols-4 md:gap-3">
    <a href={statementsHref(now)} class="panel flex flex-col gap-1 p-3 text-ink hover:border-line-strong hover:text-ink md:p-3.5">
        <span class="text-[10px] tracking-[0.12em] text-mute">THIS_BILL · {monthNumber(now)} 月帳單</span>
        <span class="text-[20px] font-bold md:text-[24px] {current.length > 0 ? 'text-ink-bright' : 'text-mute'}">{current.length > 0 ? formatAmount(totalDue(current)) : "—"}</span>
        <span class="text-[11px] text-dim">
            {#if pending.length === 0}
                {current.length > 0 ? `${current.length} 家都出帳了` : "還沒有帳單"}
            {:else}
                {current.length}/{current.length + pending.length} 家已出 · 等 {institutionNames(pending)}
            {/if}
        </span>
    </a>
    <a href={statementsHref(lastMonth)} class="panel flex flex-col gap-1 p-3 text-ink hover:border-line-strong hover:text-ink md:p-3.5">
        <span class="text-[10px] tracking-[0.12em] text-mute">LAST_BILL · {monthNumber(lastMonth)} 月帳單</span>
        <span class="text-[20px] font-bold md:text-[24px] {last.length > 0 ? 'text-ink-bright' : 'text-mute'}">{last.length > 0 ? formatAmount(totalDue(last)) : "—"}</span>
        <span class="text-[11px] {lastChange === undefined ? 'text-mute' : lastChange > 0 ? 'text-neg' : 'text-pos'}">
            {lastChange === undefined ? "沒有前一個月可比" : `${lastChange > 0 ? "+" : ""}${lastChange.toFixed(1)}% vs ${monthNumber(shiftMonth(now, -2))} 月`}
        </span>
    </a>
    <div class="panel flex flex-col gap-1 p-3 md:p-3.5">
        <span class="text-[10px] tracking-[0.12em] text-mute">DUE · 待繳</span>
        <span class="text-[20px] font-bold text-ink-bright md:text-[24px]">{formatAmount(due.amount)}</span>
        <span class="text-[11px] text-dim">{due.next ? `next ${slashDate(due.next)} · ${due.count} 張` : "沒有待繳帳單"}</span>
    </div>
    <div class="panel flex flex-col gap-1 p-3 md:p-3.5">
        <span class="text-[10px] tracking-[0.12em] text-mute">YTD · {year} 年</span>
        <span class="text-[20px] font-bold text-ink-bright md:text-[24px]">{formatAmount(ytd.total)}</span>
        <span class="text-[11px] text-dim">{ytd.average ? `月均 ${formatAmount(ytd.average)} · ${ytd.months} 個月` : "今年還沒有出完帳的月份"}</span>
    </div>
</div>

<div class="grid items-start gap-3 md:grid-cols-[1.35fr_1fr]">
    <TrendChart bills={trend} partial={pending.length > 0 ? now : undefined} />

    <section class="panel p-3 md:p-3.5">
        <div class="mb-1 flex items-baseline justify-between">
            <h2 class="label">STATEMENTS · 最近的帳單</h2>
            <a href={statementsHref("all")} class="text-[12px]">all →</a>
        </div>
        {#each recent as { doc, statement } (statement ? `s${statement.id}` : `d${doc?.id}`)}
            {#if statement}
                <StatementRow {statement} {doc} />
            {:else if doc}
                <a href={resolve("/documents/[id]", { id: String(doc.id) })} class="flex min-h-11 flex-col gap-0.5 border-t border-rule py-2.5 text-ink hover:text-ink">
                    <span><Tag status={doc.status} /> {doc.filename}</span>
                    <span class="text-[12px] text-mute">{doc.status === "pending" ? `parsing… · ${formatBytes(doc.sizeBytes)}` : (doc.error ?? doc.status)}</span>
                </a>
            {/if}
        {:else}
            <p class="m-0 border-t border-rule pt-2.5 text-mute">還沒有帳單。<a href={resolve("/documents")}>上傳第一份 →</a></p>
        {/each}
    </section>
</div>
