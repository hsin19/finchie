<script lang="ts">
import { setCheckAgain } from "#lib/api/mutations.js";
import { addDays } from "#lib/dates.js";

/**
 * Confirms an overdue account is current: no reminder until `suggested`, as long from today as its source
 * may take. With `checkAgainOn`, on the account's page, the day it was given, to put off further (far off
 * for an account that never moves) or take back.
 */
let { id, suggested, checkAgainOn, today }: { id: number; suggested: string; checkAgainOn?: string; today?: string; } = $props();
let busy = $state(false);
let error = $state<string>();

async function save(on: string) {
    busy = true;
    const outcome = await setCheckAgain(id, on);
    busy = false;
    error = outcome.error;
}
</script>

{#if checkAgainOn === undefined}
    <button
        type="button"
        class="text-[12px] text-accent hover:text-accent-soft"
        title="沒有交易的月份銀行不寄對帳單：確認後先不提醒，到期還沒有新的對帳單再提醒"
        disabled={busy}
        onclick={() => void save(suggested)}
    >
        {busy ? "儲存中…" : "確認沒變"}
    </button>
{:else}
    <span class="flex flex-wrap items-center gap-x-2 gap-y-1 text-[12px] text-dim">
        <label class="flex items-center gap-1.5">
            再提醒
            <input
                type="date"
                class="control min-h-7 px-1.5 text-[12px]"
                value={checkAgainOn}
                min={today ? addDays(today, 1) : undefined}
                disabled={busy}
                onchange={event => event.currentTarget.value && void save(event.currentTarget.value)}
            />
        </label>
        <button type="button" class="text-dim hover:text-ink" disabled={busy} onclick={() => void save("")}>取消確認</button>
    </span>
{/if}
{#if error}<span class="text-[12px] text-neg">{error}</span>{/if}
