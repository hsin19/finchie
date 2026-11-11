<script lang="ts">
import { lapsesSoon } from "#lib/accounts.js";
import type { Transaction } from "#lib/api/client.js";
import FileLink from "#lib/components/FileLink.svelte";
import Tag from "#lib/components/Tag.svelte";
import {
    formatBytes,
    monthOf,
    shortDate,
    shortDateTime,
    today,
} from "#lib/dates.js";
import {
    openUrl,
    outcome,
} from "#lib/documents.js";
import { transactionsHref } from "#lib/filters.js";
import { isPoints } from "#lib/flows.js";
import {
    cardLabel,
    institution,
} from "#lib/institutions.js";
import { onKeys } from "#lib/keys.svelte.js";
import {
    kindOrder,
    kinds,
} from "#lib/kinds.js";
import {
    formatAmount,
    sum,
} from "#lib/money.js";
import { Reparse } from "#lib/reparse.svelte.js";
import {
    billPoints,
    checkText,
    failingChecks,
    statementStatus,
} from "#lib/statements.js";
import { tone } from "#lib/tone.js";
import {
    readLine,
    statementHref,
    statementsHref,
} from "#lib/urls.js";
import { afterNavigate } from "$app/navigation";
import { resolve } from "$app/paths";
import { page } from "$app/state";
import type { PageProps } from "./$types";
import Checks from "./Checks.svelte";

let { data }: PageProps = $props();

const s = $derived(data.statement);
// Loaded with the page, which reloads with the ledger: pending while a re-parse runs, then its result.
const doc = $derived(data.document);
const bank = $derived(institution(s.institution));
const month = $derived(monthOf(s.closingDate));

// Once done, the ledger reloads and so does the statement, with new lines (they get new ids).
const reparse = new Reparse(() => doc.id);
const parsing = $derived(reparse.requesting || doc.status === "pending");
const status = $derived(statementStatus(s, doc, { reparsing: reparse.requesting }));
// A re-parse that gave no bill leaves this one as it was: say what it gave.
const reparsed = $derived(reparse.askedAt && !parsing ? outcome(doc, s) : undefined);

// Money only: points are not added to it (ADR 0019), they have a panel of their own.
const byKind = $derived(
    kindOrder
        .map(kind => {
            const rows = data.transactions.filter(t => t.kind === kind && !isPoints(t));
            return { kind, rows: rows.length, amount: sum(rows.map(t => t.amount)) };
        })
        .filter(k => k.rows > 0),
);

const points = $derived(billPoints(s.id, data.points, data.transactions));

// Lines linked to these (ADR 0009) may be on other statements; ?line= picks the line to show. Scrolled
// to once the navigation is done, since SvelteKit sets the scroll position at the very end of it; Back
// keeps the position it restores.
const target = $derived(readLine(page.url.searchParams));
afterNavigate(({ to, type }) => {
    if (type === "popstate" || !to) return;
    const line = readLine(to.url.searchParams);
    if (line) document.getElementById(`tx-${line}`)?.scrollIntoView({ block: "center" });
});

/** " · 國泰 2026-11" for a line on another statement. */
function elsewhere(link: Pick<Transaction, "statementId">) {
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
            text: `${doc.parser ?? "?"} v${doc.parserVersion ?? "?"} → ${data.transactions.length} lines · balanced=${s.balanced}`,
            tone: "accent" as const,
        },
        ...doc.warnings.map(w => ({ at: doc.parsedAt ?? doc.receivedAt, source: "warning", text: w, tone: "neg" as const })),
        ...(doc.parsedAt ? failingChecks(s) : []).map(check => ({
            at: doc.parsedAt ?? doc.receivedAt,
            source: "store",
            text: `needs_review · ${checkText(check, s.currency)}`,
            tone: "neg" as const,
        })),
    ].filter(entry => !!entry),
);

onKeys("statement", {
    f: () => window.open(openUrl(doc), "_blank", "noreferrer"),
    r: () => void reparse.run(),
});
</script>

<div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
    <h1 class="m-0 text-[18px] font-bold">{bank.name} · {monthOf(s.closingDate)}</h1>
    <Tag {status} label />
    {#if parsing}<span class="text-accent">re-parsing…</span>{/if}
    <span class="ml-auto flex flex-wrap gap-x-4 text-[12px]">
        <a href={statementsHref(month)} class="text-dim hover:text-ink">← {month} 的帳單</a>
        <!-- The lines on this statement: one statement per bank closes in a month. -->
        <a href={transactionsHref({ closing: month, institution: s.institution })} class="text-dim hover:text-ink">這張帳單的交易 →</a>
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
                        {#if program.lines.length > 0}
                            <div aria-hidden="true" class="col-span-2 border-t border-dashed border-line"></div>
                        {/if}
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
        <h2 class="label">CHECKS · 對帳{s.balanced ? "" : `（${failingChecks(s).length} 項對不上）`}</h2>
        <Checks statement={s} />
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
            <button type="button" class="control" disabled={parsing} onclick={() => void reparse.run()}><span class="text-accent">[r]</span> 修正 parser 後重新解析</button>
        </div>
        {#if reparse.error}<p class="m-0 text-neg">{reparse.error}</p>{/if}
        {#if reparsed}
            <p class="m-0 {reparsed.tone === 'neg' ? 'text-neg' : 'text-dim'}">{reparse.askedAt} 重新解析完成：{reparsed.text}</p>
        {/if}
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
                                    <a href={statementHref(line.t.parent.statementId, line.t.parent.id)} class="ml-2 text-[12px] whitespace-nowrap">
                                        ↑ {kinds[line.t.parent.kind].short} {shortDate(line.t.parent.transactionDate)}{elsewhere(line.t.parent)}
                                    </a>
                                {/if}
                                {#each line.t.children ?? [] as child (child.id)}
                                    <a href={statementHref(child.statementId, child.id)} class="ml-2 text-[12px] whitespace-nowrap">
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
