<script lang="ts">
import {
    type AccountKind,
    api,
    LEDGER,
    unwrap,
} from "#lib/api/client.js";
import { accountKinds } from "#lib/labels.js";
import {
    goto,
    invalidate,
} from "$app/navigation";
import { resolve } from "$app/paths";

/**
 * An account the owner keeps by hand (ADR 0019), of any kind but a card's: its name and kind. What it holds
 * is added on its page.
 */
let { onclose }: { onclose: () => void; } = $props();

const kinds: AccountKind[] = ["points", "deposit", "securities", "exchange"];
let name = $state("");
let kind = $state<AccountKind>("points");
let error = $state<string>();

async function create(event: SubmitEvent) {
    event.preventDefault();
    try {
        const { id } = unwrap(await api().POST("/api/accounts", { body: { kind, name: name.trim() } }));
        await invalidate(LEDGER);
        await goto(resolve("/accounts/[id]", { id: String(id) }));
    } catch (e) {
        error = (e as Error).message;
    }
}
</script>

<section class="panel px-3.5 py-2.5">
    <h2 class="label">NEW · 新增帳戶</h2>
    <p class="mt-1 text-[12px] text-mute">沒有帳單或 API 的帳戶自己記，例如飯店點數、航空哩程、現金：建立後到帳戶頁加上它有的東西。</p>
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
