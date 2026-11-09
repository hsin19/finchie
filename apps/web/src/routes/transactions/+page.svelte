<script lang="ts">
import {
    api,
    type Transaction,
    type TransactionFamily,
    type TransactionKind,
    type TransactionLink,
    TRANSACTIONS_PAGE_SIZE as PAGE_SIZE,
    type TransactionSort,
} from "#lib/api/client.js";
import Counterpart from "#lib/components/Counterpart.svelte";
import Dialog from "#lib/components/Dialog.svelte";
import FileLink from "#lib/components/FileLink.svelte";
import MonthSelect from "#lib/components/MonthSelect.svelte";
import Tag from "#lib/components/Tag.svelte";
import {
    monthOf,
    shortDate,
    weekday,
} from "#lib/dates.js";
import {
    breakdown,
    refundedInFull,
} from "#lib/family.js";
import {
    type FilterChanges,
    pageCount,
    readFilters,
    searchPeriod,
} from "#lib/filters.js";
import {
    flowOf,
    isPoints,
    isTrade,
    isTransfer,
    netByUnit,
} from "#lib/flows.js";
import { composing } from "#lib/keys.js";
import { onKeys } from "#lib/keys.svelte.js";
import {
    cardLabel,
    institution,
    kindOrder,
    kinds,
    lineAccount,
    tone,
} from "#lib/labels.js";
import {
    approx,
    formatAmount,
    negate,
    sign,
    sum,
} from "#lib/money.js";
import { QueryRequests } from "#lib/requests.svelte.js";
import {
    afterNavigate,
    goto,
} from "$app/navigation";
import { resolve } from "$app/paths";
import { page } from "$app/state";
import type { PageProps } from "./$types";

let { data }: PageProps = $props();

const items = $derived(data.result.items);
const statements = $derived(new Map(data.statements.map(s => [s.id, s])));
const documents = $derived(new Map(data.documents.map(d => [d.id, d])));

// ---- URL-backed filters

// Controls show, and changes build on, the filters as last asked for, not as last loaded.
const requests = new QueryRequests(() => page.url.searchParams);
afterNavigate(() => requests.settle());
const filters = $derived(readFilters(requests.params));

function navigate(changes: FilterChanges, { replace = false, reset = false } = {}) {
    const next = requests.request(changes, { replace });
    return goto(next.query ? resolve(`/transactions?${next.query}`) : resolve("/transactions"), { reset, replace: next.replace });
}

// Search applies while typing, a moment after the last key. The box follows the URL (Back, [t], the
// command palette) except when the URL change is the search the box itself sent: by then the user
// may have typed more.
const urlQuery = $derived(page.url.searchParams.get("q") ?? "");
let search = $state(page.url.searchParams.get("q") ?? "");
let searchTimer: ReturnType<typeof setTimeout> | undefined;
let sentQuery: string | undefined;

$effect(() => {
    const q = urlQuery;
    if (q === sentQuery) {
        sentQuery = undefined;
        return;
    }
    clearTimeout(searchTimer);
    search = q;
});

// Leaving the page drops a search that was not sent yet, instead of navigating back here.
$effect(() => () => clearTimeout(searchTimer));

function sendSearch() {
    clearTimeout(searchTimer);
    const q = search.trim();
    if (q === filters.q) return;
    sentQuery = q;
    void navigate({ q: q || null }, { replace: true });
}
function onSearchInput() {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(sendSearch, 250);
}
function onSearchKeydown(event: KeyboardEvent) {
    if (composing(event)) return;
    if (event.key === "Escape") {
        search = "";
        sendSearch();
    } else if (event.key === "Enter" || event.key === "ArrowDown") {
        event.preventDefault();
        sendSearch();
        (event.currentTarget as HTMLInputElement).blur();
        selectedId = items[0]?.id;
    }
}

// No kind filter means every kind. Toggling one off from "all" keeps the rest.
const activeKinds = $derived(new Set<TransactionKind>(filters.kinds.length > 0 ? filters.kinds : kindOrder));
function toggleKind(kind: TransactionKind) {
    const next = kindOrder.filter(k => (k === kind ? !activeKinds.has(k) : activeKinds.has(k)));
    const all = next.length === 0 || next.length === kindOrder.length;
    void navigate({ kind: all ? null : next });
}
const kindCounts = $derived(new Map(data.result.byKind.map(k => [k.kind, k])));

// Account picker: each bank, then its cards and its deposit accounts. Counts come from the byCard and
// byAccount facets, which ignore the picker, so every option shows what picking it would give.
const accountOptions = $derived.by(() => {
    const { byCard, byAccount } = data.result;
    const options: { value: string; label: string; }[] = [];
    const banks = [...new Set([...byCard, ...byAccount].map(c => c.institution))].sort();
    for (const bank of banks) {
        const cards = byCard.filter(c => c.institution === bank);
        const accounts = byAccount.filter(a => a.institution === bank);
        options.push({ value: bank, label: `${institution(bank).code} (all) · ${[...cards, ...accounts].reduce((n, c) => n + c.count, 0)}` });
        for (const c of cards) if (c.cardLast4) options.push({ value: `${bank}:${c.cardLast4}`, label: `${cardLabel(bank, c.cardLast4)} · ${c.count}` });
        for (const a of accounts) {
            const name = lineAccount({ institution: bank, accountKind: "deposit", accountName: a.accountName });
            options.push({ value: `account:${a.accountId}`, label: `${name} ${a.accountNumber ?? ""} · ${a.count}` });
        }
    }
    return options;
});
const pickValue = $derived(
    filters.account ? `account:${filters.account}` : filters.institution ? `${filters.institution}${filters.card ? `:${filters.card}` : ""}` : "",
);
function pick(value: string) {
    const account = value.startsWith("account:") ? value.slice("account:".length) : null;
    void navigate({ account, card: account ? null : value || null });
}

// ---- Selection and detail

let selectedId = $state<number>();
let sheet = $state(false);
const cursor = $derived(items.findIndex(t => t.id === selectedId));
const selected = $derived(cursor >= 0 ? items[cursor] : undefined);

function select(t: Transaction | undefined, { open = false } = {}) {
    selectedId = t?.id;
    if (t && open && !matchMedia("(min-width: 1024px)").matches) sheet = true;
    if (t) document.getElementById(`tx-${t.id}`)?.scrollIntoView({ block: "nearest" });
}

// The selected line's parent and children (ADR 0009), which may be on other statements.
let family = $state<TransactionFamily>();
const linkedId = $derived(selected && (selected.parent || selected.children?.length) ? selected.id : undefined);
$effect(() => {
    const id = linkedId;
    family = undefined;
    if (id === undefined) return;
    const controller = new AbortController();
    api()
        .GET("/api/transactions/{id}", { params: { path: { id } }, signal: controller.signal })
        .then(({ data }) => (family = data))
        // Aborted, or failed: the panel shows the line without its links.
        .catch(() => undefined);
    return () => controller.abort();
});

/** Where a linked line is: its statement, scrolled to the line. */
function lineHref(link: Pick<TransactionLink, "id" | "statementId">) {
    return resolve(`/statements/[id]?line=${link.id}`, { id: String(link.statementId) });
}

function statementLabel(statementId: number) {
    const statement = statements.get(statementId);
    return statement ? `${institution(statement.institution).name} ${monthOf(statement.closingDate)}` : `#${statementId}`;
}

// ---- Paging: a new page starts at its top.

const first = $derived(data.result.total === 0 ? 0 : (data.filters.page - 1) * PAGE_SIZE + 1);
const last = $derived((data.filters.page - 1) * PAGE_SIZE + items.length);
const pages = $derived(pageCount(data.result.total));
function goPage(n: number) {
    if (n < 1 || n > pages || n === filters.page) return;
    selectedId = undefined;
    void navigate({ page: n === 1 ? null : String(n) }, { reset: true });
}

// ---- Sort: by date or by amount; clicking the active one flips its direction.

const byDate = $derived(filters.sort === "newest" || filters.sort === "oldest");
const sortTitles: Record<TransactionSort, string> = { newest: "新到舊", oldest: "舊到新", largest: "大筆在前", smallest: "小筆在前" };
function setSort(sort: TransactionSort) {
    selectedId = undefined;
    void navigate({ sort: sort === "newest" ? null : sort });
}

// Mobile lists the page grouped by day when sorted by date.
const days = $derived.by(() => {
    const groups: { date: string; items: Transaction[]; }[] = [];
    for (const t of items) {
        if (groups.at(-1)?.date !== t.transactionDate) groups.push({ date: t.transactionDate, items: [] });
        groups.at(-1)!.items.push(t);
    }
    return groups;
});

// Amounts as the owner sees them: money out negative, whichever account (ADR 0013). Money that only
// moved between the owner's own lines is muted.
function amountTone(t: Transaction) {
    if (refundedInFull(t) || isTransfer(t)) return "text-mute";
    // Shares or points, not money (ADR 0017, ADR 0019).
    if (isTrade(t) || isPoints(t)) return "text-ink";
    if (sign(flowOf(t)) > 0) return "text-pos";
    return t.kind === "purchase" || t.kind === "other" ? "text-ink" : tone[kinds[t.kind].tone];
}

function amount(t: Transaction) {
    return formatAmount(flowOf(t), { signed: true });
}

/** The unit after an amount, unless it is NT$. */
function unitOf(t: Pick<Transaction, "unit">) {
    return t.unit && t.unit !== "TWD" ? t.unit : "";
}

function dayTotal(lines: Transaction[]) {
    return netByUnit(lines).map(n => `${n.unit === "TWD" ? "" : `${n.unit} `}${formatAmount(n.amount, { signed: true })}`).join(" · ");
}

function original(t: Transaction) {
    return t.originalAmount && t.originalCurrency ? `${t.originalCurrency} ${formatAmount(t.originalAmount)}` : "";
}

function rate(t: Transaction) {
    if (!t.originalAmount || !t.originalCurrency || approx(t.originalAmount) === 0) return undefined;
    return `${(approx(t.amount) / approx(t.originalAmount)).toFixed(2)} TWD / ${t.originalCurrency}`;
}

onKeys({
    "j": () => select(items[Math.min(cursor + 1, items.length - 1)]),
    "k": () => select(items[Math.max(cursor - 1, 0)]),
    "Enter": () => select(selected ?? items[0], { open: true }),
    "x": () => {
        selectedId = undefined;
        sheet = false;
    },
    "Escape": () => (selectedId = undefined),
    "n": () => goPage(filters.page + 1),
    "p": () => goPage(filters.page - 1),
    "/": () => document.getElementById("q")?.focus(),
});
</script>

{#snippet linked(l: Transaction, arrow: string, from: Transaction)}
    <a href={lineHref(l)} class="flex flex-col gap-0.5 rounded-[4px] px-1.5 py-1 text-ink hover:bg-raised hover:text-ink">
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

{#snippet detail(t: Transaction)}
    {@const statement = statements.get(t.statementId)}
    {@const doc = statement ? documents.get(statement.documentId) : undefined}
    <div class="flex flex-col gap-3">
        <div class="text-[16px] font-bold break-words">{t.description}</div>
        <div class="flex flex-col gap-0.5">
            <div class="text-[24px] font-bold {amountTone(t)}">{amount(t)} <span class="text-[13px] font-normal text-mute">{t.unit ?? statement?.currency ?? "TWD"}</span></div>
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
                <dd class="m-0 text-dim">{rate(t)}</dd>
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
                <dd class="m-0">{formatAmount(t.trade.prepaid ?? (sign(t.trade.net) < 0 ? negate(t.trade.net) : t.trade.net))} <span class="text-mute">TWD</span></dd>
            {/if}
            {#if t.accountKind !== undefined && t.accountKind !== "credit_card" && t.accountId !== undefined}
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
                <dd class="m-0 text-dim">{formatAmount(t.balance)} <span class="text-mute">{t.unit ?? ""}</span></dd>
            {/if}
            {#if statement}
                <dt class="text-mute">statement</dt>
                <dd class="m-0">
                    <a href={resolve("/statements/[id]", { id: String(statement.id) })}>{t.institution} {monthOf(statement.closingDate)}</a>
                    <Tag status={statement.reconciliation.balanced ? "parsed" : "needs_review"} />
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
{/snippet}

<section class="panel flex flex-col gap-2.5 p-3 md:p-3.5">
    <div class="flex items-center gap-2 rounded-[4px] border border-line-strong bg-ground px-2.5">
        <label for="q" class="whitespace-nowrap"><span class="text-accent">[/]</span> 搜尋</label>
        <input
            id="q"
            type="search"
            autocomplete="off"
            placeholder="說明或備註，例如 uber、房租"
            class="min-h-10 min-w-0 grow border-none bg-transparent text-ink outline-none"
            bind:value={search}
            oninput={onSearchInput}
            onkeydown={onSearchKeydown}
        />
        <span class="hidden text-[12px] text-mute sm:inline"><span class="text-accent">[esc]</span> 清除</span>
    </div>
    <div class="flex flex-wrap items-center gap-2">
        <MonthSelect all months={data.months} value={searchPeriod(filters, data.latestMonth)} onchange={m => void navigate({ m })} />
        {#if filters.closing}
            <button type="button" class="control pressed" title="移除這個篩選" onclick={() => void navigate({ closing: null })}>
                {filters.closing} 結帳的帳單 <span class="text-mute">×</span>
            </button>
        {/if}
        <select class="control" aria-label="帳戶或卡片" value={pickValue} onchange={event => pick(event.currentTarget.value)}>
            <option value="">all accounts</option>
            {#each accountOptions as option (option.value)}
                <option value={option.value}>{option.label}</option>
            {/each}
            {#if pickValue && !accountOptions.some(o => o.value === pickValue)}<option value={pickValue}>{pickValue}</option>{/if}
        </select>
        <div role="group" aria-label="交易種類" class="flex gap-1.5 overflow-x-auto whitespace-nowrap">
            {#each kindOrder as kind (kind)}
                {@const on = activeKinds.has(kind)}
                <button type="button" aria-pressed={on} class="control shrink-0 {on ? 'pressed' : 'text-mute'}" onclick={() => toggleKind(kind)}>
                    {kinds[kind].short} <span class="text-mute">{kindCounts.get(kind)?.count ?? 0}</span>
                </button>
            {/each}
        </div>
        <label class="flex min-h-9 items-center gap-1.5 text-dim">
            <input type="checkbox" class="accent-accent" checked={filters.overseas} onchange={event => void navigate({ overseas: event.currentTarget.checked ? "1" : null })} />
            只看海外
        </label>
        <label class="flex min-h-9 items-center gap-1.5 text-dim" title="自己帳戶之間的轉帳、繳卡費">
            <input
                type="checkbox"
                class="accent-accent"
                checked={filters.hideTransfers}
                onchange={event => void navigate({ hide: event.currentTarget.checked ? "transfers" : null })}
            />
            隱藏轉帳與卡費
        </label>
    </div>
</section>

<div class="flex flex-wrap gap-x-5 gap-y-1 border-b border-dashed border-line px-0.5 pb-2 text-dim">
    <span>rows <span class="text-ink">{data.result.total}</span></span>
    <!-- Seen from the owner; money between their own lines is neither (ADR 0016). -->
    {#each data.result.flows as flow (flow.unit)}
        {@const unit = flow.unit === "TWD" ? "" : `${flow.unit} `}
        {@const total = sum([flow.spent, flow.received])}
        <!-- Another unit's zero is noise: most have money only one way. -->
        {#if unit === "" || sign(flow.spent) !== 0}<span>{unit}支出 <span class="text-ink">{formatAmount(flow.spent)}</span></span>{/if}
        {#if unit === "" || sign(flow.received) !== 0}<span>{unit}收入 <span class="text-pos">{formatAmount(flow.received, { signed: true })}</span></span>{/if}
        <span>{unit}淨額 <span class={sign(total) < 0 ? "text-neg" : "text-ink"}>{formatAmount(total, { signed: true })}</span></span>
        {#if flow.movedCount > 0}
            <span title="自己帳戶之間的轉帳、繳卡費：每筆只列一次，不算支出或收入">
                {unit}轉帳 <span class="text-mute">{formatAmount(flow.moved)}（{flow.movedCount} 筆）</span>
            </span>
        {/if}
    {/each}
    <span role="group" aria-label="排序" class="ml-auto flex items-center gap-2 text-mute">
        sort
        <button
            type="button"
            aria-pressed={byDate}
            title={sortTitles[filters.sort === "oldest" ? "newest" : "oldest"]}
            class={byDate ? "text-ink" : "hover:text-ink"}
            onclick={() => setSort(filters.sort === "newest" ? "oldest" : "newest")}
        >
            date{byDate ? (filters.sort === "oldest" ? " ▲" : " ▼") : ""}
        </button>
        <button
            type="button"
            aria-pressed={!byDate}
            title={sortTitles[filters.sort === "largest" ? "smallest" : "largest"]}
            class={byDate ? "hover:text-ink" : "text-ink"}
            onclick={() => setSort(filters.sort === "largest" ? "smallest" : "largest")}
        >
            amount{byDate ? "" : filters.sort === "smallest" ? " ▲" : " ▼"}
        </button>
    </span>
</div>

<div class="grid gap-3 {selected ? 'lg:grid-cols-[1fr_340px]' : ''}">
    <section class="panel min-w-0 px-3.5 pt-1 pb-2.5">
        <!-- Desktop: a table. -->
        <div class="hidden overflow-x-auto md:block">
            <table class="w-full border-collapse">
                <thead>
                    <tr class="text-left text-mute">
                        <th class="w-4 py-2 pr-1 font-medium"><span class="sr-only">選取</span></th>
                        <th class="px-2 py-2 font-medium">DATE</th>
                        <th class="px-2 py-2 font-medium">DESCRIPTION</th>
                        <th class="px-2 py-2 font-medium">KIND</th>
                        <th class="px-2 py-2 font-medium">ACCOUNT</th>
                        <th class="px-2 py-2 font-medium">CTRY</th>
                        <th class="px-2 py-2 text-right font-medium">ORIG</th>
                        <th class="py-2 pl-2 text-right font-medium">AMOUNT</th>
                    </tr>
                </thead>
                <tbody>
                    {#each items as t (t.id)}
                        <tr
                            id="tx-{t.id}"
                            class="cursor-pointer border-t border-rule {t.id === selectedId ? 'bg-raised' : 'hover:bg-raised/60'}"
                            onclick={() => select(t, { open: true })}
                        >
                            <td class="py-2 pr-1 text-accent">{t.id === selectedId ? "▶" : ""}</td>
                            <td class="px-2 py-2 whitespace-nowrap">{shortDate(t.transactionDate)}</td>
                            <td class="max-w-[320px] px-2 py-2">
                                <span class="block truncate">{t.description}{#if t.memo}<span class="text-dim"> · {t.memo}</span>{/if}</span>
                                {#if breakdown(t)}<span class="block truncate text-[11px] text-mute">帳單上 {breakdown(t)}</span>{/if}
                                {#if t.counterpart}<Counterpart other={t.counterpart} out={sign(flowOf(t)) < 0} class="block truncate text-[11px] text-mute hover:text-ink" />{/if}
                            </td>
                            <td class="px-2 py-2 {tone[kinds[t.kind].tone]}">{kinds[t.kind].short}</td>
                            <td class="px-2 py-2 whitespace-nowrap text-dim">{lineAccount(t)}</td>
                            <td class="px-2 py-2 text-dim">{t.country ?? "—"}</td>
                            <td class="px-2 py-2 text-right whitespace-nowrap text-dim">{original(t)}</td>
                            <td class="py-2 pl-2 text-right whitespace-nowrap {amountTone(t)}">
                                {amount(t)}{#if unitOf(t)} <span class="text-mute">{unitOf(t)}</span>{/if}
                            </td>
                        </tr>
                    {:else}
                        <tr class="border-t border-rule"><td colspan="8" class="py-3 text-mute">沒有符合的交易。</td></tr>
                    {/each}
                </tbody>
            </table>
        </div>

        <!-- Mobile: grouped by day when sorted by date; by amount, one list with the date on each row. -->
        {#snippet mobileRow(t: Transaction, withDate: boolean)}
            <button type="button" class="flex min-h-11 w-full flex-col gap-0.5 border-t border-rule py-2.5 text-left text-ink" onclick={() => select(t, { open: true })}>
                <span class="flex w-full justify-between gap-2.5">
                    <span class="truncate">{t.description}{t.memo ? ` · ${t.memo}` : ""}</span>
                    <span class="shrink-0 {amountTone(t)}">{amount(t)}{unitOf(t) ? ` ${unitOf(t)}` : ""}</span>
                </span>
                <span class="text-[12px] text-mute">
                    {withDate ? `${shortDate(t.transactionDate)} · ` : ""}{lineAccount(t)} · {kinds[t.kind].short}{original(t) ? ` · ${original(t)}` : ""}{breakdown(t) ? ` · ${breakdown(t)}` : ""}{t.counterpart ? ` · ${sign(flowOf(t)) < 0 ? "→" : "←"} 自己的帳戶` : ""}
                </span>
            </button>
        {/snippet}
        <div class="md:hidden">
            {#if byDate}
                {#each days as day (day.date)}
                    <div class="flex justify-between pt-3 pb-1.5 text-[12px] tracking-[0.06em] text-mute">
                        <span>{shortDate(day.date)} {weekday(day.date)}</span>
                        <span>{dayTotal(day.items)}</span>
                    </div>
                    {#each day.items as t (t.id)}
                        {@render mobileRow(t, false)}
                    {/each}
                {:else}
                    <p class="m-0 py-3 text-mute">沒有符合的交易。</p>
                {/each}
            {:else}
                {#each items as t (t.id)}
                    {@render mobileRow(t, true)}
                {:else}
                    <p class="m-0 py-3 text-mute">沒有符合的交易。</p>
                {/each}
            {/if}
        </div>

        <div class="flex flex-wrap items-center justify-between gap-2 border-t border-line pt-2.5 text-[12px] text-mute">
            <span>{first}–{last} / {data.result.total}</span>
            <span class="flex items-center gap-2">
                <span class="hidden lg:inline"><span class="text-accent">[j/k]</span> 上下移動 · <span class="text-accent">[enter]</span> 詳細 ·</span>
                <button type="button" class="control min-h-8" disabled={filters.page <= 1} onclick={() => goPage(filters.page - 1)}><span class="text-accent">[p]</span> prev</button>
                <button type="button" class="control min-h-8" disabled={filters.page >= pages} onclick={() => goPage(filters.page + 1)}><span class="text-accent">[n]</span> next</button>
            </span>
        </div>
    </section>

    {#if selected}
        <aside aria-label="交易詳細" class="panel hidden self-start p-3.5 lg:sticky lg:top-3 lg:block">
            <div class="mb-3 flex items-center justify-between">
                <h2 class="label">TXN #{selected.id}</h2>
                <button type="button" class="text-mute hover:text-ink" aria-label="關閉詳細" onclick={() => (selectedId = undefined)}><span class="text-accent">[x]</span></button>
            </div>
            {@render detail(selected)}
        </aside>
    {/if}
</div>

<Dialog bind:open={sheet} title="交易詳細">
    {#if selected}
        <div class="flex items-center justify-between border-b border-line px-4 py-3">
            <h2 class="label">TXN #{selected.id}</h2>
            <button type="button" class="text-mute" aria-label="關閉" onclick={() => (sheet = false)}><span class="text-accent">[esc]</span></button>
        </div>
        <div class="p-4">{@render detail(selected)}</div>
    {/if}
</Dialog>
