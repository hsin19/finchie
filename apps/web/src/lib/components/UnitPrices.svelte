<script lang="ts">
import { describePrice } from "#lib/accounts.js";
import {
    api,
    type Balance,
} from "#lib/api/client.js";
import { change } from "#lib/api/mutations.js";

/**
 * Each unit the account holds with its newest price and where it came from (ADR 0020), and the owner's
 * own price for any of them: like any price, it counts until a newer one comes. The base currency is
 * worth itself.
 */
let { balances, base }: { balances: Balance[]; base: string; } = $props();

const units = $derived(balances.filter(b => b.unitId !== undefined && b.unit !== base));
let values = $state<Record<number, string>>({});
let error = $state<string>();

async function save(id: number, value: string) {
    ({ error } = await change(api().PUT("/api/units/{id}/price", { params: { path: { id } }, body: { value: value.trim() } })));
    if (!error) values[id] = "";
}

async function clear(id: number) {
    ({ error } = await change(api().DELETE("/api/units/{id}/price", { params: { path: { id } } })));
}
</script>

{#if units.length > 0}
    <section class="panel px-3.5 pt-1 pb-2.5">
        <div class="flex flex-wrap items-baseline justify-between gap-2 py-2.5">
            <h2 class="label">PRICES · 單位價格</h2>
            <span class="text-[12px] text-mute">以最新的價格為準，不論來源；自訂的價格所有帳戶共用，有更新的匯率或收盤價時就換成它</span>
        </div>
        <div class="flex flex-col divide-y divide-rule">
            {#each units as balance (balance.unitId)}
                {@const id = balance.unitId!}
                <form
                    class="flex flex-wrap items-center gap-x-3 gap-y-1.5 py-2"
                    onsubmit={event => {
                        event.preventDefault();
                        void save(id, values[id] ?? "");
                    }}
                >
                    <span class="min-w-0 flex-1 basis-64 text-[13px] {balance.price ? 'text-dim' : 'text-mute'}">
                        {balance.price ? describePrice(balance.unit, balance.price, base) : `${balance.unit} 沒有價格`}
                    </span>
                    <span class="flex items-center gap-2 text-dim">
                        1 {balance.unit} = NT$
                        <input
                            class="control min-h-9 w-28 text-right text-ink"
                            inputmode="decimal"
                            placeholder="自訂"
                            aria-label="{balance.unit} 的價格"
                            value={values[id] ?? ""}
                            oninput={event => (values[id] = event.currentTarget.value)}
                        />
                        <button type="submit" class="control min-h-9 border-accent px-3" disabled={!(values[id] ?? "").trim()}>儲存</button>
                        {#if balance.price?.source === "owner"}
                            <button type="button" class="control min-h-9 px-3" onclick={() => void clear(id)}>清除自訂</button>
                        {/if}
                    </span>
                </form>
            {/each}
            {#if error}<span class="pt-2 text-[12px] text-neg">{error}</span>{/if}
        </div>
    </section>
{/if}
