<script lang="ts">
import {
    type AccountDetail,
    api,
} from "#lib/api/client.js";
import { change } from "#lib/api/mutations.js";
import { accountKinds } from "#lib/kinds.js";

/** The owner's name and full number for an account (ADR 0016): optional, and an empty field clears one. */
let { account, onclose }: { account: AccountDetail; onclose: () => void; } = $props();

// The form starts from what the account has; the page is remounted for another account.
// svelte-ignore state_referenced_locally
let name = $state(account.name ?? "");
// svelte-ignore state_referenced_locally
let fullNumber = $state(account.fullNumber ?? "");
let error = $state<string>();

async function save(event: SubmitEvent) {
    event.preventDefault();
    const outcome = await change(api().PATCH("/api/accounts/{id}", { params: { path: { id: account.id } }, body: { name, fullNumber } }));
    if (outcome.error) error = outcome.error;
    else onclose();
}
</script>

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
    <button type="button" class="control min-h-9 px-3" onclick={onclose}>取消</button>
    {#if error}<span class="w-full text-[12px] text-neg">{error}</span>{/if}
</form>
