<script lang="ts">
import {
    accountName,
    type AccountPage,
    describePrice,
    holdingsByWorth,
    shownNumber,
} from "#lib/accounts.js";
import type { AccountDetail } from "#lib/api/client.js";
import AccountStatus from "#lib/components/AccountStatus.svelte";
import { institution } from "#lib/institutions.js";
import { accountKinds } from "#lib/kinds.js";
import {
    formatAmount,
    sign,
} from "#lib/money.js";
import { resolve } from "$app/paths";
import AccountNames from "./AccountNames.svelte";

/** An account's name, what it is worth or holds, its kind, how current it is, and how its statements get here. */
let { account, layout, today, base }: { account: AccountDetail; layout: AccountPage; today: string; base: string; } = $props();

const bank = $derived(institution(account.institution));
const holdings = $derived(holdingsByWorth(account.balances));
const unpriced = $derived(account.balances.filter(b => b.worth === undefined).length);
const held = $derived((account.positions ?? []).filter(p => sign(p.shares) !== 0).length);
// Where the owner has a part in how its statements get here.
const source = $derived(bank.source?.[account.kind]);
let editing = $state(false);
</script>

<section class="panel px-3.5 py-2.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2">
        <h1 class="text-[16px] font-bold">
            {bank.name} {accountName(account)}
            {#if shownNumber(account)}<span class="font-normal text-mute">{shownNumber(account)}</span>{/if}
        </h1>
        <span class="flex gap-3 text-[12px]">
            {#if !editing}<button type="button" class="text-dim hover:text-ink" onclick={() => (editing = true)}>編輯名稱與帳號</button>{/if}
            <a href={resolve("/accounts")} class="text-dim hover:text-ink">← 全部帳戶</a>
        </span>
    </div>
    {#if editing}<AccountNames {account} onclose={() => (editing = false)} />{/if}
    <div class="mt-2 flex flex-wrap gap-x-8 gap-y-2">
        {#if layout.headline === "worth"}
            <div>
                <div class="label">WORTH · 市值</div>
                <div class="text-[18px] text-ink">{account.worth !== undefined ? `≈ NT$${formatAmount(account.worth)}` : layout.noWorth}</div>
                <div class="text-[12px] text-mute">
                    {#if account.worthFrom === "valuation" && account.valuation}
                        取交易所估值 {formatAmount(account.valuation.amount)} {account.valuation.unit}：{unpriced} 種沒有價格
                    {:else if layout.positions}
                        {held} 檔，股數到 {account.balances[0]?.asOf ?? "—"} 為止
                    {:else}
                        {holdings.length} 種，到 {account.balances[0]?.asOf ?? "—"} 為止
                    {/if}
                </div>
            </div>
        {:else}
            {#each holdings as balance (balance.unit)}
                <div>
                    <div class="label">BALANCE · {balance.unit}</div>
                    <div class="text-[18px] {sign(balance.amount) < 0 ? 'text-neg' : 'text-ink'}">{formatAmount(balance.amount)}</div>
                    {#if balance.unit !== base && balance.worth !== undefined}
                        <div class="text-[12px] text-mute" title={balance.price ? describePrice(balance.unit, balance.price, base) : ""}>
                            ≈ NT${formatAmount(balance.worth)}{balance.price?.source === "owner" ? "（自訂價格）" : ""}
                        </div>
                    {/if}
                    <div class="text-[12px] text-mute">{balance.asOf} 為止</div>
                </div>
            {/each}
        {/if}
        <div>
            <div class="label">KIND</div>
            <div>{accountKinds[account.kind].label}</div>
        </div>
        <div>
            <div class="label">STATUS · 狀態</div>
            <AccountStatus {account} {today} full />
        </div>
    </div>
    {#if source}
        <p class="mt-2.5 border-t border-rule pt-2 text-[12px] text-dim">來源流程：{source}</p>
    {/if}
</section>
