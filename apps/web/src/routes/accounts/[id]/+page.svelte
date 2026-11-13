<script lang="ts">
import { accountPage } from "#lib/accounts.js";
import HoldingsForm from "#lib/components/HoldingsForm.svelte";
import UnitPrices from "#lib/components/UnitPrices.svelte";
import { today } from "#lib/dates.js";
import { formatAmount } from "#lib/money.js";
import type { PageProps } from "./$types";
import AccountHeader from "./AccountHeader.svelte";
import AccountLines from "./AccountLines.svelte";
import BalanceHistory from "./BalanceHistory.svelte";
import Gaps from "./Gaps.svelte";
import Holdings from "./Holdings.svelte";
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
    <section class="panel px-3.5 pt-1 pb-2.5">
        <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
            <h2 class="label">EXPIRING · 即將到期</h2>
            <span class="text-[12px] text-mute">{layout.form ? "你輸入的" : "照最新一期帳單 · 同一行可能是幾個月加起來"}</span>
        </div>
        <table class="border-collapse">
            <tbody>
                {#each account.expiries as expiry (`${expiry.unit}:${expiry.by}`)}
                    <tr class="border-t border-rule">
                        <td class="py-1.5 pr-4 text-dim">{expiry.by} 前</td>
                        <td class="py-1.5 pr-2 text-right text-ink">{formatAmount(expiry.amount)}</td>
                        <td class="py-1.5 text-mute">{expiry.unit}</td>
                    </tr>
                {:else}
                    <tr><td class="py-2.5 text-mute">{layout.form ? "沒有輸入到期的點數。" : "帳單上沒有即將到期的點數。"}</td></tr>
                {/each}
            </tbody>
        </table>
    </section>
{/if}

{#if layout.form}
    <HoldingsForm accountId={account.id} kind={account.kind} balances={account.balances} expiries={account.expiries} {base} />
{/if}

<UnitPrices balances={account.balances} {base} />

<!-- An exchange reports no lines at all (ADR 0018), nor does the owner of an account they keep (ADR 0019). -->
{#if layout.lines}
    <AccountLines accountId={account.id} lines={data.lines} note={layout.lines.note} trade={layout.lines.trade} />
{/if}

<BalanceHistory {account} by={layout.history} />
