<script lang="ts">
import type { Notification } from "#lib/api/client.js";
import {
    handleNotifications,
    readNotifications,
} from "#lib/api/mutations.js";
import {
    notificationHref,
    notificationKinds,
    pending,
} from "#lib/attention.js";
import Dot from "#lib/components/Dot.svelte";
import PushSwitch from "#lib/components/PushSwitch.svelte";
import {
    shortDateTime,
    today,
} from "#lib/dates.js";
import {
    describe,
    named,
    noticeLabels,
    noticeTone,
} from "#lib/notices.js";
import type { Shown } from "#lib/stream.svelte.js";
import Check from "@lucide/svelte/icons/check";
import Undo2 from "@lucide/svelte/icons/undo-2";
import { untrack } from "svelte";
import type { PageData } from "./$types";

let { data }: { data: Shown<PageData>; } = $props();

// The page is not open across days.
const day = today();
const base = $derived(data.settings.baseCurrency);

// Opening the page reads what it lists; what was new when it opened stays marked as such.
const fresh = untrack(() => new Set(data.all.filter(n => !n.readAt).map(n => n.id)));
$effect(() => {
    const unread = data.all.filter(n => !n.readAt).map(n => n.id);
    if (unread.length > 0) void readNotifications(unread);
});

const sections = $derived([
    // What to deal with first leads, as on the overview; within a kind, the newest.
    { title: "PENDING · 待處理", items: data.all.filter(pending).sort((a, b) => notificationKinds.indexOf(a.kind) - notificationKinds.indexOf(b.kind)), empty: "沒有要處理的事" },
    { title: "HANDLED · 已處理，情況還在", items: data.all.filter(n => !n.resolvedAt && n.handledAt), empty: "" },
    { title: "RESOLVED · 已解決", items: data.all.filter(n => n.resolvedAt), empty: "" },
]);

let busy = $state(false);
let error = $state<string>();

async function handle(n: Notification) {
    busy = true;
    error = (await handleNotifications([n.id], !n.handledAt)).error;
    busy = false;
}
</script>

<PushSwitch publicKey={data.settings.pushPublicKey} />

{#each sections as section (section.title)}
    {#if section.items.length > 0 || section.empty}
        <section class="panel flex flex-col gap-1 p-3 md:p-3.5">
            <h2 class="label">{section.title}（{section.items.length}）</h2>
            {#if section.items.length === 0}
                <p class="m-0 text-[13px] text-mute">{section.empty}</p>
            {/if}
            <ul class="m-0 list-none p-0">
                {#each section.items as n (n.id)}
                    <li class="flex flex-col gap-0.5 border-t border-rule py-2 first:border-t-0 md:flex-row md:items-baseline md:gap-3">
                        <span class="flex shrink-0 gap-3 text-[12px] md:w-56">
                            <span class="text-mute">{shortDateTime(n.raisedAt)}</span>
                            <span class={n.resolvedAt ? "text-mute" : noticeTone(n.kind)}><Dot /> {noticeLabels[n.kind]}</span>
                            {#if fresh.has(n.id)}<span class="text-accent">新</span>{/if}
                        </span>
                        <a href={notificationHref(n)} class="min-w-0 grow {n.resolvedAt ? 'text-dim' : 'text-ink'} hover:text-ink-bright">
                            {n.account ? `${named(n.account)} ` : ""}{describe(n, day, base)}
                        </a>
                        {#if n.resolvedAt}
                            <span class="shrink-0 text-[12px] text-mute">{shortDateTime(n.resolvedAt)} 解決</span>
                        {:else}
                            <button
                                type="button"
                                class="control inline-flex size-6 min-h-6 shrink-0 items-center justify-center self-start p-0 text-[12px] text-dim {n.handledAt ? 'hover:text-ink' : 'hover:text-pos'}"
                                title={n.handledAt ? "移回待處理" : "標為已處理：Finchie 看不到的事已經處理好了，例如已經轉錢進去。先不顯示，情況有變會再通知"}
                                aria-label={n.handledAt ? "移回待處理" : "標為已處理"}
                                disabled={busy}
                                onclick={() => void handle(n)}
                            >
                                {#if n.handledAt}<Undo2 class="size-3.5" />{:else}<Check class="size-3.5" />{/if}
                            </button>
                        {/if}
                    </li>
                {/each}
            </ul>
        </section>
    {/if}
{/each}
{#if error}<p class="m-0 text-[12px] text-neg">{error}</p>{/if}
