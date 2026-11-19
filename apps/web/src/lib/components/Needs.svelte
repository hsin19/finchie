<script lang="ts">
import { handleNotifications } from "#lib/api/mutations.js";
import type { Need } from "#lib/attention.js";
import ConfirmCurrent from "#lib/components/ConfirmCurrent.svelte";
import Dot from "#lib/components/Dot.svelte";
import { noticeTone } from "#lib/notices.js";
import { resolve } from "$app/paths";
import Check from "@lucide/svelte/icons/check";

/** What needs the owner (attention.ts): shown first, and only when there is something. */
let { needs }: { needs: Need[]; } = $props();

let busy = $state(false);
let error = $state<string>();

async function handle(ids: number[]) {
    busy = true;
    error = (await handleNotifications(ids)).error;
    busy = false;
}
</script>

{#snippet handled(ids: number[])}
    <!-- An action, not a state: a check to press, said in words on hover and to screen readers. -->
    <button
        type="button"
        class="control ml-1.5 inline-flex size-6 min-h-6 items-center justify-center p-0 align-middle text-[12px] text-dim hover:text-pos"
        title="標為已處理：Finchie 看不到的事已經處理好了，例如已經轉錢進去。先不顯示，情況有變會再通知"
        aria-label="標為已處理"
        disabled={busy}
        onclick={() => void handle(ids)}
    >
        <Check class="size-3.5" />
    </button>
{/snippet}

<section class="panel border-neg px-3.5 py-2.5" aria-labelledby="needs-heading">
    <h2 id="needs-heading" class="label flex gap-3 text-neg">
        ATTENTION · 待處理（{needs.length}）
        <a href={resolve("/notifications")} class="ml-auto font-normal tracking-normal text-dim normal-case hover:text-ink">全部通知</a>
    </h2>
    <ul class="m-0 mt-1 list-none p-0">
        {#each needs as need (need.kind)}
            <li class="flex flex-col gap-0.5 border-t border-rule py-2 first:border-t-0 md:flex-row md:items-baseline md:gap-3">
                <span class="shrink-0">
                    <a href={need.href} class="text-ink hover:text-ink-bright"><Dot class={noticeTone(need.kind)} /> {need.text}</a>
                    {#if need.names.length === 0}{@render handled(need.ids)}{/if}
                </span>
                {#if need.names.length > 0}
                    <span class="text-[12px] text-dim">
                        {#each need.names as name, i (name.ids.join())}{i > 0 ? "、" : ""}<a href={name.href}>{name.text}</a>{#if name.confirm !== undefined}<span class="ml-1.5"><ConfirmCurrent
                                        id={name.confirm.id}
                                        suggested={name.confirm.checkAgainOn}
                                    /></span>{:else}{@render handled(name.ids)}{/if}{/each}
                    </span>
                {/if}
            </li>
        {/each}
    </ul>
    {#if error}<p class="m-0 text-[12px] text-neg">{error}</p>{/if}
</section>
