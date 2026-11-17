<script lang="ts" generics="L extends object, C extends object">
import PageSkeleton from "#lib/components/PageSkeleton.svelte";
import {
    type Deferred,
    join,
    Settled,
    SKELETON_AFTER_MS,
} from "#lib/stream.svelte.js";
import {
    goto,
    snapshot,
} from "$app/navigation";
import { resolve } from "$app/paths";
import { navigating } from "$app/state";
import ArrowLeft from "@lucide/svelte/icons/arrow-left";
import {
    isHttpError,
    isRedirect,
} from "@sveltejs/kit";
import {
    type Snippet,
    tick,
} from "svelte";

/**
 * A page's content once its data is in: the layout's (`ledger`) and the page's own (`content`), together,
 * kept fresh as either reloads. Until the first of it is in, nothing for a glance and then the page's
 * skeleton (`route`, its route id); should it fail, why, and a way to try again.
 */
let { ledger, content, route, children }: { ledger: Deferred<L>; content: Deferred<C>; route: string; children: Snippet<[L & C]>; } = $props();

// One object until either half changes: a new one would redraw the content, undoing what it restored on
// Back (documents/Content.svelte).
const joined = $derived(join(ledger, content));
const data = new Settled(() => joined);

let slow = $state(false);
$effect(() => {
    if (data.value !== undefined) return;
    const timer = setTimeout(() => (slow = true), SKELETON_AFTER_MS);
    return () => clearTimeout(timer);
});

// A reload's scroll: on the app's first page SvelteKit restores it before the content is in, against a page
// with nothing in it yet, so the content goes there itself once it draws. A later page draws within its
// navigation, and SvelteKit's own restoring does.
let restoreTo = $state<number>();
snapshot<number>({
    id: "page-scroll",
    capture: () => window.scrollY,
    restore: y => {
        if (data.value === undefined) restoreTo = y;
    },
});
$effect(() => {
    if (data.value === undefined || restoreTo === undefined) return;
    const y = restoreTo;
    restoreTo = undefined;
    void tick().then(() => window.scrollTo(0, y));
});

// A redirect the load would have made, had it waited for its data; not over a page the owner has since asked
// for, while it loads (the page left stays, unseen, until then).
$effect(() => {
    const error = data.error;
    if (!isRedirect(error) || navigating.to) return;
    // eslint-disable-next-line svelte/no-navigation-without-resolve -- the load named the route, as a redirect does
    void goto(error.location, { replace: true });
});

// What went wrong. Something not there is not there on a reload either, so it leads home instead, as
// +error.svelte does.
const problem = $derived.by(() => {
    const error = data.error;
    if (error === undefined || isRedirect(error)) return undefined;
    if (isHttpError(error)) return { message: error.body.message, missing: error.status === 404 };
    return { message: error instanceof Error ? error.message : String(error), missing: false };
});
</script>

{#if data.value !== undefined}
    {@render children(data.value)}
{:else if problem}
    <section class="panel flex flex-wrap items-center gap-3 p-3.5" role="alert">
        <span class="text-neg">載入失敗：{problem.message}</span>
        {#if problem.missing}
            <span class="text-mute">找不到這個頁面或資料。<a href={resolve("/")}><ArrowLeft /> 回總覽</a></span>
        {:else}
            <button type="button" class="control min-h-8 text-[12px]" onclick={() => location.reload()}>重新載入</button>
        {/if}
    </section>
{:else if slow}
    <PageSkeleton {route} />
{/if}
