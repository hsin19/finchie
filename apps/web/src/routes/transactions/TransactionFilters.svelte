<script lang="ts">
import type {
    TransactionKind,
    TransactionSearch,
} from "#lib/api/client.js";
import MonthSelect from "#lib/components/MonthSelect.svelte";
import SearchBox from "#lib/components/SearchBox.svelte";
import type { Month } from "#lib/dates.js";
import {
    type FilterChanges,
    type Filters,
    pickerChanges,
    pickerOptions,
    pickerValue,
    searchPeriod,
} from "#lib/filters.js";
import {
    kindOrder,
    kinds,
} from "#lib/kinds.js";

/**
 * The transactions page's filters, each a change to the URL (`navigate`). `filters` are as last asked
 * for; `result` gives the counts beside them. `onleave`: Enter or ↓ in the search box, to the rows.
 */
let { filters, result, months, latestMonth, navigate, onleave }: {
    filters: Filters;
    result: TransactionSearch;
    months: Month[];
    latestMonth: Month;
    navigate: (changes: FilterChanges, options?: { replace?: boolean; }) => unknown;
    onleave: () => void;
} = $props();

// No kind filter means every kind. Toggling one off from "all" keeps the rest.
const activeKinds = $derived(new Set<TransactionKind>(filters.kinds.length > 0 ? filters.kinds : kindOrder));
function toggleKind(kind: TransactionKind) {
    const next = kindOrder.filter(k => (k === kind ? !activeKinds.has(k) : activeKinds.has(k)));
    const all = next.length === 0 || next.length === kindOrder.length;
    void navigate({ kind: all ? null : next });
}
const counts = $derived(new Map(result.byKind.map(k => [k.kind, k])));

const options = $derived(pickerOptions(result));
const picked = $derived(pickerValue(filters));
</script>

<section class="panel flex flex-col gap-2.5 p-3 md:p-3.5">
    <SearchBox asked={() => filters.q} onsearch={q => void navigate({ q: q || null }, { replace: true })} placeholder="說明或備註，例如 uber、房租" {onleave} />
    <div class="flex flex-wrap items-center gap-2">
        <MonthSelect all {months} value={searchPeriod(filters, latestMonth)} onchange={m => void navigate({ m })} />
        {#if filters.closing}
            <button type="button" class="control pressed" title="移除這個篩選" onclick={() => void navigate({ closing: null })}>
                {filters.closing} 結帳的帳單 <span class="text-mute">×</span>
            </button>
        {/if}
        <select class="control" aria-label="帳戶或卡片" value={picked} onchange={event => void navigate(pickerChanges(event.currentTarget.value))}>
            <option value="">all accounts</option>
            {#each options as option (option.value)}
                <option value={option.value}>{option.label}</option>
            {/each}
            {#if picked && !options.some(o => o.value === picked)}<option value={picked}>{picked}</option>{/if}
        </select>
        <div role="group" aria-label="交易種類" class="flex flex-wrap gap-1.5 whitespace-nowrap">
            {#each kindOrder as kind (kind)}
                {@const on = activeKinds.has(kind)}
                <button type="button" aria-pressed={on} class="control shrink-0 {on ? 'pressed' : 'text-mute'}" onclick={() => toggleKind(kind)}>
                    {kinds[kind].short} <span class="text-mute">{counts.get(kind)?.count ?? 0}</span>
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
