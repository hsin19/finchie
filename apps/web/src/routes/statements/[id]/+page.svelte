<script lang="ts">
import { lapsesSoon } from "#lib/accounts.js";
import {
    api,
    LEDGER,
    type Transaction,
    type TransactionLink,
    unwrap,
} from "#lib/api/client.js";
import FileLink from "#lib/components/FileLink.svelte";
import Tag from "#lib/components/Tag.svelte";
import {
    formatBytes,
    monthOf,
    shortDate,
    shortDateTime,
    today,
} from "#lib/dates.js";
import { openUrl } from "#lib/documents.js";
import { isPoints } from "#lib/flows.js";
import { onKeys } from "#lib/keys.svelte.js";
import {
    cardLabel,
    institution,
    kindOrder,
    kinds,
    tone,
} from "#lib/labels.js";
import {
    formatAmount,
    sign,
    subtract,
    sum,
} from "#lib/money.js";
import {
    afterNavigate,
    goto,
    invalidate,
} from "$app/navigation";
import { resolve } from "$app/paths";
import { page } from "$app/state";
import type { PageProps } from "./$types";

let { data }: PageProps = $props();

const s = $derived(data.statement);
const doc = $derived(data.document);
const bank = $derived(institution(s.institution));
const delta = $derived(subtract(s.reconciliation.stated, s.reconciliation.computed));

// Re-parsing replaces the statement's lines, and it keeps its id: wait for the document to finish,
// then reload it. The layout keeps data.documents fresh while anything is pending. Finished means a
// new parsedAt: until the ledger reloads, data.documents still shows the previous parse.
let reparsing = $state(false);
let parsedBefore: string | undefined;
let reparseMessage = $state<string>();
const live = $derived(data.documents.find(d => d.id === doc.id) ?? doc);
const status = $derived(reparsing || live.status === "pending" ? "pending" : s.reconciliation.balanced ? "parsed" : "needs_review");

// Money only: points are not added to it (ADR 0019), they have a panel of their own.
const byKind = $derived(
    kindOrder
        .map(kind => {
            const rows = data.transactions.filter(t => t.kind === kind && !isPoints(t));
            return { kind, rows: rows.length, amount: sum(rows.map(t => t.amount)) };
        })
        .filter(k => k.rows > 0),
);

// The points the bill reports, by program: its lines, the balance after it, and what will lapse, which
// is known as of the newest bill only.
const points = $derived(
    data.points.flatMap(account => {
        const lines = data.transactions.filter(t => isPoints(t) && t.accountId === account.id);
        const newest = account.balances.some(b => b.statementId === s.id);
        return [...new Set(lines.map(t => t.unit ?? ""))].map(unit => ({
            account,
            unit,
            lines: lines.filter(t => (t.unit ?? "") === unit),
            balance: account.history.find(b => b.statementId === s.id && b.unit === unit),
            expiries: newest ? account.expiries.filter(e => e.unit === unit) : [],
        }));
    }),
);

// Lines linked to these (ADR 0009) may be on other statements; ?line= picks the line to show. Scrolled
// to once the navigation is done, since SvelteKit sets the scroll position at the very end of it; Back
// keeps the position it restores.
const target = $derived(Number(page.url.searchParams.get("line")) || undefined);
afterNavigate(({ to, type }) => {
    if (type === "popstate") return;
    const line = Number(to?.url.searchParams.get("line"));
    if (line) document.getElementById(`tx-${line}`)?.scrollIntoView({ block: "center" });
});

function lineHref(link: TransactionLink) {
    return resolve(`/statements/[id]?line=${link.id}`, { id: String(link.statementId) });
}

/** " · 國泰 2026-11" for a line on another statement. */
function elsewhere(link: TransactionLink) {
    if (link.statementId === s.id) return "";
    const other = data.statements.find(st => st.id === link.statementId);
    return other ? ` · ${institution(other.institution).name} ${monthOf(other.closingDate)}` : "";
}

// The statement lists lines card by card; mark where each card's lines start.
type Line = { marker: string; } | { n: number; t: Transaction; };
const lines = $derived.by(() => {
    const out: Line[] = [];
    let card: string | undefined;
    data.transactions.forEach((t, i) => {
        if (t.cardLast4 && t.cardLast4 !== card) {
            card = t.cardLast4;
            out.push({ marker: `卡號 ${cardLabel(s.institution, card)}` });
        }
        out.push({ n: i + 1, t });
    });
    return out;
});

const log = $derived(
    [
        { at: doc.receivedAt, source: "upload", text: `#${doc.id} ${doc.filename} stored · sha256 ${doc.sha256.slice(0, 4)}…${doc.sha256.slice(-4)}`, tone: "dim" as const },
        doc.parsedAt && {
            at: doc.parsedAt,
            source: "parser",
            text: `${doc.parser ?? "?"} v${doc.parserVersion ?? "?"} → ${data.transactions.length} lines · balanced=${s.reconciliation.balanced}`,
            tone: "accent" as const,
        },
        ...doc.warnings.map(w => ({ at: doc.parsedAt ?? doc.receivedAt, source: "warning", text: w, tone: "neg" as const })),
        doc.parsedAt && !s.reconciliation.balanced && {
            at: doc.parsedAt,
            source: "store",
            text: `status needs_review · Δ ${formatAmount(delta, { signed: true })}`,
            tone: "neg" as const,
        },
    ].filter(entry => !!entry),
);

async function reparse() {
    if (reparsing) return;
    reparseMessage = undefined;
    parsedBefore = live.parsedAt;
    reparsing = true;
    try {
        unwrap(await api().POST("/api/documents/{id}/reparse", { params: { path: { id: doc.id } } }));
        await invalidate(LEDGER);
    } catch (error) {
        reparsing = false;
        reparseMessage = (error as Error).message;
    }
}

$effect(() => {
    if (!reparsing) return;
    if (live.status === "pending" || live.parsedAt === parsedBefore) return;
    reparsing = false;
    const next = data.statements.find(st => st.documentId === doc.id);
    if (next) void goto(resolve("/statements/[id]", { id: String(next.id) }), { replace: true, refreshAll: true });
    else reparseMessage = `重新解析後沒有帳單：${live.status}${live.error ? ` · ${live.error}` : ""}`;
});

onKeys({
    f: () => window.open(openUrl(doc), "_blank", "noreferrer"),
    r: () => void reparse(),
});
</script>

<div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
    <h1 class="m-0 text-[18px] font-bold">{bank.name} · {monthOf(s.closingDate)}</h1>
    <Tag {status} label />
    {#if reparsing}<span class="text-accent">re-parsing…</span>{/if}
    <span class="ml-auto flex flex-wrap gap-x-4 text-[12px]">
        <a href={resolve(`/statements?m=${monthOf(s.closingDate)}`)} class="text-dim hover:text-ink">← {monthOf(s.closingDate)} 的帳單</a>
        <!-- The lines on this statement: one statement per bank closes in a month. -->
        <a href={resolve(`/transactions?closing=${monthOf(s.closingDate)}&card=${s.institution}`)} class="text-dim hover:text-ink">這張帳單的交易 →</a>
        {#if doc.contentType === "message/rfc822"}
            <a href={resolve("/documents/[id]", { id: String(doc.id) })} class="text-dim hover:text-ink">原始信件 →</a>
        {/if}
    </span>
</div>

<div class="grid items-start gap-3 md:grid-cols-[1fr_1.3fr]">
    <div class="flex flex-col gap-3">
        <section class="panel p-3.5">
            <h2 class="label mb-2.5">META</h2>
            <dl class="m-0 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
                <dt class="text-mute">closing</dt>
                <dd class="m-0">{s.closingDate}</dd>
                <dt class="text-mute">due</dt>
                <dd class="m-0">{s.dueDate}</dd>
                <dt class="text-mute">paid</dt>
                <dd class="m-0 {s.paidOn ? 'text-pos' : 'text-mute'}">{s.paidOn ?? "還沒看到繳款（等涵蓋到期日的存款對帳單）"}</dd>
                <dt class="text-mute">total_due</dt>
                <dd class="m-0">{formatAmount(s.totalDue)} {s.currency}</dd>
                <dt class="text-mute">document</dt>
                <dd class="m-0 min-w-0 truncate">#{doc.id} {doc.filename} · {formatBytes(doc.sizeBytes)}</dd>
                <dt class="text-mute">sha256</dt>
                <dd class="m-0 text-dim" title={doc.sha256}>{doc.sha256.slice(0, 4)}…{doc.sha256.slice(-4)}</dd>
                <dt class="text-mute">parser</dt>
                <dd class="m-0 text-dim">{doc.parser ? `${doc.parser} v${doc.parserVersion}` : "—"}</dd>
                <dt class="text-mute">parsed</dt>
                <dd class="m-0 text-dim">{doc.parsedAt ? shortDateTime(doc.parsedAt) : "—"}</dd>
                <dt class="text-mute">warnings</dt>
                <dd class="m-0 {doc.warnings.length ? 'text-neg' : 'text-dim'}">{doc.warnings.length}</dd>
            </dl>
        </section>

        {#each points as program (`${program.account.id}-${program.unit}`)}
            <section class="panel p-3.5">
                <div class="mb-2 flex flex-wrap items-baseline justify-between gap-2">
                    <h2 class="label">POINTS · {program.unit}</h2>
                    <a href={resolve("/accounts/[id]", { id: String(program.account.id) })} class="text-[12px]">點數帳戶 →</a>
                </div>
                <dl class="m-0 grid grid-cols-[1fr_auto] gap-x-4 gap-y-1.5">
                    {#each program.lines as line (line.id)}
                        <dt class="text-mute">{kinds[line.kind].label}</dt>
                        <dd class="m-0 text-right">{formatAmount(line.amount, { signed: true })}</dd>
                    {/each}
                    {#if program.balance}
                        <div aria-hidden="true" class="col-span-2 border-t border-dashed border-line"></div>
                        <dt class="text-mute">這期之後的餘額</dt>
                        <dd class="m-0 text-right font-bold text-ink-bright">{formatAmount(program.balance.amount)}</dd>
                    {/if}
                    {#each program.expiries as expiry (expiry.by)}
                        <dt class={lapsesSoon(expiry.by, today()) ? "text-neg" : "text-mute"}>{expiry.by} 前到期</dt>
                        <dd class="m-0 text-right {lapsesSoon(expiry.by, today()) ? 'text-neg' : ''}">{formatAmount(expiry.amount)}</dd>
                    {/each}
                </dl>
            </section>
        {/each}
    </div>

    <section class="panel flex flex-col gap-3 p-3.5">
        <h2 class="label">RECONCILIATION · 對帳</h2>
        <div class="grid grid-cols-[auto_1fr_auto] gap-x-4 gap-y-1.5">
            <span class="text-mute">stated</span><span>{s.reconciliation.statedLabel}（帳單上印的）</span><span class="text-right">{formatAmount(s.reconciliation.stated)}</span>
            <span class="text-mute">computed</span><span>明細加總</span><span class="text-right">{formatAmount(s.reconciliation.computed)}</span>
        </div>
        <div class="grid grid-cols-[auto_1fr_auto] gap-x-4 border-t border-dashed border-line pt-2">
            {#if s.reconciliation.balanced}
                <span class="text-pos">Δ</span><span class="text-pos">兩邊一致</span><span class="text-right text-pos">0</span>
            {:else}
                <span class="text-neg">Δ</span>
                <span class="text-neg">{sign(delta) > 0 ? "帳單比明細多，可能漏抓了一列" : "明細比帳單多，可能多抓或重複了一列"}</span>
                <span class="text-right text-neg">{formatAmount(delta, { signed: true })}</span>
            {/if}
        </div>
        <table class="w-full border-collapse">
            <thead>
                <tr class="text-left text-mute">
                    <th class="py-1.5 pr-2 font-medium">KIND</th>
                    <th class="px-2 py-1.5 text-right font-medium">ROWS</th>
                    <th class="py-1.5 pl-2 text-right font-medium">SUM</th>
                </tr>
            </thead>
            <tbody>
                {#each byKind as k (k.kind)}
                    <tr class="border-t border-rule">
                        <td class="py-1.5 pr-2 {tone[kinds[k.kind].tone]}">{kinds[k.kind].short}</td>
                        <td class="px-2 py-1.5 text-right text-dim">{k.rows}</td>
                        <td class="py-1.5 pl-2 text-right">{formatAmount(k.amount)}</td>
                    </tr>
                {/each}
            </tbody>
        </table>
        <div class="flex flex-wrap gap-2">
            <FileLink {doc} class="control inline-flex items-center text-ink hover:text-ink"><span class="text-accent">[f]</span>&nbsp;開啟帳單 PDF</FileLink>
            <button type="button" class="control" disabled={reparsing} onclick={() => void reparse()}><span class="text-accent">[r]</span> 修正 parser 後重新解析</button>
        </div>
        {#if reparseMessage}<p class="m-0 text-neg">{reparseMessage}</p>{/if}
    </section>
</div>

<section class="panel px-3.5 pt-1 pb-2.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
        <h2 class="label">LINES · 帳單明細（{data.transactions.length} 列）</h2>
        <span class="text-[12px] text-mute">依原檔順序</span>
    </div>
    <div class="overflow-x-auto">
        <table class="w-full min-w-[760px] border-collapse">
            <thead>
                <tr class="text-left text-mute">
                    <th class="py-2 pr-2 font-medium">#</th>
                    <th class="px-2 py-2 font-medium">DATE</th>
                    <th class="px-2 py-2 font-medium">POSTED</th>
                    <th class="px-2 py-2 font-medium">DESCRIPTION</th>
                    <th class="px-2 py-2 font-medium">KIND</th>
                    <th class="px-2 py-2 text-right font-medium">ORIG</th>
                    <th class="py-2 pl-2 text-right font-medium">{s.currency}</th>
                </tr>
            </thead>
            <tbody>
                {#each lines as line, i (i)}
                    {#if "marker" in line}
                        <tr><td colspan="7" class="border-t border-dashed border-line py-2 text-[12px] text-mute">{line.marker}</td></tr>
                    {:else}
                        <tr id="tx-{line.t.id}" class="border-t border-rule {line.t.id === target ? 'bg-accent/10' : 'hover:bg-raised'}">
                            <td class="py-2 pr-2 text-mute">{String(line.n).padStart(2, "0")}</td>
                            <td class="px-2 py-2">{shortDate(line.t.transactionDate)}</td>
                            <td class="px-2 py-2 text-dim">{line.t.postingDate ? shortDate(line.t.postingDate) : ""}</td>
                            <td class="px-2 py-2">
                                {line.t.description}
                                {#if line.t.parent}
                                    <a href={lineHref(line.t.parent)} class="ml-2 text-[12px] whitespace-nowrap">
                                        ↑ {kinds[line.t.parent.kind].short} {shortDate(line.t.parent.transactionDate)}{elsewhere(line.t.parent)}
                                    </a>
                                {/if}
                                {#each line.t.children ?? [] as child (child.id)}
                                    <a href={lineHref(child)} class="ml-2 text-[12px] whitespace-nowrap">
                                        ↓ {kinds[child.kind].label} {formatAmount(child.amount, { signed: true })}{elsewhere(child)}
                                    </a>
                                {/each}
                            </td>
                            <td class="px-2 py-2 {tone[kinds[line.t.kind].tone]}">{kinds[line.t.kind].short}</td>
                            <td class="px-2 py-2 text-right text-dim">
                                {line.t.originalAmount && line.t.originalCurrency ? `${line.t.originalCurrency} ${formatAmount(line.t.originalAmount)}` : ""}
                            </td>
                            <td class="py-2 pl-2 text-right {line.t.kind === 'purchase' ? 'text-ink' : tone[kinds[line.t.kind].tone]}">
                                {formatAmount(line.t.amount)}{#if isPoints(line.t)}<span class="ml-1 text-mute">{line.t.unit}</span>{/if}
                            </td>
                        </tr>
                    {/if}
                {/each}
            </tbody>
        </table>
    </div>
</section>

<section class="panel p-3.5">
    <h2 class="label mb-2">LOG · 處理紀錄</h2>
    <div class="flex flex-col gap-1 text-[12px]">
        {#each log as entry, i (i)}
            <div class="grid grid-cols-[auto_auto_1fr] gap-x-3">
                <span class="text-mute">{shortDateTime(entry.at)}</span>
                <span class="w-14 text-dim">{entry.source}</span>
                <span class={tone[entry.tone]}>{entry.text}</span>
            </div>
        {/each}
    </div>
</section>
