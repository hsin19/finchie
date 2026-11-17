<script lang="ts">
import { namePayee } from "#lib/api/mutations.js";
import { transactionsHref } from "#lib/filters.js";
import Pencil from "@lucide/svelte/icons/pencil";

/**
 * A line's counterparty, as the bank prints it, with the owner's name for it: named once, it shows on
 * every line of it, and search finds them by it. The link lists them all.
 */
let { counterparty, name }: { counterparty: string; name?: string | null; } = $props();

let editing = $state(false);
let draft = $state("");
let busy = $state(false);
let error = $state<string>();

// Opened by the owner's click, to type the name at once.
const focus = (input: HTMLInputElement) => input.focus();

function edit() {
    draft = name ?? "";
    error = undefined;
    editing = true;
}

async function save() {
    busy = true;
    const outcome = await namePayee(counterparty, draft.trim());
    busy = false;
    error = outcome.error;
    if (!outcome.error) editing = false;
}
</script>

{#if editing}
    <form class="flex flex-wrap items-center gap-1.5" onsubmit={event => (event.preventDefault(), void save())}>
        <input
            class="control min-h-7 w-32 px-1.5 text-[12px]"
            aria-label="這個對象的名稱"
            placeholder="例如 房東"
            bind:value={draft}
            {@attach focus}
            onkeydown={event => event.key === "Escape" && (editing = false)}
        />
        <button type="submit" class="control min-h-7 px-2 text-[12px]" disabled={busy || (!draft.trim() && !name)}>{draft.trim() || !name ? "儲存" : "移除名稱"}</button>
        <button type="button" class="text-[12px] text-mute hover:text-ink" onclick={() => (editing = false)}>取消</button>
        <span class="w-full text-[12px] text-mute">{counterparty}</span>
    </form>
{:else}
    <span class="flex flex-wrap items-baseline gap-x-2">
        {#if name}
            <a href={transactionsHref({ q: name, period: "all" })} title="這個對象的所有交易">{name}</a>
            <span class="text-[12px] text-mute">{counterparty}</span>
        {:else}
            <a href={transactionsHref({ q: counterparty, period: "all" })} class="text-dim" title="這個對象的所有交易">{counterparty}</a>
        {/if}
        <button type="button" class="text-[12px] text-mute hover:text-ink" title={name ? "改名" : "幫這個對象取名"} aria-label={name ? "改名" : "取名"} onclick={edit}>
            <Pencil class="size-3" />
        </button>
    </span>
{/if}
{#if error}<span class="block text-[12px] text-neg">{error}</span>{/if}
