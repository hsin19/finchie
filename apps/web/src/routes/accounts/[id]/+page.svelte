<script lang="ts">
import {
    describePrice,
    historyByDay,
    holdingsByWorth,
    shownNumber,
} from "#lib/accounts.js";
import {
    api,
    LEDGER,
    type Trade,
    unwrap,
} from "#lib/api/client.js";
import Counterpart from "#lib/components/Counterpart.svelte";
import HoldingsForm from "#lib/components/HoldingsForm.svelte";
import UnitPrices from "#lib/components/UnitPrices.svelte";
import {
    accountKinds,
    accountName,
    institution,
} from "#lib/labels.js";
import {
    approx,
    formatAmount,
    negate,
    sign,
} from "#lib/money.js";
import { invalidate } from "$app/navigation";
import { resolve } from "$app/paths";
import type { PageProps } from "./$types";

let { data }: PageProps = $props();

const account = $derived(data.account);
const bank = $derived(institution(account.institution));
// A brokerage account holds shares, one unit per security, and its lines are trades (ADR 0017).
const brokerage = $derived(account.kind === "securities");
// An exchange account holds coins and reports nothing but snapshots of them (ADR 0018).
const exchange = $derived(account.kind === "exchange");
// A points account's bills print its programs' balances, their totals as lines, and what will lapse (ADR 0019).
const points = $derived(account.kind === "points");
// One the owner opened and keeps by hand: its number is its unit (ADR 0019).
const owned = $derived(account.institution === "owner");
const holdings = $derived(holdingsByWorth(account.balances));
const unpriced = $derived(account.balances.filter(b => b.worth === undefined).length);
const snapshots = $derived(historyByDay(account.history));

/** What a trade paid or received, or what was paid in advance for it. */
function tradeMoney(trade: Trade): string {
    if (trade.prepaid) return `預繳 ${formatAmount(trade.prepaid)}`;
    return sign(trade.net) < 0 ? `付 ${formatAmount(negate(trade.net))}` : `收 ${formatAmount(trade.net)}`;
}

// Held first, then what was sold out; cost and gains only where known (ADR 0017).
const held = $derived((account.positions ?? []).filter(p => sign(p.shares) !== 0));
// Sold out: worth showing only for what a sale is known to have gained or lost.
const soldOut = $derived((account.positions ?? []).filter(p => sign(p.shares) === 0 && p.realized !== undefined && sign(p.realized) !== 0));
const closeOf = $derived(new Map(account.balances.map(b => [b.unit, b])));

/** Gain as a share of cost, for showing only. */
function percent(gain: string, cost: string): string {
    return approx(cost) > 0 ? `${((approx(gain) / approx(cost)) * 100).toFixed(1)}%` : "";
}

const unknownCost = "最舊那期之前就有，或之後有缺口：成本不知道";

// The owner's details (ADR 0016): optional, and an empty field clears one.
let editing = $state(false);
let name = $state("");
let fullNumber = $state("");
let saveError = $state<string>();

function edit() {
    name = account.name ?? "";
    fullNumber = account.fullNumber ?? "";
    saveError = undefined;
    editing = true;
}

async function save(event: SubmitEvent) {
    event.preventDefault();
    try {
        unwrap(await api().PATCH("/api/accounts/{id}", { params: { path: { id: account.id } }, body: { name, fullNumber } }));
        editing = false;
        await invalidate(LEDGER);
    } catch (error) {
        saveError = (error as Error).message;
    }
}

function amountTone(value: string) {
    return sign(value) < 0 ? "text-neg" : sign(value) > 0 ? "text-pos" : "text-dim";
}
</script>

<section class="panel px-3.5 py-2.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2">
        <h1 class="text-[16px] font-bold">
            {bank.name} {accountName(account)}
            {#if shownNumber(account)}<span class="font-normal text-mute">{shownNumber(account)}</span>{/if}
        </h1>
        <span class="flex gap-3 text-[12px]">
            {#if !editing}<button type="button" class="text-dim hover:text-ink" onclick={edit}>編輯名稱與帳號</button>{/if}
            <a href={resolve("/accounts")} class="text-dim hover:text-ink">← 全部帳戶</a>
        </span>
    </div>
    {#if editing}
        <form class="mt-2 flex flex-wrap items-end gap-3" onsubmit={save}>
            <label class="flex flex-col gap-1 text-[12px] text-mute">
                名稱（空白則用「{account.label ?? accountKinds[account.kind].label}」）
                <input class="control min-h-9 w-56 text-ink" bind:value={name} placeholder={account.label ?? ""} />
            </label>
            <label class="flex flex-col gap-1 text-[12px] text-mute">
                {account.number ? `完整帳號（要和 ${account.number} 露出的數字對得上）` : "帳號"}
                <input class="control min-h-9 w-56 text-ink" bind:value={fullNumber} inputmode={account.number ? "numeric" : "text"} placeholder={account.number} />
            </label>
            <button type="submit" class="control min-h-9 border-accent px-3">儲存</button>
            <button type="button" class="control min-h-9 px-3" onclick={() => (editing = false)}>取消</button>
            {#if saveError}<span class="w-full text-[12px] text-neg">{saveError}</span>{/if}
        </form>
    {/if}
    <div class="mt-2 flex flex-wrap gap-x-8 gap-y-2">
        {#if brokerage}
            <div>
                <div class="label">WORTH · 市值</div>
                <div class="text-[18px] text-ink">{account.worth !== undefined ? `≈ NT$${formatAmount(account.worth)}` : "有證券沒有收盤價"}</div>
                <div class="text-[12px] text-mute">{held.length} 檔，股數到 {account.balances[0]?.asOf ?? "—"} 為止</div>
            </div>
        {:else if exchange}
            <div>
                <div class="label">WORTH · 市值</div>
                <div class="text-[18px] text-ink">{account.worth !== undefined ? `≈ NT$${formatAmount(account.worth)}` : "有幣種沒有價格"}</div>
                <div class="text-[12px] text-mute">
                    {#if account.worthFrom === "valuation" && account.valuation}
                        取交易所估值 {formatAmount(account.valuation.amount)} {account.valuation.unit}：{unpriced} 種沒有價格
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
                    {#if balance.unit !== "TWD" && balance.worth !== undefined}
                        <div class="text-[12px] text-mute" title={balance.price ? describePrice(balance.unit, balance.price) : ""}>
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
            <div class="label">CONTINUITY</div>
            {#if account.continuity === "unknown"}
                <div class="text-mute">無法檢查</div>
                <div class="text-[12px] text-mute">它的來源只給餘額、沒有明細，接不起來</div>
            {:else if account.gaps.length > 0}
                <div class="text-neg">缺 {account.gaps.length} 段</div>
            {:else}
                <div class="text-pos">ok · 每一期都接得起來</div>
            {/if}
        </div>
    </div>
    {#if account.institution === "cathay" && account.kind === "deposit"}
        <p class="mt-2.5 border-t border-rule pt-2 text-[12px] text-dim">
            來源流程：<span class="text-ink">CUBE App → 更多 → 臺幣 → 匯出臺幣電子存摺</span> → Gmail 篩選器自動轉寄 → 解析入帳（單次上限 220 筆；期間和之前匯出的重疊沒關係，重疊的日子以結束日較晚的那份為準）。
        </p>
    {/if}
</section>

{#if account.gaps.length > 0}
    <section class="panel border-neg px-3.5 py-2.5">
        <h2 class="label text-neg">GAPS · 餘額接不起來</h2>
        <p class="mt-1 text-dim">
            {
                points
                ? "這兩期之間少了點數，帳單卻沒列出：多半是點數過期了（過期的點數不會出現在任何欄位），或少了一期帳單。"
                : "這兩個日期之間有錢進出、卻沒有任何明細：多半是少了一期對帳單。上傳那期之後就會接起來。"
            }
        </p>
        <table class="mt-1.5 border-collapse">
            <tbody>
                {#each account.gaps as gap (`${gap.unit}:${gap.to}`)}
                    <tr class="border-t border-rule">
                        <td class="py-1.5 pr-4">{gap.from} → {gap.to}</td>
                        <td class="py-1.5 pr-2 text-right {amountTone(gap.unseen)}">{formatAmount(gap.unseen, { signed: true })}</td>
                        <td class="py-1.5 text-mute">{gap.unit} 沒有明細</td>
                    </tr>
                {/each}
            </tbody>
        </table>
    </section>
{/if}

{#if exchange}
    <section class="panel px-3.5 pt-1 pb-2.5">
        <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
            <h2 class="label">HOLDINGS · 持有（{holdings.length}）</h2>
            <span class="text-[12px] text-mute">每 6 小時從交易所 API 同步 · 參考價為每日匯率</span>
        </div>
        <table class="border-collapse">
            <thead>
                <tr class="text-left text-mute">
                    <th class="py-2 pr-4 font-medium">COIN</th>
                    <th class="px-2 py-2 text-right font-medium">AMOUNT</th>
                    <th class="py-2 pl-4 text-right font-medium">≈ NT$</th>
                </tr>
            </thead>
            <tbody>
                {#each holdings as balance (balance.unit)}
                    <tr class="border-t border-rule">
                        <td class="py-1.5 pr-4">{balance.unit}</td>
                        <td class="px-2 py-1.5 text-right">{formatAmount(balance.amount)}</td>
                        <td class="py-1.5 pl-4 text-right text-dim" title={balance.price ? describePrice(balance.unit, balance.price) : ""}>
                            {balance.worth !== undefined ? formatAmount(balance.worth) : "沒有價格"}
                        </td>
                    </tr>
                {:else}
                    <tr><td colspan="3" class="py-2.5 text-mute">目前沒有持有任何幣。</td></tr>
                {/each}
            </tbody>
        </table>
    </section>
{/if}

{#if brokerage}
    <section class="panel px-3.5 pt-1 pb-2.5">
        <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
            <h2 class="label">POSITIONS · 持股與損益</h2>
            <span class="text-[12px] text-mute">平均成本，含手續費；賣出扣手續費和稅</span>
        </div>
        <div class="overflow-x-auto">
            <table class="w-full min-w-[760px] border-collapse">
                <thead>
                    <tr class="text-left text-mute">
                        <th class="py-2 pr-2 font-medium">SECURITY</th>
                        <th class="px-2 py-2 text-right font-medium">SHARES</th>
                        <th class="px-2 py-2 text-right font-medium">CLOSE</th>
                        <th class="px-2 py-2 text-right font-medium">WORTH</th>
                        <th class="px-2 py-2 text-right font-medium">COST</th>
                        <th class="px-2 py-2 text-right font-medium">GAIN</th>
                        <th class="py-2 pl-2 text-right font-medium">REALIZED</th>
                    </tr>
                </thead>
                <tbody>
                    {#each [...held, ...soldOut] as position (position.unit)}
                        {@const close = closeOf.get(position.unit)}
                        <tr class="border-t border-rule">
                            <td class="py-2 pr-2">{position.unit} <span class="text-mute">{position.code ?? ""}</span></td>
                            <td class="px-2 py-2 text-right">{sign(position.shares) === 0 ? "出清" : formatAmount(position.shares)}</td>
                            <td class="px-2 py-2 text-right text-dim" title={close?.price ? describePrice(close.unit, close.price) : ""}>
                                {close?.price ? formatAmount(close.price.quoted) : ""}
                            </td>
                            <td class="px-2 py-2 text-right">{sign(position.shares) === 0 ? "" : position.worth !== undefined ? formatAmount(position.worth) : "—"}</td>
                            <td class="px-2 py-2 text-right text-dim" title={position.cost !== undefined ? `${position.since} 起的成交` : unknownCost}>
                                {position.cost !== undefined ? formatAmount(position.cost) : sign(position.shares) === 0 ? "" : "不知道"}
                            </td>
                            <td class="px-2 py-2 text-right {position.gain ? (sign(position.gain) < 0 ? 'text-neg' : 'text-pos') : 'text-mute'}">
                                {#if position.gain !== undefined && position.cost !== undefined}
                                    {formatAmount(position.gain, { signed: true })} <span class="text-[12px]">{percent(position.gain, position.cost)}</span>
                                {/if}
                            </td>
                            <td class="py-2 pl-2 text-right {position.realized ? (sign(position.realized) < 0 ? 'text-neg' : 'text-pos') : 'text-mute'}">
                                {position.realized !== undefined && sign(position.realized) !== 0 ? formatAmount(position.realized, { signed: true }) : ""}
                            </td>
                        </tr>
                    {:else}
                        <tr><td colspan="7" class="py-2.5 text-mute">目前沒有持股。</td></tr>
                    {/each}
                </tbody>
            </table>
        </div>
    </section>
{/if}

{#if points}
    <section class="panel px-3.5 pt-1 pb-2.5">
        <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
            <h2 class="label">EXPIRING · 即將到期</h2>
            <span class="text-[12px] text-mute">{owned ? "你輸入的" : "照最新一期帳單 · 同一行可能是幾個月加起來"}</span>
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
                    <tr><td class="py-2.5 text-mute">{owned ? "沒有輸入到期的點數。" : "帳單上沒有即將到期的點數。"}</td></tr>
                {/each}
            </tbody>
        </table>
    </section>
{/if}

{#if owned}
    <HoldingsForm accountId={account.id} kind={account.kind} balances={account.balances} expiries={account.expiries} />
{/if}

<UnitPrices balances={account.balances} />

<!-- An exchange reports no lines at all (ADR 0018), nor does the owner of an account they keep (ADR 0019). -->
{#if !exchange && !owned}
    <section class="panel px-3.5 pt-1 pb-2.5">
        <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
            <h2 class="label">LINES · 明細（{data.lines.length}）</h2>
            <span class="text-[12px] text-mute">新的在上 · {brokerage ? "買進的股數是正數" : points ? "帳單上每一期的合計，獲得是正數" : "存入是正數"}</span>
        </div>
        <div class="overflow-x-auto">
            <table class="w-full min-w-[820px] border-collapse">
                <thead>
                    <tr class="text-left text-mute">
                        <th class="py-2 pr-2 font-medium">DATE</th>
                        <th class="px-2 py-2 font-medium">DESCRIPTION</th>
                        <th class="px-2 py-2 font-medium">{brokerage ? "TRADE" : "MEMO"}</th>
                        <th class="px-2 py-2 font-medium">{brokerage ? "MONEY" : "COUNTERPARTY"}</th>
                        <th class="px-2 py-2 text-right font-medium">AMOUNT</th>
                        <th class="py-2 pl-2 text-right font-medium">BALANCE</th>
                    </tr>
                </thead>
                <tbody>
                    {#each data.lines as line (line.id)}
                        <tr class="border-t border-rule hover:bg-raised">
                            <td class="py-2 pr-2 whitespace-nowrap text-dim">{line.date}</td>
                            <td class="px-2 py-2">
                                {line.description}
                                {#if line.counterpart}<Counterpart other={line.counterpart} out={sign(line.amount) < 0} class="block text-[12px] text-dim hover:text-ink" />{/if}
                            </td>
                            {#if line.trade}
                                <td class="px-2 py-2 text-dim">{line.trade.category} @ {formatAmount(line.trade.price)}</td>
                                <td class="px-2 py-2 whitespace-nowrap text-mute" title={`價金 ${line.trade.amount} · 手續費 ${line.trade.fee} · 稅 ${line.trade.tax}`}>
                                    {tradeMoney(line.trade)}
                                </td>
                            {:else}
                                <td class="px-2 py-2 text-dim">{line.memo ?? ""}</td>
                                <td class="px-2 py-2 text-mute">{line.counterparty ?? ""}</td>
                            {/if}
                            <td class="px-2 py-2 text-right whitespace-nowrap {amountTone(line.amount)}">
                                {formatAmount(line.amount, { signed: true })} <span class="text-mute">{line.unit}</span>
                            </td>
                            <td class="py-2 pl-2 text-right whitespace-nowrap text-dim">{line.balance ? formatAmount(line.balance) : ""}</td>
                        </tr>
                    {:else}
                        <tr><td colspan="6" class="py-2.5 text-mute">這個帳戶還沒有明細：對帳單只印了它的餘額。</td></tr>
                    {/each}
                </tbody>
            </table>
        </div>
    </section>
{/if}

<section class="panel px-3.5 pt-1 pb-2.5">
    <h2 class="label py-2.5">HISTORY · {exchange ? "每天的快照" : "每一期的餘額"}</h2>
    {#if exchange}
        <table class="border-collapse">
            <tbody>
                {#each snapshots as day (day.asOf)}
                    <tr class="border-t border-rule align-top">
                        <td class="py-1.5 pr-4 whitespace-nowrap text-dim">{day.asOf}</td>
                        <td class="py-1.5">
                            {#each day.balances as balance, i (balance.unit)}
                                {i > 0 ? " · " : ""}<span class={sign(balance.amount) === 0 ? "text-mute" : "text-ink"}>{formatAmount(balance.amount)}</span>
                                <span class="text-mute">{balance.unit}</span>
                            {/each}
                        </td>
                    </tr>
                {/each}
            </tbody>
        </table>
    {:else}
        <table class="border-collapse">
            <tbody>
                {#each account.history as balance (`${balance.statementId ?? ""}:${balance.unit}:${balance.asOf}`)}
                    <tr class="border-t border-rule">
                        <td class="py-1.5 pr-4 text-dim">{balance.asOf}</td>
                        <td class="py-1.5 pr-2 text-right {sign(balance.amount) < 0 ? 'text-neg' : 'text-ink'}">{formatAmount(balance.amount)}</td>
                        <td class="py-1.5 pr-4 text-mute">{balance.unit}</td>
                    </tr>
                {/each}
            </tbody>
        </table>
    {/if}
</section>
