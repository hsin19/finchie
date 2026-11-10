<script lang="ts">
import type {
    Document,
    Statement,
    Transaction,
    TransactionFamily,
} from "#lib/api/client.js";
import Counterpart from "#lib/components/Counterpart.svelte";
import FileLink from "#lib/components/FileLink.svelte";
import Tag from "#lib/components/Tag.svelte";
import {
    monthOf,
    shortDate,
} from "#lib/dates.js";
import {
    breakdown,
    refundedInFull,
} from "#lib/family.js";
import {
    flowOf,
    isPoints,
    isTrade,
    isTransfer,
} from "#lib/flows.js";
import {
    cardLabel,
    institution,
} from "#lib/institutions.js";
import { kinds } from "#lib/kinds.js";
import {
    approx,
    formatAmount,
    negate,
    sign,
} from "#lib/money.js";
import { statementStatus } from "#lib/statements.js";
import { tone } from "#lib/tone.js";
import {
    amountText,
    amountTone,
    original,
    rate,
} from "#lib/transactions.js";
import { statementHref } from "#lib/urls.js";
import { resolve } from "$app/paths";

/**
 * One transaction in full: its amounts, where it is, its statement and document, and the lines linked to
 * it (`family`, ADR 0009), which may be on other statements. `statements` and `documents` are the
 * layout's, by id.
 */
let { t, family, statements, documents, base }: {
    t: Transaction;
    family?: TransactionFamily;
    statements: ReadonlyMap<number, Statement>;
    documents: ReadonlyMap<number, Document>;
    base: string;
} = $props();

const statement = $derived(statements.get(t.statementId));
const doc = $derived(statement ? documents.get(statement.documentId) : undefined);

function statementLabel(statementId: number) {
    const other = statements.get(statementId);
    return other ? `${institution(other.institution).name} ${monthOf(other.closingDate)}` : `#${statementId}`;
}
</script>

{#snippet linked(l: Transaction, arrow: string, from: Transaction)}
    <a href={statementHref(l.statementId, l.id)} class="flex flex-col gap-0.5 rounded-[4px] px-1.5 py-1 text-ink hover:bg-raised hover:text-ink">
        <span class="flex gap-2">
            <span class="text-accent">{arrow}</span>
            <span class={tone[kinds[l.kind].tone]}>{kinds[l.kind].short}</span>
            <span class="grow truncate">{l.description}</span>
            <span class={l.kind === "purchase" ? "text-ink" : tone[kinds[l.kind].tone]}>{formatAmount(l.amount)}</span>
        </span>
        <span class="pl-5 text-[12px] text-mute">
            {shortDate(l.transactionDate)}{l.statementId === from.statementId ? "" : ` · ${statementLabel(l.statementId)}`}
            {#if l.kind === "foreign_transaction_fee" && approx(from.amount) > 0}
                · {((approx(l.amount) / approx(from.amount)) * 100).toFixed(1)}%
            {/if}
        </span>
    </a>
{/snippet}

<div class="flex flex-col gap-3">
    <div class="text-[16px] font-bold break-words">{t.description}</div>
    <div class="flex flex-col gap-0.5">
        <div class="text-[24px] font-bold {amountTone(t)}">{amountText(t)} <span class="text-[13px] font-normal text-mute">{t.unit}</span></div>
        {#if breakdown(t)}<div class="text-[12px] text-dim">帳單上 {breakdown(t)}{refundedInFull(t) ? " · 已全額刷退" : ""}</div>{/if}
        {#if isTransfer(t)}<div class="text-[12px] text-mute">在自己的帳戶之間移動：不算支出或收入</div>{/if}
        {#if isTrade(t)}<div class="text-[12px] text-mute">股數：買賣不算支出或收入，手續費和稅算進成本</div>{/if}
        {#if isPoints(t)}<div class="text-[12px] text-mute">點數：帳單上這一期的合計，不算支出或收入</div>{/if}
    </div>
    <dl class="m-0 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
        <dt class="text-mute">date</dt>
        <dd class="m-0">{t.transactionDate}{t.postingDate && t.postingDate !== t.transactionDate ? `（入帳 ${shortDate(t.postingDate)}）` : ""}</dd>
        <dt class="text-mute">kind</dt>
        <dd class="m-0 {tone[kinds[t.kind].tone]}">{kinds[t.kind].short} <span class="text-mute">{kinds[t.kind].label}</span></dd>
        {#if original(t)}
            <dt class="text-mute">original</dt>
            <dd class="m-0">{original(t)}</dd>
            <dt class="text-mute">rate</dt>
            <dd class="m-0 text-dim">{rate(t, base)}</dd>
        {/if}
        {#if t.country}
            <dt class="text-mute">country</dt>
            <dd class="m-0">{t.country}</dd>
        {/if}
        {#if t.trade}
            <dt class="text-mute">trade</dt>
            <dd class="m-0">{t.trade.category} @ {formatAmount(t.trade.price)}</dd>
            <dt class="text-mute">amount</dt>
            <dd class="m-0">{formatAmount(t.trade.amount)} <span class="text-mute">手續費 {formatAmount(t.trade.fee)} · 稅 {formatAmount(t.trade.tax)}</span></dd>
            <dt class="text-mute">{t.trade.prepaid ? "prepaid" : sign(t.trade.net) < 0 ? "paid" : "received"}</dt>
            <dd class="m-0">{formatAmount(t.trade.prepaid ?? (sign(t.trade.net) < 0 ? negate(t.trade.net) : t.trade.net))} <span class="text-mute">{base}</span></dd>
        {/if}
        {#if t.accountKind !== "credit_card" && t.accountId !== undefined}
            <dt class="text-mute">account</dt>
            <dd class="m-0">
                <a href={resolve("/accounts/[id]", { id: String(t.accountId) })}>{institution(t.institution).name} {t.accountName ?? ""}</a>
                <span class="text-mute">{t.accountNumber ?? ""}</span>
            </dd>
        {:else}
            <dt class="text-mute">card</dt>
            <dd class="m-0">{institution(t.institution).name} {cardLabel(t.institution, t.cardLast4)}</dd>
        {/if}
        {#if t.memo}
            <dt class="text-mute">memo</dt>
            <dd class="m-0 break-words">{t.memo}</dd>
        {/if}
        {#if t.counterparty}
            <dt class="text-mute">counterparty</dt>
            <dd class="m-0 break-words text-dim">{t.counterparty}</dd>
        {/if}
        {#if t.counterpart}
            <dt class="text-mute">{t.accountKind === "deposit" && t.counterpart.accountKind === "credit_card" ? "paid" : "transfer"}</dt>
            <dd class="m-0"><Counterpart other={t.counterpart} out={sign(flowOf(t)) < 0} /></dd>
        {/if}
        {#if t.balance}
            <dt class="text-mute">balance</dt>
            <dd class="m-0 text-dim">{formatAmount(t.balance)} <span class="text-mute">{t.unit}</span></dd>
        {/if}
        {#if statement}
            <dt class="text-mute">statement</dt>
            <dd class="m-0">
                <a href={resolve("/statements/[id]", { id: String(statement.id) })}>{t.institution} {monthOf(statement.closingDate)}</a>
                <Tag status={statementStatus(statement, doc)} />
            </dd>
        {/if}
        {#if doc}
            <dt class="text-mute">parser</dt>
            <dd class="m-0 text-dim">{doc.parser} v{doc.parserVersion}</dd>
        {/if}
    </dl>
    {#if family && (family.parent || family.children.length > 0)}
        <div class="flex flex-col gap-1 border-t border-dashed border-line pt-2.5">
            <div class="label">LINKED · 關聯</div>
            {#if family.parent}
                {@render linked(family.parent, "↑", t)}
            {/if}
            {#each family.children as child (child.id)}
                {@render linked(child, "↓", t)}
            {/each}
        </div>
    {/if}
    {#if doc}
        <FileLink {doc}>原始檔 #{doc.id} {doc.filename} →</FileLink>
    {/if}
</div>
