<script lang="ts">
import type {
    AccountLine,
    Trade,
} from "#lib/api/client.js";
import {
    setFillMemo,
    unfillGap,
} from "#lib/api/mutations.js";
import Counterpart from "#lib/components/Counterpart.svelte";
import {
    formatAmount,
    negate,
    sign,
} from "#lib/money.js";

/**
 * An account's lines with their running balances; a trade's price and the money paid or received (ADR 0017).
 * The owner's fill of a gap is one of them, for the whole period no statement covers, as a passbook prints
 * one for what went unprinted: its memo is written, and it is taken back, here.
 */
let { accountId, lines, note, trade, empty }: { accountId: number; lines: AccountLine[]; note: string; trade: boolean; empty: string; } = $props();

const lineKey = (line: AccountLine) => line.fill ? `fill:${line.fill.id}` : `line:${line.id}`;

/** The fill whose request is on its way, and why the last one failed. */
let busy = $state<number>();
let failed = $state<{ id: number; error: string; }>();
/** The fill whose memo is being written, and what is written so far. */
let editing = $state<{ id: number; memo: string; }>();

async function run(id: number, request: () => Promise<{ error?: string; }>): Promise<boolean> {
    if (busy !== undefined) return false;
    busy = id;
    failed = undefined;
    const { error } = await request();
    busy = undefined;
    if (error) failed = { id, error };
    return !error;
}

function edit(id: number, memo: string | null | undefined) {
    failed = undefined;
    editing = { id, memo: memo ?? "" };
}

async function saveMemo() {
    if (!editing) return;
    const { id, memo } = editing;
    if (await run(id, () => setFillMemo(accountId, id, memo))) editing = undefined;
}

// Opened by the owner's own click, to type in at once.
const focus = (input: HTMLInputElement) => input.focus();

function onMemoKeydown(event: KeyboardEvent) {
    if (event.isComposing) return;
    if (event.key === "Enter") {
        event.preventDefault();
        void saveMemo();
    } else if (event.key === "Escape") {
        editing = undefined;
    }
}

/** What a trade paid or received, or what was paid in advance for it. */
function tradeMoney(t: Trade): string {
    if (t.prepaid) return `預繳 ${formatAmount(t.prepaid)}`;
    return sign(t.net) < 0 ? `付 ${formatAmount(negate(t.net))}` : `收 ${formatAmount(t.net)}`;
}

function amountTone(value: string) {
    return sign(value) < 0 ? "text-neg" : sign(value) > 0 ? "text-pos" : "text-dim";
}
</script>

<section class="panel px-3.5 pt-1 pb-2.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
        <h2 class="label">LINES · 明細（{lines.length}）</h2>
        <span class="text-[12px] text-mute">新的在上 · {note}</span>
    </div>
    <div class="overflow-x-auto">
        <table class="w-full min-w-[820px] border-collapse">
            <thead>
                <tr class="text-left text-mute">
                    <th class="py-2 pr-2 font-medium">DATE</th>
                    <th class="px-2 py-2 font-medium">DESCRIPTION</th>
                    <th class="px-2 py-2 font-medium">{trade ? "TRADE" : "MEMO"}</th>
                    <th class="px-2 py-2 font-medium">{trade ? "MONEY" : "COUNTERPARTY"}</th>
                    <th class="px-2 py-2 text-right font-medium">AMOUNT</th>
                    <th class="py-2 pl-2 text-right font-medium">BALANCE</th>
                </tr>
            </thead>
            <tbody>
                {#each lines as line (lineKey(line))}
                    {#if line.fill}
                        {@const fill = line.fill}
                        <tr class="border-t border-dashed border-line-strong bg-raised/40">
                            <td class="py-2 pr-2 whitespace-nowrap text-dim">{line.date}</td>
                            <td class="px-2 py-2">
                                人工補帳
                                <span class="block text-[12px] text-mute">{fill.from} → {fill.to} 沒有帳單</span>
                            </td>
                            <td class="px-2 py-2 text-dim">
                                {#if editing?.id === fill.id}
                                    <input
                                        type="text"
                                        class="control w-full min-w-48"
                                        placeholder="例如：那幾期的信被刪了"
                                        aria-label="備註"
                                        {@attach focus}
                                        bind:value={editing.memo}
                                        onkeydown={onMemoKeydown}
                                    />
                                {:else}
                                    {line.memo ?? ""}
                                {/if}
                            </td>
                            <td class="px-2 py-2 whitespace-nowrap">
                                {#if editing?.id === fill.id}
                                    <button type="button" class="mr-3 text-accent hover:text-accent-soft" disabled={busy !== undefined} onclick={() => void saveMemo()}>
                                        {busy === fill.id ? "儲存中…" : "儲存"}
                                    </button>
                                    <button type="button" class="text-dim hover:text-ink" onclick={() => (editing = undefined)}>取消</button>
                                {:else}
                                    <button type="button" class="mr-3 text-dim hover:text-ink" disabled={busy !== undefined} onclick={() => edit(fill.id, line.memo)}>
                                        {line.memo ? "改備註" : "＋備註"}
                                    </button>
                                    <button type="button" class="text-dim hover:text-neg" disabled={busy !== undefined} onclick={() => void run(fill.id, () => unfillGap(accountId, fill.id))}>
                                        {busy === fill.id ? "撤銷中…" : "撤銷"}
                                    </button>
                                {/if}
                            </td>
                            <td class="px-2 py-2 text-right whitespace-nowrap {amountTone(line.amount)}">
                                {formatAmount(line.amount, { signed: true })} <span class="text-mute">{line.unit}</span>
                            </td>
                            <td class="py-2 pl-2 text-right whitespace-nowrap text-dim">{line.balance ? formatAmount(line.balance) : ""}</td>
                        </tr>
                        {#if failed?.id === fill.id}
                            <tr><td colspan="6" class="pb-2 break-words text-neg">{failed.error}</td></tr>
                        {/if}
                    {:else}
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
                    {/if}
                {:else}
                    <tr><td colspan="6" class="py-2.5 text-mute">{empty}</td></tr>
                {/each}
            </tbody>
        </table>
    </div>
</section>
