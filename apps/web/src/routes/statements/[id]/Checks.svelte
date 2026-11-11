<script lang="ts">
import type { Statement } from "#lib/api/client.js";
import { formatAmount } from "#lib/money.js";
import {
    checkDelta,
    deltaMeaning,
} from "#lib/statements.js";

/**
 * Every figure the bill prints that its lines are checked against (ADR 0021): those that do not add up
 * first, each with what it means; then those that do, one line each.
 */
let { statement }: { statement: Pick<Statement, "checks" | "currency">; } = $props();

const failing = $derived(statement.checks.filter(c => !c.balanced));
const passing = $derived(statement.checks.filter(c => c.balanced));
const unit = (u: string) => (u && u !== statement.currency ? u : "");
</script>

{#each failing as check, i (i)}
    {@const delta = checkDelta(check)}
    <div class="flex flex-col gap-1.5 rounded-[4px] border border-neg px-3 py-2">
        <div class="font-bold text-neg">{check.name} <span class="font-normal text-mute">{unit(check.unit)}</span></div>
        <div class="grid grid-cols-[auto_1fr_auto] gap-x-4 gap-y-1">
            <span class="text-mute">stated</span><span>帳單上印的</span><span class="text-right">{formatAmount(check.stated)}</span>
            <span class="text-mute">computed</span><span>明細加總</span><span class="text-right">{formatAmount(check.computed)}</span>
            <span class="text-neg">Δ</span><span class="text-neg">{deltaMeaning(delta)}</span><span class="text-right text-neg">{formatAmount(delta, { signed: true })}</span>
        </div>
    </div>
{/each}
{#if passing.length > 0}
    <div class="grid grid-cols-[auto_1fr_auto] gap-x-4 gap-y-1">
        {#each passing as check, i (i)}
            <span class="text-pos">✓</span>
            <span>{check.name} <span class="text-mute">{unit(check.unit)}</span></span>
            <span class="text-right text-dim">{formatAmount(check.stated)}</span>
        {/each}
    </div>
{/if}
{#if statement.checks.length === 0}
    <p class="m-0 text-mute">這張帳單沒有可以對的數字。</p>
{/if}
