<script lang="ts">
import type { Statement } from "#lib/api/client.js";
import {
    byClosingMonth,
    monthlyBills,
    pendingInstitutions,
    percentChange,
    totalDue,
} from "#lib/bills.js";
import Tag from "#lib/components/Tag.svelte";
import TrendChart from "#lib/components/TrendChart.svelte";
import {
    formatBytes,
    type Month,
    monthOf,
    monthsBetween,
    shiftMonth,
    shortDate,
    thisMonth,
    today,
} from "#lib/dates.js";
import {
    documentStatus,
    institution,
} from "#lib/labels.js";
import {
    approx,
    formatAmount,
    subtract,
    sum,
} from "#lib/money.js";
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

const due = $derived.by(() => {
    // A bill autopaid before its due date is not still due (ADR 0016).
    const upcoming = data.statements.filter(s => s.dueDate >= today() && !s.paidOn);
    const next = upcoming.map(s => s.dueDate).sort()[0];
    return { amount: sum(upcoming.map(s => s.totalDue)), next, count: upcoming.length };
});

// This year so far. The average leaves out this month, which is still closing.
const year = now.slice(0, 4);
const ytd = $derived.by(() => {
    const months = [...bills.keys()].filter(m => m.startsWith(year));
    const complete = months.filter(m => m < now);
    const completeTotal = sum(complete.map(m => totalDue(billOf(m))));
    return {
        total: sum(months.map(m => totalDue(billOf(m)))),
        average: complete.length > 0 ? String(Math.round(approx(completeTotal) / complete.length)) : undefined,
        months: complete.length,
    };
});

const trend = $derived(monthlyBills(data.statements, monthsBetween(shiftMonth(now, -11), now).toReversed()));

// Documents still parsing or that failed first, so they get noticed; then the newest statements.
const parsedDocuments = $derived(new Set(data.statements.map(s => s.documentId)));
const recent = $derived([
    ...data.documents.filter(d => !parsedDocuments.has(d.id) && ["pending", "failed"].includes(documentStatus(d))).map(doc => ({ doc, statement: undefined })),
    ...data.statements.map(statement => ({ doc: data.documents.find(d => d.id === statement.documentId), statement })),
].slice(0, 6));
const counts = $derived.by(() => {
    const c = { parsed: 0, pending: 0, needs_review: 0, failed: 0 };
    for (const d of data.documents) {
        const status = documentStatus(d);
        if (status in c) c[status as keyof typeof c]++;
    }
    return c;
});

function reconciliation(s: Statement): string {
    if (s.reconciliation.balanced) return "balanced";
    return `Δ ${formatAmount(subtract(s.reconciliation.stated, s.reconciliation.computed), { signed: true })} · 需確認`;
}

const names = (ids: string[]) => ids.map(i => institution(i).name).join("、");
</script>

<div class="flex flex-wrap gap-x-4 gap-y-0.5 rounded-[6px] border border-dashed border-line px-2.5 py-2 text-[12px] text-dim">
    <span><span class="text-pos">●</span> ok {counts.parsed}</span>
    <span><span class="text-accent">●</span> run {counts.pending}</span>
    <span><span class="text-neg">●</span> chk {counts.needs_review}</span>
    {#if counts.failed}<span><span class="text-neg">●</span> err {counts.failed}</span>{/if}
</div>

<div class="grid grid-cols-2 gap-2 md:grid-cols-4 md:gap-3">
    <a href={resolve(`/statements?m=${now}`)} class="panel flex flex-col gap-1 p-3 text-ink hover:border-line-strong hover:text-ink md:p-3.5">
        <span class="text-[10px] tracking-[0.12em] text-mute">THIS_BILL · {monthNumber(now)} 月帳單</span>
        <span class="text-[20px] font-bold md:text-[24px] {current.length > 0 ? 'text-ink-bright' : 'text-mute'}">{current.length > 0 ? formatAmount(totalDue(current)) : "—"}</span>
        <span class="text-[11px] text-dim">
            {#if pending.length === 0}
                {current.length > 0 ? `${current.length} 家都出帳了` : "還沒有帳單"}
            {:else}
                {current.length}/{current.length + pending.length} 家已出 · 等 {names(pending)}
            {/if}
        </span>
    </a>
    <a href={resolve(`/statements?m=${lastMonth}`)} class="panel flex flex-col gap-1 p-3 text-ink hover:border-line-strong hover:text-ink md:p-3.5">
        <span class="text-[10px] tracking-[0.12em] text-mute">LAST_BILL · {monthNumber(lastMonth)} 月帳單</span>
        <span class="text-[20px] font-bold md:text-[24px] {last.length > 0 ? 'text-ink-bright' : 'text-mute'}">{last.length > 0 ? formatAmount(totalDue(last)) : "—"}</span>
        <span class="text-[11px] {lastChange === undefined ? 'text-mute' : lastChange > 0 ? 'text-neg' : 'text-pos'}">
            {lastChange === undefined ? "沒有前一個月可比" : `${lastChange > 0 ? "+" : ""}${lastChange.toFixed(1)}% vs ${monthNumber(shiftMonth(now, -2))} 月`}
        </span>
    </a>
    <div class="panel flex flex-col gap-1 p-3 md:p-3.5">
        <span class="text-[10px] tracking-[0.12em] text-mute">DUE · 待繳</span>
        <span class="text-[20px] font-bold text-ink-bright md:text-[24px]">{formatAmount(due.amount)}</span>
        <span class="text-[11px] text-dim">{due.next ? `next ${shortDate(due.next).replace("-", "/")} · ${due.count} 張` : "沒有待繳帳單"}</span>
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
            <a href={resolve("/statements?all")} class="text-[12px]">all →</a>
        </div>
        {#each recent as { doc, statement } (statement ? `s${statement.id}` : `d${doc?.id}`)}
            <a
                href={statement ? resolve("/statements/[id]", { id: String(statement.id) }) : resolve("/statements?all")}
                class="flex min-h-11 flex-col gap-0.5 border-t border-rule py-2.5 text-ink hover:text-ink"
            >
                <span>
                    <Tag status={doc ? documentStatus(doc) : statement?.reconciliation.balanced ? "parsed" : "needs_review"} />
                    {#if statement}
                        {institution(statement.institution).name} {monthOf(statement.closingDate)}
                    {:else}
                        {doc?.filename}
                    {/if}
                </span>
                <span class="text-[12px] text-mute">
                    {#if statement}
                        {statement.transactionCount} 筆 · {reconciliation(statement)} · {shortDate(statement.dueDate).replace("-", "/")} 到期 {formatAmount(statement.totalDue)}
                    {:else if doc?.status === "pending"}
                        parsing… · {formatBytes(doc.sizeBytes)}
                    {:else}
                        {doc?.error ?? doc?.status}
                    {/if}
                </span>
            </a>
        {:else}
            <p class="m-0 border-t border-rule pt-2.5 text-mute">還沒有帳單。<a href={resolve("/upload")}>上傳第一份 →</a></p>
        {/each}
    </section>
</div>
