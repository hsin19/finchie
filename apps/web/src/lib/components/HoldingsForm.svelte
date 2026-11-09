<script lang="ts">
import {
    type AccountKind,
    api,
    type Balance,
    type Expiry,
    LEDGER,
    unwrap,
} from "#lib/api/client.js";
import { invalidate } from "$app/navigation";

/**
 * What an account the owner keeps holds, unit by unit, and of points what lapses by when (ADR 0019). Saved
 * as today's snapshot of the whole account: a unit taken off is at 0, and a later save the same day
 * replaces it.
 */
let { accountId, kind, balances, expiries }: { accountId: number; kind: AccountKind; balances: Balance[]; expiries: Expiry[]; } = $props();

interface Lapse {
    amount: string;
    by: string;
}
interface Row {
    unit: string;
    amount: string;
    lapses: Lapse[];
}

function rowsOf(held: Balance[], lapsing: Expiry[]): Row[] {
    return held.map(b => ({
        unit: b.unit,
        amount: b.amount,
        lapses: lapsing.filter(e => e.unit === b.unit).map(e => ({ amount: e.amount, by: e.by })),
    }));
}

// The form starts from what the account has, and keeps what is typed until it is saved.
// svelte-ignore state_referenced_locally
let rows = $state<Row[]>(rowsOf(balances, expiries));
let error = $state<string>();
let saved = $state(false);

const points = $derived(kind === "points");
// An account's units are of its kind's (ADR 0020).
const unitKind = $derived(kind === "securities" ? "security" : points ? "points" : "money");
const unitLabel = $derived(kind === "securities" ? "證券名稱（同對帳單）" : points ? "點數名稱" : "幣別");
let known = $state<string[]>([]);
$effect(() => {
    void api().GET("/api/units", { params: { query: { kind: unitKind } } }).then(result => {
        known = (result.data ?? []).map(u => u.symbol);
    });
});
const held = $derived(new Set(balances.map(b => b.unit)));

function changed() {
    saved = false;
}

async function save(event: SubmitEvent) {
    event.preventDefault();
    // A row left empty is dropped; one half filled is a mistake, not dropped.
    const filled = rows.filter(r => r.unit.trim() !== "" || r.amount.trim() !== "");
    if (filled.some(r => r.unit.trim() === "" || r.amount.trim() === "")) {
        error = `每一列都要填${unitLabel}和數量`;
        return;
    }
    if (filled.some(r => r.lapses.some(l => (l.amount.trim() === "") !== (l.by === "")))) {
        error = "到期的數量和日期要一起填";
        return;
    }
    try {
        const holdings = filled.map(r => ({
            unit: r.unit.trim(),
            amount: r.amount.trim(),
            expiries: r.lapses.filter(l => l.amount.trim() !== "").map(l => ({ amount: l.amount.trim(), by: l.by })),
        }));
        unwrap(await api().PUT("/api/accounts/{id}/holdings", { params: { path: { id: accountId } }, body: { holdings } }));
        error = undefined;
        await invalidate(LEDGER);
        // Again from what the account now has: amounts as stored, units at 0 gone, and only what is still
        // to lapse.
        rows = rowsOf(balances, expiries);
        saved = true;
    } catch (e) {
        error = (e as Error).message;
    }
}
</script>

<section class="panel px-3.5 pt-1 pb-2.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
        <h2 class="label">UPDATE · 更新持有</h2>
        <span class="text-[12px] text-mute">手動維護的帳戶：存成今天的快照，同一天再存會蓋掉；拿掉的那種就是 0</span>
    </div>
    <datalist id="units-{accountId}">
        {#each known as symbol (symbol)}<option value={symbol}></option>{/each}
    </datalist>
    <form class="flex flex-col gap-3" onsubmit={save} oninput={changed}>
        {#each rows as row, i (i)}
            <div class="flex flex-col gap-1.5 border-t border-rule pt-2.5">
                <div class="flex flex-wrap items-center gap-2 text-dim">
                    <input
                        class="control min-h-9 w-48 text-ink"
                        list="units-{accountId}"
                        placeholder={unitLabel}
                        aria-label={unitLabel}
                        bind:value={row.unit}
                    />
                    <input class="control min-h-9 w-36 text-right text-ink" inputmode="decimal" placeholder="數量" aria-label="數量" bind:value={row.amount} />
                    <button
                        type="button"
                        class="text-[12px] text-dim hover:text-neg"
                        onclick={() => {
                            rows.splice(i, 1);
                            changed();
                        }}
                    >
                        拿掉
                    </button>
                </div>
                {#if points && row.unit.trim() !== "" && !held.has(row.unit.trim()) && known.includes(row.unit.trim())}
                    <span class="text-[12px] text-accent">「{row.unit.trim()}」是已經有的點數：會當作同一種，價格也共用。</span>
                {/if}
                {#if points}
                    {#each row.lapses as lapse, j (j)}
                        <div class="flex flex-wrap items-center gap-2 pl-4 text-[13px] text-dim">
                            <input class="control min-h-9 w-32 text-right text-ink" inputmode="decimal" placeholder="數量" bind:value={lapse.amount} />
                            在
                            <input class="control min-h-9 text-ink" type="date" bind:value={lapse.by} />
                            前到期
                            <button
                                type="button"
                                class="text-[12px] text-dim hover:text-neg"
                                onclick={() => {
                                    row.lapses.splice(j, 1);
                                    changed();
                                }}
                            >
                                刪除
                            </button>
                        </div>
                    {/each}
                    <button
                        type="button"
                        class="self-start pl-4 text-[12px] text-dim hover:text-ink"
                        onclick={() => {
                            row.lapses.push({ amount: "", by: "" });
                            changed();
                        }}
                    >
                        ＋ 加一筆到期
                    </button>
                {/if}
            </div>
        {:else}
            <p class="text-[13px] text-mute">還沒有記任何東西。</p>
        {/each}
        <div class="flex flex-wrap items-center gap-3 border-t border-rule pt-2.5">
            <button
                type="button"
                class="text-[12px] text-dim hover:text-ink"
                onclick={() => {
                    rows.push({ unit: "", amount: "", lapses: [] });
                    changed();
                }}
            >
                ＋ 加一種
            </button>
            <button type="submit" class="control min-h-9 border-accent px-3">儲存</button>
            {#if saved}<span class="text-[12px] text-pos">已儲存</span>{/if}
            {#if error}<span class="text-[12px] text-neg">{error}</span>{/if}
        </div>
    </form>
</section>
