<script lang="ts">
import { accountPage } from "#lib/accounts.js";
import HoldingsForm from "#lib/components/HoldingsForm.svelte";
import UnitPrices from "#lib/components/UnitPrices.svelte";
import { today } from "#lib/dates.js";
import type { PageProps } from "./$types";
import AccountHeader from "./AccountHeader.svelte";
import AccountLines from "./AccountLines.svelte";
import BalanceHistory from "./BalanceHistory.svelte";
import Gaps from "./Gaps.svelte";
import Holdings from "./Holdings.svelte";
import Lapses from "./Lapses.svelte";
import Positions from "./Positions.svelte";

let { data }: PageProps = $props();

const account = $derived(data.account);
const base = $derived(data.settings.baseCurrency);
// What the page has for this kind of account (accounts.ts), rather than a branch per kind here.
const layout = $derived(accountPage(account));
</script>

<AccountHeader {account} {layout} today={today()} {base} />

<Gaps {account} explanation={layout.gaps} />

{#if layout.holdings}
    <Holdings {account} {base} intervalHours={data.settings.sourceIntervalHours} />
{/if}

{#if layout.positions}
    <Positions {account} {base} />
{/if}

{#if layout.expiring}
    <Lapses {account} today={today()} owned={layout.form} />
{/if}

{#if layout.form}
    <HoldingsForm accountId={account.id} kind={account.kind} balances={account.balances} expiries={account.expiries} {base} />
{/if}

<UnitPrices balances={account.balances} {base} />

<!-- The owner of an account they keep gives it no lines (ADR 0019). -->
{#if layout.lines}
    <AccountLines accountId={account.id} lines={data.lines} note={layout.lines.note} trade={layout.lines.trade} empty={layout.lines.empty} />
{/if}

<BalanceHistory {account} by={layout.history} />
