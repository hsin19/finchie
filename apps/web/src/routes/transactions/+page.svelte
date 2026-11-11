<script lang="ts">
import {
    api,
    type Transaction,
    type TransactionFamily,
    TRANSACTIONS_PAGE_SIZE as PAGE_SIZE,
    type TransactionSort,
} from "#lib/api/client.js";
import Dialog from "#lib/components/Dialog.svelte";
import { documentsById } from "#lib/documents.js";
import {
    type FilterChanges,
    pageCount,
    readFilters,
    transactionsQueryHref,
} from "#lib/filters.js";
import { onKeys } from "#lib/keys.svelte.js";
import {
    formatAmount,
    sign,
    sum,
} from "#lib/money.js";
import { QueryRequests } from "#lib/requests.svelte.js";
import {
    afterNavigate,
    goto,
} from "$app/navigation";
import { page } from "$app/state";
import type { PageProps } from "./$types";
import TransactionDetail from "./TransactionDetail.svelte";
import TransactionFilters from "./TransactionFilters.svelte";
import TransactionList from "./TransactionList.svelte";

let { data }: PageProps = $props();

const items = $derived(data.result.items);
const base = $derived(data.settings.baseCurrency);
const statements = $derived(new Map(data.statements.map(s => [s.id, s])));
const documents = $derived(documentsById(data.documents, data.pending));

// ---- URL-backed filters

// Controls show, and changes build on, the filters as last asked for, not as last loaded.
const requests = new QueryRequests(() => page.url.searchParams);
afterNavigate(() => requests.settle());
const filters = $derived(readFilters(requests.params));

function navigate(changes: FilterChanges, { replace = false, reset = false } = {}) {
    const next = requests.request(changes, { replace });
    return goto(transactionsQueryHref(next.query), { reset, replace: next.replace });
}

// ---- Selection and detail

let selectedId = $state<number>();
let sheet = $state(false);
const cursor = $derived(items.findIndex(t => t.id === selectedId));
const selected = $derived(cursor >= 0 ? items[cursor] : undefined);

// Where the detail goes: beside the rows from this width (Tailwind's lg, as the layout below), else a sheet.
const WIDE = "(min-width: 1024px)";

function select(t: Transaction | undefined, { open = false } = {}) {
    selectedId = t?.id;
    if (t && open && !matchMedia(WIDE).matches) sheet = true;
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

onKeys("transactions", {
    j: () => select(items[Math.min(cursor + 1, items.length - 1)]),
    k: () => select(items[Math.max(cursor - 1, 0)]),
    Enter: () => select(selected ?? items[0], { open: true }),
    x: () => {
        selectedId = undefined;
        sheet = false;
    },
    Escape: () => (selectedId = undefined),
    n: () => goPage(filters.page + 1),
    p: () => goPage(filters.page - 1),
});
</script>

<TransactionFilters
    {filters}
    result={data.result}
    months={data.months}
    latestMonth={data.latestMonth}
    {navigate}
    onleave={() => (selectedId = items[0]?.id)}
/>

<div class="flex flex-wrap gap-x-5 gap-y-1 border-b border-dashed border-line px-0.5 pb-2 text-dim">
    <span>rows <span class="text-ink">{data.result.total}</span></span>
    <!-- Seen from the owner; money between their own lines is neither (ADR 0016). -->
    {#each data.result.flows as flow (flow.unit)}
        {@const unit = flow.unit === base ? "" : `${flow.unit} `}
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
    <TransactionList {items} {selectedId} {byDate} {base} onselect={t => select(t, { open: true })}>
        <div class="flex flex-wrap items-center justify-between gap-2 border-t border-line pt-2.5 text-[12px] text-mute">
            <span>{first}–{last} / {data.result.total}</span>
            <span class="flex items-center gap-2">
                <span class="hidden lg:inline"><span class="text-accent">[j/k]</span> 上下移動 · <span class="text-accent">[enter]</span> 詳細 ·</span>
                <button type="button" class="control min-h-8" disabled={filters.page <= 1} onclick={() => goPage(filters.page - 1)}><span class="text-accent">[p]</span> prev</button>
                <button type="button" class="control min-h-8" disabled={filters.page >= pages} onclick={() => goPage(filters.page + 1)}><span class="text-accent">[n]</span> next</button>
            </span>
        </div>
    </TransactionList>

    {#if selected}
        <aside aria-label="交易詳細" class="panel hidden self-start p-3.5 lg:sticky lg:top-3 lg:block">
            <div class="mb-3 flex items-center justify-between">
                <h2 class="label">TXN #{selected.id}</h2>
                <button type="button" class="text-mute hover:text-ink" aria-label="關閉詳細" onclick={() => (selectedId = undefined)}><span class="text-accent">[x]</span></button>
            </div>
            <TransactionDetail t={selected} {family} {statements} {documents} {base} />
        </aside>
    {/if}
</div>

<Dialog bind:open={sheet} title="交易詳細">
    {#if selected}
        <div class="flex items-center justify-between border-b border-line px-4 py-3">
            <h2 class="label">TXN #{selected.id}</h2>
            <button type="button" class="text-mute" aria-label="關閉" onclick={() => (sheet = false)}><span class="text-accent">[esc]</span></button>
        </div>
        <div class="p-4"><TransactionDetail t={selected} {family} {statements} {documents} {base} /></div>
    {/if}
</Dialog>
