<script lang="ts">
import {
    api,
    type Document,
    LEDGER,
    type Statement,
    unwrap,
} from "#lib/api/client.js";
import FileLink from "#lib/components/FileLink.svelte";
import Tag from "#lib/components/Tag.svelte";
import {
    formatBytes,
    monthOf,
    shortDateTime,
} from "#lib/dates.js";
import {
    documentStatus,
    institution,
} from "#lib/labels.js";
import {
    formatAmount,
    subtract,
} from "#lib/money.js";
import { invalidate } from "$app/navigation";
import { resolve } from "$app/paths";

let { statements, documents: allDocuments }: { statements: Statement[]; documents: Document[]; } = $props();

const documents = $derived(new Map(allDocuments.map(d => [d.id, d])));
const parsedDocuments = $derived(new Set(statements.map(s => s.documentId)));
// Documents that produced no statement: still parsing, failed, or a second copy of one.
const others = $derived(allDocuments.filter(d => !parsedDocuments.has(d.id)));
let reparseError = $state<string>();

async function reparse(id: number) {
    reparseError = undefined;
    try {
        unwrap(await api().POST("/api/documents/{id}/reparse", { params: { path: { id } } }));
        await invalidate(LEDGER);
    } catch (error) {
        reparseError = `#${id}: ${(error as Error).message}`;
    }
}
</script>

<section class="panel px-3.5 pt-1 pb-2.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
        <h1 class="label">STATEMENTS · 全部帳單（{statements.length}）</h1>
        <span class="text-[12px] text-mute">依結帳日，新的在上 · <a href={resolve("/statements")} class="text-dim hover:text-ink"><span class="text-accent">[a]</span> 按月看 →</a></span>
    </div>
    <div class="overflow-x-auto">
        <table class="w-full min-w-[860px] border-collapse">
            <thead>
                <tr class="text-left text-mute">
                    <th class="py-2 pr-2 font-medium">STATUS</th>
                    <th class="px-2 py-2 font-medium">BANK</th>
                    <th class="px-2 py-2 font-medium">CLOSING</th>
                    <th class="px-2 py-2 font-medium">DUE</th>
                    <th class="px-2 py-2 font-medium">PAID</th>
                    <th class="px-2 py-2 text-right font-medium">TOTAL_DUE</th>
                    <th class="px-2 py-2 text-right font-medium">ROWS</th>
                    <th class="px-2 py-2 font-medium">RECONCILE</th>
                    <th class="py-2 pl-2 font-medium">DOCUMENT</th>
                </tr>
            </thead>
            <tbody>
                {#each statements as s (s.id)}
                    {@const doc = documents.get(s.documentId)}
                    {@const delta = subtract(s.reconciliation.stated, s.reconciliation.computed)}
                    <tr class="border-t border-rule hover:bg-raised">
                        <td class="py-2.5 pr-2"><Tag status={doc?.status ?? (s.reconciliation.balanced ? "parsed" : "needs_review")} /></td>
                        <td class="px-2 py-2.5">
                            <a href={resolve("/statements/[id]", { id: String(s.id) })}>{institution(s.institution).name} {monthOf(s.closingDate)}</a>
                        </td>
                        <td class="px-2 py-2.5 text-dim">{s.closingDate}</td>
                        <td class="px-2 py-2.5 text-dim">{s.dueDate}</td>
                        <td class="px-2 py-2.5 {s.paidOn ? 'text-pos' : 'text-mute'}" title={s.paidOn ? "" : "還沒看到繳款：等涵蓋到期日的存款對帳單"}>{s.paidOn ?? "—"}</td>
                        <td class="px-2 py-2.5 text-right">{formatAmount(s.totalDue)} <span class="text-mute">{s.currency}</span></td>
                        <td class="px-2 py-2.5 text-right text-dim">{s.transactionCount}</td>
                        <td class="px-2 py-2.5 {s.reconciliation.balanced ? 'text-pos' : 'text-neg'}">
                            {s.reconciliation.balanced ? "balanced" : `Δ ${formatAmount(delta, { signed: true })}`}
                        </td>
                        <td class="py-2.5 pl-2 text-dim">
                            {#if doc}<FileLink {doc} class="text-dim hover:text-ink">#{doc.id} {doc.filename}</FileLink>{/if}
                        </td>
                    </tr>
                {:else}
                    <tr class="border-t border-rule">
                        <td colspan="8" class="py-3 text-mute">還沒有帳單。<a href={resolve("/upload")}>上傳 →</a></td>
                    </tr>
                {/each}
            </tbody>
        </table>
    </div>
</section>

{#if others.length > 0}
    <section class="panel px-3.5 pt-1 pb-2.5">
        <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
            <h2 class="label">NO STATEMENT · 沒有產生帳單的文件（{others.length}）</h2>
            {#if reparseError}<span class="text-[12px] text-neg">{reparseError}</span>{/if}
        </div>
        {#each others as doc (doc.id)}
            <div class="flex flex-wrap items-start gap-x-3 gap-y-1 border-t border-rule py-2.5">
                <Tag status={documentStatus(doc)} />
                <div class="flex min-w-0 grow flex-col gap-0.5">
                    <FileLink {doc} class="truncate text-ink hover:text-accent">#{doc.id} {doc.filename}</FileLink>
                    <span class="text-[12px] {doc.status === 'failed' ? 'text-neg' : 'text-mute'}">
                        {doc.error ?? (doc.status === "pending" ? "parsing…" : doc.status)}
                    </span>
                </div>
                <span class="text-[12px] text-mute">{formatBytes(doc.sizeBytes)} · {shortDateTime(doc.receivedAt)}</span>
                {#if documentStatus(doc) === "failed"}
                    <button type="button" class="control min-h-8" onclick={() => void reparse(doc.id)}>重新解析</button>
                {/if}
            </div>
        {/each}
    </section>
{/if}
