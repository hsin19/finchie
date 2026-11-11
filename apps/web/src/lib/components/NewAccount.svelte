<script lang="ts">
import { ownedKinds } from "#lib/accounts.js";
import {
    type AccountKind,
    api,
} from "#lib/api/client.js";
import { change } from "#lib/api/mutations.js";
import { accountKinds } from "#lib/kinds.js";
import { goto } from "$app/navigation";
import { resolve } from "$app/paths";

/**
 * An account the owner keeps by hand (ADR 0019), of any kind but a card's: its name and kind. What it holds
 * is added on its page.
 */
let { onclose }: { onclose: () => void; } = $props();

// Points first: what is most often kept by hand.
const kinds: AccountKind[] = ["points", ...ownedKinds.filter(k => k !== "points")];
let name = $state("");
let kind = $state<AccountKind>("points");
let error = $state<string>();

async function create(event: SubmitEvent) {
    event.preventDefault();
    const created = await change(api().POST("/api/accounts", { body: { kind, name: name.trim() } }));
    if (created.error !== undefined) error = created.error;
    else await goto(resolve("/accounts/[id]", { id: String(created.data.id) }));
}
</script>

<section class="panel px-3.5 py-2.5">
    <h2 class="label">NEW · 新增帳戶</h2>
    <p class="mt-1 text-[12px] text-mute">沒有帳單或 API 的帳戶自己記，例如飯店點數、航空哩程、現金，或是貸款（記還欠的本金，算負債）：建立後到帳戶頁加上它有的東西。</p>
    <form class="mt-2 flex flex-wrap items-end gap-3" onsubmit={create}>
        <label class="flex flex-col gap-1 text-[12px] text-mute">
            帳戶名稱
            <input class="control min-h-9 w-44 text-ink" bind:value={name} required placeholder="飯店會員" />
        </label>
        <label class="flex flex-col gap-1 text-[12px] text-mute">
            種類
            <select class="control min-h-9 text-ink" bind:value={kind}>
                {#each kinds as k (k)}<option value={k}>{accountKinds[k].label}</option>{/each}
            </select>
        </label>
        <button type="submit" class="control min-h-9 border-accent px-3">建立</button>
        <button type="button" class="control min-h-9 px-3" onclick={onclose}>取消</button>
        {#if error}<span class="w-full text-[12px] text-neg">{error}</span>{/if}
    </form>
</section>
