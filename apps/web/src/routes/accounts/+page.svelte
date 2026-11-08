<script lang="ts">
import {
    sortAccounts,
    totalWorth,
} from "#lib/accounts.js";
import type { Account } from "#lib/api/client.js";
import {
    accountKinds,
    accountName,
    institution,
} from "#lib/labels.js";
import {
    formatAmount,
    sign,
} from "#lib/money.js";
import { resolve } from "$app/paths";
import type { PageProps } from "./$types";

let { data }: PageProps = $props();

// Deposit accounts first: their balances are checked; a card bill's are what it says is owed.
const groups = $derived(
    (["deposit", "credit_card"] as const)
        .map(kind => {
            const accounts = data.accounts.filter(a => a.kind === kind);
            return { kind, accounts: sortAccounts(accounts, data.sort), worth: totalWorth(accounts) };
        })
        .filter(group => group.accounts.length > 0),
);
const sorts = [
    { value: "bank", label: "依銀行", href: resolve("/accounts") },
    { value: "balance", label: "依餘額", href: resolve("/accounts?sort=balance") },
] as const;
const gapped = $derived(data.accounts.filter(a => a.gaps.length > 0));

function href(account: Account) {
    return resolve("/accounts/[id]", { id: String(account.id) });
}
</script>

{#if gapped.length > 0}
    <section class="panel border-neg px-3.5 py-2.5">
        <h2 class="label text-neg">GAPS · 餘額接不起來（{gapped.length}）</h2>
        <p class="mt-1 text-dim">兩筆餘額之間有錢進出、卻沒有明細：多半是少了一期對帳單。找到那封信上傳，就會接起來。</p>
        <ul class="mt-1.5">
            {#each gapped as account (account.id)}
                <li>
                    <a href={href(account)}>{institution(account.institution).name} {accountName(account)} {account.number}</a>
                    <span class="text-mute">· {account.gaps.length} 段</span>
                </li>
            {/each}
        </ul>
    </section>
{/if}

<nav aria-label="排序" class="flex items-baseline gap-2 text-[12px] text-mute">
    排序
    {#each sorts as sort (sort.value)}
        <a
            href={sort.href}
            aria-current={data.sort === sort.value ? "page" : undefined}
            class="rounded-[4px] border px-2 py-1 {data.sort === sort.value ? 'border-accent text-ink' : 'border-line text-dim hover:text-ink'}"
        >{sort.label}</a>
    {/each}
    <span>· 外幣以最新匯率折合新臺幣比較</span>
</nav>

{#each groups as group (group.kind)}
    <section class="panel px-3.5 pt-1 pb-2.5">
        <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
            <h1 class="label">ACCOUNTS · {accountKinds[group.kind].label}（{group.accounts.length}）</h1>
            <span class="text-[12px] text-mute">
                合計 ≈ NT${formatAmount(group.worth.total)}{group.worth.missing ? `（${group.worth.missing} 個帳戶沒有匯率，不計）` : ""}
                {#if group.kind === "credit_card"}· 餘額是帳單上的應繳金額，負數是欠款{/if}
            </span>
        </div>
        <div class="overflow-x-auto">
            <table class="w-full min-w-[720px] border-collapse">
                <thead>
                    <tr class="text-left text-mute">
                        <th class="py-2 pr-2 font-medium">BANK</th>
                        <th class="px-2 py-2 font-medium">ACCOUNT</th>
                        <th class="px-2 py-2 text-right font-medium">BALANCE</th>
                        <th class="px-2 py-2 font-medium">AS_OF</th>
                        <th class="py-2 pl-2 font-medium">CONTINUITY</th>
                    </tr>
                </thead>
                <tbody>
                    {#each group.accounts as account (account.id)}
                        <tr class="border-t border-rule align-top hover:bg-raised">
                            <td class="py-2.5 pr-2">{institution(account.institution).name}</td>
                            <td class="px-2 py-2.5">
                                <a href={href(account)}>{accountName(account)}</a>
                                {#if account.number}<span class="text-mute">{account.number}</span>{/if}
                            </td>
                            <td class="px-2 py-2.5 text-right">
                                {#each account.balances as balance (balance.unit)}
                                    <div class={sign(balance.amount) < 0 ? "text-neg" : "text-ink"}>
                                        {formatAmount(balance.amount)} <span class="text-mute">{balance.unit}</span>
                                    </div>
                                    {#if balance.unit !== "TWD" && balance.worth !== undefined}
                                        <div class="text-[12px] text-mute" title={`${balance.rateDay} 的匯率`}>≈ NT${formatAmount(balance.worth)}</div>
                                    {/if}
                                {:else}
                                    <span class="text-mute">—</span>
                                {/each}
                            </td>
                            <td class="px-2 py-2.5 text-dim">
                                {#each account.balances as balance (balance.unit)}
                                    <div>{balance.asOf}</div>
                                    <!-- Keeps each date beside its balance when a worth line follows the balance. -->
                                    {#if balance.unit !== "TWD" && balance.worth !== undefined}<div aria-hidden="true" class="invisible text-[12px]">·</div>{/if}
                                {/each}
                            </td>
                            <td class="py-2.5 pl-2">
                                {#if account.continuity === "unknown"}
                                    <span class="text-mute" title="信用卡帳單的明細依消費日，還無法逐期檢查">—</span>
                                {:else if account.gaps.length > 0}
                                    <span class="text-neg">缺 {account.gaps.length} 段</span>
                                {:else}
                                    <span class="text-pos">ok</span>
                                {/if}
                            </td>
                        </tr>
                    {/each}
                </tbody>
            </table>
        </div>
    </section>
{:else}
    <section class="panel px-3.5 py-2.5 text-dim">還沒有帳戶：上傳一封帳單或對帳單，就會出現。</section>
{/each}
