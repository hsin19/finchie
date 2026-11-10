<script lang="ts">
import {
    accountStatus,
    daysAgo,
    isOwned,
} from "#lib/accounts.js";
import type { Account } from "#lib/api/client.js";

/**
 * How current an account is and whether it joins up: quiet while all is well, then only what needs a
 * look. `full`, on the account's own page, also says when it joins up and when it cannot be checked.
 */
let { account, today, full = false }: { account: Account; today: string; full?: boolean; } = $props();

const status = $derived(accountStatus(account, today));
</script>

{#if status.days !== undefined}
    <div class={status.stale ? "text-accent" : full ? "text-ink" : "text-dim"} title="最新的餘額是 {status.asOf} 的">{daysAgo(status.days)}{full ? "更新" : ""}</div>
{/if}
{#if status.stale}
    <div class="text-[12px] text-accent">● 太久沒更新</div>
{:else if status.note}
    <div class="text-[12px] text-mute">{status.note}</div>
{/if}
{#if status.gaps > 0}
    <div class="text-[12px] text-neg">● 不連續 · {status.gaps} 段</div>
{:else if full && status.checked}
    <div class="text-[12px] text-pos">● 每一期都接得起來</div>
{:else if full && !isOwned(account)}
    <div class="text-[12px] text-mute">來源只給餘額、沒有明細，接不起來</div>
{/if}
