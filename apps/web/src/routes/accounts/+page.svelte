<script lang="ts">
import {
    type AccountSort,
    accountStatus,
    daysAgo,
    describePrice,
    isSmallAccount,
    lapsesSoon,
    leftOut,
    listedBalances,
    shownNumber,
    sortAccounts,
    totalWorth,
    unpaidBills,
} from "#lib/accounts.js";
import type { Account } from "#lib/api/client.js";
import NewAccount from "#lib/components/NewAccount.svelte";
import {
    accountKinds,
    accountName,
    institution,
} from "#lib/labels.js";
import {
    formatAmount,
    roundWhole,
    sign,
    subtract,
    sum,
} from "#lib/money.js";
import { goto } from "$app/navigation";
import { resolve } from "$app/paths";
import type { PageProps } from "./$types";

let { data }: PageProps = $props();

const hideSmall = $derived(!data.all);
// The form for an account kept by hand; not a filter, so not in the URL.
let adding = $state(false);
// For telling points that lapse soon; the page is not open across days.
// The owner's day, wherever the browser is (ADR 0004).
const today = new Date().toLocaleDateString("sv-SE", { timeZone: "Asia/Taipei" });

// By kind, in a fixed order. Card bills are no accounts the owner has and are not listed (ADR 0013).
// Small accounts are left out of the list, never out of the totals.
const groups = $derived(
    (["deposit", "securities", "exchange", "points"] as const)
        .map(kind => {
            const accounts = data.accounts.filter(a => a.kind === kind);
            const listed = hideSmall ? accounts.filter(a => !isSmallAccount(a)) : accounts;
            return {
                kind,
                accounts: sortAccounts(listed, data.sort),
                hidden: accounts.length - listed.length,
                worth: totalWorth(accounts),
                // Points until the owner gives them a value (ADR 0019).
                worthless: accounts.every(a => a.worth === undefined),
            };
        })
        .filter(group => group.accounts.length + group.hidden > 0),
);

function accountsHref(sort: AccountSort, all: boolean) {
    const query = [sort === "bank" ? "sort=bank" : "", all ? "all" : ""].filter(Boolean).join("&");
    return query ? resolve(`/accounts?${query}`) : resolve("/accounts");
}

const sorts = $derived([
    { value: "balance", label: "依餘額", href: accountsHref("balance", data.all) },
    { value: "bank", label: "依銀行", href: accountsHref("bank", data.all) },
]);

const gapped = $derived(data.accounts.filter(a => a.gaps.length > 0));
// Net of the card bills not yet seen paid (ADR 0016); a paid bill's money already left a deposit account.
const holdings = $derived(totalWorth(data.accounts));
const deposits = $derived(totalWorth(data.accounts.filter(a => a.kind === "deposit")));
const brokerages = $derived(data.accounts.filter(a => a.kind === "securities"));
const securities = $derived(totalWorth(brokerages));
const exchangeAccounts = $derived(data.accounts.filter(a => a.kind === "exchange"));
const exchanges = $derived(totalWorth(exchangeAccounts));
const unpaid = $derived(unpaidBills(data.statements));
const owed = $derived(sum(unpaid.map(s => s.totalDue)));

function href(account: Account) {
    return resolve("/accounts/[id]", { id: String(account.id) });
}
</script>

{#if gapped.length > 0}
    <section class="panel border-neg px-3.5 py-2.5">
        <h2 class="label text-neg">GAPS · 餘額接不起來（{gapped.length}）</h2>
        <p class="mt-1 text-dim">
            兩筆餘額之間有錢進出、卻沒有明細：多半是少了一期對帳單，找到那封信上傳就會接起來。點數帳戶的缺口多半是點數過期了。
        </p>
        <ul class="mt-1.5">
            {#each gapped as account (account.id)}
                <li>
                    <a href={href(account)}>{institution(account.institution).name} {accountName(account)} {shownNumber(account)}</a>
                    <span class="text-mute">· {account.gaps.length} 段</span>
                </li>
            {/each}
        </ul>
    </section>
{/if}

<section class="panel flex flex-wrap items-baseline gap-x-8 gap-y-2 px-3.5 py-2.5">
    <div>
        <div class="label">NET · 淨額</div>
        <div class="text-[20px] font-bold text-ink-bright">≈ NT${formatAmount(subtract(holdings.total, owed))}</div>
    </div>
    <div>
        <div class="label">DEPOSITS · 存款</div>
        <div>≈ NT${formatAmount(deposits.total)}{deposits.missing ? `（${deposits.missing} 個帳戶沒有價格，不計）` : ""}</div>
    </div>
    {#if brokerages.length > 0}
        <div>
            <div class="label">SECURITIES · 證券市值</div>
            <div>≈ NT${formatAmount(securities.total)}{securities.missing ? `（${securities.missing} 個帳戶有證券沒有收盤價，不計）` : ""}</div>
        </div>
    {/if}
    {#if exchangeAccounts.length > 0}
        <div>
            <div class="label">EXCHANGES · 交易所</div>
            <div>≈ NT${formatAmount(exchanges.total)}{exchanges.missing ? `（${exchanges.missing} 個交易所有幣種沒有價格、也沒有交易所估值，不計）` : ""}</div>
        </div>
    {/if}
    <div>
        <div class="label">CARDS · 未見繳款的卡費</div>
        <div class={unpaid.length > 0 ? "text-neg" : "text-dim"}>
            {unpaid.length > 0 ? `${formatAmount(owed)}（${unpaid.length} 張）` : "沒有"}
        </div>
        {#if unpaid.length > 0}
            <a href={resolve("/statements?all")} class="text-[12px] text-dim hover:text-ink">
                {unpaid.map(s => `${institution(s.institution).name} ${s.dueDate.slice(5).replace("-", "/")} 到期`).join("、")} →
            </a>
        {/if}
    </div>
</section>

<div class="flex flex-wrap items-center gap-x-4 gap-y-2 text-[12px] text-mute">
    <nav aria-label="排序" class="flex items-baseline gap-2">
        排序
        {#each sorts as sort (sort.value)}
            <a
                href={sort.href}
                aria-current={data.sort === sort.value ? "page" : undefined}
                class="rounded-[4px] border px-2 py-1 {data.sort === sort.value ? 'border-accent text-ink' : 'border-line text-dim hover:text-ink'}"
            >{sort.label}</a>
        {/each}
    </nav>
    <label class="flex min-h-9 items-center gap-1.5 text-dim" title="帳戶和餘額都算；合計照樣全部加進去">
        <input
            type="checkbox"
            class="accent-accent"
            checked={hideSmall}
            onchange={event => void goto(accountsHref(data.sort, !event.currentTarget.checked))}
        />
        隱藏 &lt; NT$1,000
    </label>
    <span>外幣以最新匯率折合新臺幣比較</span>
    {#if !adding}
        <button type="button" class="ml-auto text-dim hover:text-ink" onclick={() => (adding = true)}>＋ 新增帳戶</button>
    {/if}
</div>

{#if adding}<NewAccount onclose={() => (adding = false)} />{/if}

{#each groups as group (group.kind)}
    <section class="panel px-3.5 pt-1 pb-2.5">
        <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
            <h1 class="label">ACCOUNTS · {accountKinds[group.kind].label}（{group.accounts.length}）</h1>
            <span class="text-[12px] text-mute">
                {#if group.worthless}
                    沒有價值，不計入淨值
                {:else}
                    合計 ≈ NT${formatAmount(group.worth.total)}{group.worth.missing ? `（${group.worth.missing} 個帳戶沒有價格，不計）` : ""}
                {/if}
                {#if group.hidden > 0}· 另 {group.hidden} 個 &lt; NT$1,000 未列出{/if}
            </span>
        </div>
        <div class="overflow-x-auto">
            <!-- Fixed widths, so the tables of deposits, securities, and exchanges line up; the account takes the rest. -->
            <table class="w-full min-w-[860px] table-fixed border-collapse">
                <colgroup>
                    <col class="w-28" />
                    <col />
                    <col class="w-[22rem]" />
                    <col class="w-44" />
                </colgroup>
                <thead>
                    <tr class="text-left text-mute">
                        <th class="py-2 pr-2 font-medium">BANK</th>
                        <th class="px-2 py-2 font-medium">ACCOUNT</th>
                        <th class="px-2 py-2 text-right font-medium">BALANCE</th>
                        <th class="py-2 pl-4 font-medium">STATUS</th>
                    </tr>
                </thead>
                <tbody>
                    {#each group.accounts as account (account.id)}
                        {@const view = listedBalances(account, hideSmall)}
                        {@const rows = view.listed}
                        {@const note = leftOut(view)}
                        <!-- An account holding NT$ alone is worth its balance: no worth line of its own. -->
                        {@const ntdOnly = account.balances.length === 1 && account.balances[0].unit === "TWD"}
                        <!-- A worth line only where something has a worth: not for points no one has valued. -->
                        {@const worthLine = !ntdOnly && (account.worth !== undefined || account.balances.some(b => b.worth !== undefined))}
                        {@const lapsing = account.expiries[0]}
                        {@const status = accountStatus(account, today)}
                        <tr class="border-t border-rule align-top hover:bg-raised">
                            <td class="py-2.5 pr-2">{institution(account.institution).name}</td>
                            <td class="px-2 py-2.5">
                                <a href={href(account)}>{accountName(account)}</a>
                                {#if shownNumber(account)}<span class="text-mute">{shownNumber(account)}</span>{/if}
                            </td>
                            <td class="px-2 py-2.5 text-right">
                                {#if worthLine}
                                    <div
                                        class={account.worth !== undefined && sign(account.worth) < 0 ? "text-neg" : "text-accent-soft"}
                                        title={account.worthFrom === "valuation" ? "有幣種沒有價格：取交易所的估值" : "各項折合新臺幣加總"}
                                    >
                                        {#if account.worth !== undefined}
                                            ≈ {formatAmount(roundWhole(account.worth))} <span class="text-mute">TWD</span>
                                        {:else}
                                            無法估值
                                        {/if}
                                    </div>
                                {/if}
                                {#each rows as balance (balance.unit)}
                                    <div class={sign(balance.amount) < 0 ? "text-neg" : ntdOnly ? "text-accent-soft" : "text-ink"}>
                                        {formatAmount(balance.amount)} <span class="text-mute">{balance.unit}{balance.code ? ` ${balance.code}` : ""}</span>
                                    </div>
                                    {#if balance.unit !== "TWD" && balance.worth !== undefined}
                                        <div class="text-[12px] text-mute" title={balance.price ? describePrice(balance.unit, balance.price) : ""}>
                                            ≈ NT${formatAmount(balance.worth)}
                                        </div>
                                    {/if}
                                {/each}
                                {#if note}<div class="text-[12px] text-mute">{note}</div>{/if}
                                {#if lapsing}
                                    <div class="text-[12px] {lapsesSoon(lapsing.by, today) ? 'text-neg' : 'text-mute'}">
                                        {formatAmount(lapsing.amount)} {lapsing.unit} {lapsing.by} 前到期
                                    </div>
                                {/if}
                            </td>
                            <!-- Quiet while all is well: how old its newest balance is, then only what needs a look. -->
                            <td class="py-2.5 pl-4">
                                {#if status.days !== undefined}
                                    <div class={status.stale ? "text-accent" : "text-dim"} title="最新的餘額是 {status.asOf} 的">{daysAgo(status.days)}</div>
                                {/if}
                                {#if status.stale}
                                    <div class="text-[12px] text-accent">● 太久沒更新</div>
                                {:else if status.note}
                                    <div class="text-[12px] text-mute">{status.note}</div>
                                {/if}
                                {#if status.gaps > 0}<div class="text-[12px] text-neg">● 不連續 · {status.gaps} 段</div>{/if}
                            </td>
                        </tr>
                    {:else}
                        <tr><td colspan="4" class="py-2.5 text-mute">都不到 NT$1,000：取消勾選「隱藏 &lt; NT$1,000」就會列出。</td></tr>
                    {/each}
                </tbody>
            </table>
        </div>
    </section>
{:else}
    <section class="panel px-3.5 py-2.5 text-dim">還沒有帳戶：上傳一封帳單或對帳單，就會出現。</section>
{/each}
