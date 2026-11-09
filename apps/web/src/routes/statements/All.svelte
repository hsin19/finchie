<script lang="ts">
import type {
    Document,
    Statement,
} from "#lib/api/client.js";
import Tag from "#lib/components/Tag.svelte";
import { monthOf } from "#lib/dates.js";
import { institution } from "#lib/labels.js";
import {
    formatAmount,
    subtract,
} from "#lib/money.js";
import { resolve } from "$app/paths";

// Documents, those that produced no statement among them, are on /documents.
let { statements, documents: allDocuments }: { statements: Statement[]; documents: Document[]; } = $props();

const documents = $derived(new Map(allDocuments.map(d => [d.id, d])));
</script>

<section class="panel px-3.5 pt-1 pb-2.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
        <h1 class="label">STATEMENTS · 全部帳單（{statements.length}）</h1>
        <span class="text-[12px] text-mute">依結帳日，新的在上 · <a href={resolve("/statements")} class="text-dim hover:text-ink"><span class="text-accent">[l]</span> 按月看 →</a></span>
    </div>
    <div class="overflow-x-auto">
        <table class="w-full min-w-[680px] border-collapse">
            <thead>
                <tr class="text-left text-mute">
                    <th class="py-2 pr-2 font-medium">STATUS</th>
                    <th class="px-2 py-2 font-medium">BANK</th>
                    <th class="px-2 py-2 font-medium">CLOSING</th>
                    <th class="px-2 py-2 font-medium">DUE</th>
                    <th class="px-2 py-2 font-medium">PAID</th>
                    <th class="px-2 py-2 text-right font-medium">TOTAL_DUE</th>
                    <th class="px-2 py-2 text-right font-medium">ROWS</th>
                    <th class="py-2 pl-2 font-medium">RECONCILE</th>
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
                        <td class="py-2.5 pl-2 {s.reconciliation.balanced ? 'text-pos' : 'text-neg'}">
                            {s.reconciliation.balanced ? "balanced" : `Δ ${formatAmount(delta, { signed: true })}`}
                        </td>
                    </tr>
                {:else}
                    <tr class="border-t border-rule">
                        <td colspan="8" class="py-3 text-mute">還沒有帳單。<a href={resolve("/documents")}>上傳 →</a></td>
                    </tr>
                {/each}
            </tbody>
        </table>
    </div>
</section>
