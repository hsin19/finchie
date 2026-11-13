<script lang="ts">
import { needs } from "#lib/attention.js";
import {
    byClosingMonth,
    dueBills,
    pendingInstitutions,
    percentChange,
    totalDue,
} from "#lib/bills.js";
import {
    monthFlows,
    otherUnits,
    reportedThrough,
    yearFlow,
} from "#lib/cashflow.js";
import CashflowChart from "#lib/components/CashflowChart.svelte";
import Needs from "#lib/components/Needs.svelte";
import {
    type Month,
    shiftMonth,
    slashDate,
    thisMonth,
    today,
} from "#lib/dates.js";
import { institutionNames } from "#lib/institutions.js";
import {
    formatAmount,
    roundWhole,
    sign,
} from "#lib/money.js";
import { netWorthChange } from "#lib/networth.js";
import { statementsHref } from "#lib/urls.js";
import { resolve } from "$app/paths";
import type { PageProps } from "./$types";

let { data }: PageProps = $props();

// The page is not open across days.
const now = thisMonth();
const day = today();
const lastMonth = shiftMonth(now, -1);
const monthNumber = (m: Month) => Number(m.slice(5));

// What needs the owner comes first, and only when there is something.
const found = $derived(needs({ byState: data.documentsByState, accounts: data.accounts, today: day }));

// This month's card bills and last month's, and what is still due.
const bills = $derived(byClosingMonth(data.statements));
const billOf = (m: Month) => bills.get(m) ?? [];
const current = $derived(billOf(now));
const pending = $derived(pendingInstitutions(data.statements, now, lastMonth));
const last = $derived(billOf(lastMonth));
const lastChange = $derived(percentChange(totalDue(last), totalDue(billOf(shiftMonth(now, -2)))));
const due = $derived(dueBills(data.statements, day));

// Net worth, and how it moved since last month's end.
const net = $derived(data.netWorth.net);
const moved = $derived(netWorthChange(data.history ?? []));

// A year of what came in and went out, across every account.
const base = $derived(data.settings.baseCurrency);
const flows = $derived(monthFlows(data.months, data.searches, base, reportedThrough(data.accounts, day)));
const year = $derived({ name: now.slice(0, 4), ...yearFlow(flows, now.slice(0, 4)) });
const others = $derived(otherUnits(data.searches, base));
</script>

{#if found.length > 0}<Needs needs={found} />{/if}

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
        <span class="text-[20px] font-bold md:text-[24px] {sign(due.amount) > 0 ? 'text-ink-bright' : 'text-mute'}">{formatAmount(due.amount)}</span>
        <span class="text-[11px] text-dim">{due.next ? `${slashDate(due.next)} 到期 · ${due.count} 張` : "沒有待繳帳單"}</span>
    </div>
    <!-- An estimate (ADR 0023): sources report on days of their own. -->
    <a href={resolve("/accounts")} class="panel flex flex-col gap-1 p-3 text-ink hover:border-line-strong hover:text-ink md:p-3.5">
        <span class="text-[10px] tracking-[0.12em] text-mute">NET · 淨值</span>
        <span class="text-[20px] font-bold md:text-[24px] {sign(net) < 0 ? 'text-neg' : 'text-ink-bright'}">≈ {formatAmount(roundWhole(net))}</span>
        <span class="text-[11px] {moved === undefined ? 'text-mute' : sign(moved) < 0 ? 'text-neg' : 'text-pos'}">
            {moved === undefined ? "資產減負債" : `${formatAmount(roundWhole(moved), { signed: true })} vs ${monthNumber(lastMonth)} 月底`}
        </span>
    </a>
</div>

<CashflowChart {flows} {year} {others} />
