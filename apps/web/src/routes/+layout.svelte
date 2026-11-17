<script lang="ts">
import Dot from "#lib/components/Dot.svelte";
import Key from "#lib/components/Key.svelte";
import PageSkeleton from "#lib/components/PageSkeleton.svelte";
import { renewPush } from "#lib/push.js";
import ArrowLeftRight from "@lucide/svelte/icons/arrow-left-right";
import Bell from "@lucide/svelte/icons/bell";
import FileText from "@lucide/svelte/icons/file-text";
import House from "@lucide/svelte/icons/house";
import ReceiptText from "@lucide/svelte/icons/receipt-text";
import Wallet from "@lucide/svelte/icons/wallet";
import {
    type Component,
    onMount,
} from "svelte";
import "./layout.css";
import {
    api,
    LEDGER,
    unwrap,
} from "#lib/api/client.js";
import {
    commandLine,
    commandText,
} from "#lib/command.js";
import CommandPalette, { type Command } from "#lib/components/CommandPalette.svelte";
import HelpDialog from "#lib/components/HelpDialog.svelte";
import { shortDateTime } from "#lib/dates.js";
import {
    pendingKey,
    shownCount,
} from "#lib/documents.js";
import { pageCommand } from "#lib/heading.svelte.js";
import { onKeys } from "#lib/keys.svelte.js";
import {
    carriedLinks,
    entityKey,
    monthHref,
} from "#lib/nav.js";
import { poll } from "#lib/poll.js";
import {
    absence,
    RESUME_AFTER_MS,
} from "#lib/resume.js";
import { focusSearch } from "#lib/search.svelte.js";
import type { KeyOf } from "#lib/shortcuts.js";
import {
    markDrawn,
    Settled,
} from "#lib/stream.svelte.js";
import { version } from "$app/env";
import {
    afterNavigate,
    beforeNavigate,
    goto,
    invalidate,
    refreshAll,
} from "$app/navigation";
import {
    asset,
    resolve,
} from "$app/paths";
import {
    navigating,
    page,
    updated,
} from "$app/state";
import type { ResolvedPathname } from "$app/types";
import type { LayoutProps } from "./$types";

let { data, children }: LayoutProps = $props();

// What every page shows beside its own data: on the app's first page it comes after the layout has drawn.
const ledger = new Settled(() => data.ledger);
// From the first page on, loads wait for their data (stream.svelte.ts).
afterNavigate(markDrawn);

// The backend drops a subscription its push service says is gone, and a browser may renew one: telling it
// again whenever the app opens keeps this device's pushes coming. A failure waits for the next opening.
onMount(() => void renewPush().catch(() => undefined));
let help = $state(false);

// While the parser works, look at what is still pending, and reload the ledger once that changes: a
// document finished (or a new one came in by mail). A look that fails is tried again later; it never
// takes the page down.
const polling = poll(async () => {
    const { items } = unwrap(await api().GET("/api/documents", { params: { query: { state: ["pending"], limit: 500 } } }));
    const pending = pendingKey(items);
    if (pending !== pendingKey(ledger.value?.pending ?? [])) await invalidate(LEDGER);
    return pending;
}, { hidden: document.hidden });
$effect(() => polling.update(pendingKey(ledger.value?.pending ?? [])));
$effect(() => () => polling.stop());

// Installed to a home screen, the app stays open for days and has no reload button: back after a while, it
// reloads the ledger. With the session gone meanwhile, that read is what signs in again (session.ts).
const away = absence(RESUME_AFTER_MS, { hidden: document.hidden });
function onvisibilitychange() {
    polling.visible(!document.hidden);
    if (away.change(document.hidden)) void invalidate(LEDGER);
}

// SvelteKit notices a new release by itself (on returning to the app, among others) and loads the page
// in full when a navigation fails because of it. One that would succeed, to a page whose code is
// already here, would keep the old app going against the new API: load that one in full as well.
beforeNavigate(({ willUnload, to }) => {
    if (updated.current && !willUnload && to?.url) location.href = to.url.href;
});

let palette = $state(false);

// Switching between statements and transactions keeps the selected month; the overview has none.
const carried = $derived(carriedLinks(page.route.id, page.url.searchParams));

// The page being opened while its data loads: the tabs show it at once, and should the loading take longer
// than a glance, its skeleton stands in for the page left (or, on the same page, the page dims).
const SKELETON_AFTER_MS = 150;
const shown = $derived(navigating.to?.route.id ?? page.route.id);
const elsewhere = $derived(navigating.to !== null && navigating.to.route.id !== page.route.id);
let slow = $state(false);
$effect(() => {
    if (!navigating.to) {
        slow = false;
        return;
    }
    const timer = setTimeout(() => (slow = true), SKELETON_AFTER_MS);
    return () => clearTimeout(timer);
});
type NavItem = { key: KeyOf<"global">; label: string; href: ResolvedPathname; current: boolean; icon: Component<{ class?: string; }>; };
const pages: NavItem[] = $derived([
    { key: "o", label: "總覽", href: resolve("/"), current: shown === "/", icon: House },
    { key: "s", label: "帳單", href: carried.statements, current: /^\/statements/.test(shown ?? ""), icon: ReceiptText },
    { key: "t", label: "交易", href: carried.transactions, current: shown === "/transactions", icon: ArrowLeftRight },
    { key: "a", label: "帳戶", href: resolve("/accounts"), current: /^\/accounts/.test(shown ?? ""), icon: Wallet },
]);
// Documents arrive by mail by themselves: their page is for an upload by hand or a look at what parsing
// gave, so it is a link of its own, not a tab. What needs a look there the overview says.
const documents: NavItem = $derived({ key: "d", label: "文件", href: resolve("/documents"), current: /^\/documents/.test(shown ?? ""), icon: FileText });
// What needs the owner the overview puts first; the link counts what they have not seen yet.
const notifications: NavItem = $derived({ key: "i", label: "通知", href: resolve("/notifications"), current: shown === "/notifications", icon: Bell });
const nav = $derived([...pages, documents, notifications]);

const prompt = $derived(commandLine(page.url.pathname, page.url.searchParams, pageCommand.current));

const lastUpload = $derived(ledger.value?.documents[0]);
const parser = $derived(ledger.value?.documents.find(d => d.parserVersion)?.parserVersion);

const commands: Command[] = $derived([
    ...nav.map(item => ({ id: `go-${item.key}`, label: `前往 ${item.label}`, hint: `[${item.key}]`, run: () => void goto(item.href) })),
    ...(ledger.value?.months ?? []).map(m => ({
        id: `month-${m}`,
        label: `period ${m}`,
        hint: "切換月份",
        run: () => void goto(monthHref(page.route.id, page.url.searchParams, m)),
    })),
    { id: "refresh", label: "重新載入資料", hint: "refresh", run: () => void refreshAll() },
    { id: "help", label: "快捷鍵說明", hint: "[?]", run: () => (help = true) },
]);

// The page's own search box if it has one, else the transactions'.
async function search() {
    if (focusSearch()) return;
    await goto(carried.transactions);
    focusSearch();
}

// Looked up when pressed: the links change with the page.
function go(key: KeyOf<"global">) {
    const item = nav.find(i => i.key === key);
    if (item) void goto(item.href);
}

onKeys("global", {
    "o": () => go("o"),
    "s": () => go("s"),
    "t": () => go("t"),
    "a": () => go("a"),
    "d": () => go("d"),
    "i": () => go("i"),
    "/": () => void search(),
    "?": () => (help = !help),
});

function onkeydown(event: KeyboardEvent) {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        palette = !palette;
    }
}
</script>

<svelte:window {onkeydown} />
<svelte:document {onvisibilitychange} />

<svelte:head>
    <title>{commandText(prompt)}</title>
</svelte:head>

<!-- Installed on iOS, the page runs under the status bar (app.html): it starts below it, and this strip
     keeps what scrolls past from showing through the clock. In a browser tab the inset is 0. -->
<div class="fixed inset-x-0 top-0 z-10 h-[env(safe-area-inset-top)] bg-ground"></div>

<div class="mx-auto flex min-h-dvh max-w-[1240px] flex-col gap-3 px-3.5 pt-[calc(env(safe-area-inset-top)+0.875rem)] pb-[calc(env(safe-area-inset-bottom)+4.5rem)] md:px-5 md:pt-[calc(env(safe-area-inset-top)+1.25rem)] md:pb-6">
    <!-- One line at any width: the command gives way, cut short, and the links stay at the top right. -->
    <header class="flex items-center gap-x-3 border-b border-line pb-2.5 md:panel md:gap-x-4 md:px-3.5 md:py-2.5">
        <div class="min-w-0 flex-1 truncate text-[14px]">
            <a href={resolve("/")} aria-label="finchie" class="font-bold text-accent"><img src={asset("favicon.svg")} alt="" class="-mr-0.5 inline-block size-[18px] align-[-4px]" />inchie</a>
            {prompt.command}{#each prompt.flags as flag (flag.name)}
                <span class="text-mute">{` ${flag.name}`}</span>{flag.value ? ` ${flag.value}` : ""}
            {/each}
        </div>
        <nav aria-label="主要" class="hidden shrink-0 gap-1.5 md:flex">
            {#each pages as item (item.key)}
                <a
                    href={item.href}
                    aria-current={item.current ? "page" : undefined}
                    class="rounded-[4px] border px-2.5 py-2 {item.current ? 'border-accent bg-accent/10 text-ink' : 'border-line text-dim hover:text-ink'}"
                ><span class="text-accent">[{item.key}]</span> {item.label}</a>
            {/each}
        </nav>
        <a
            href={documents.href}
            aria-current={documents.current ? "page" : undefined}
            class="flex min-h-9 shrink-0 items-center gap-1.5 text-[12px] md:text-[13px] {documents.current ? 'text-ink' : 'text-dim hover:text-ink'}"
        >
            <Key hint={documents.key} icon={documents.icon} />{documents.label}
            {#if (ledger.value?.pending.length ?? 0) > 0}<span class="text-accent"><Dot /> {ledger.value?.pending.length}</span>{/if}
        </a>
        <a
            href={notifications.href}
            aria-current={notifications.current ? "page" : undefined}
            class="flex min-h-9 shrink-0 items-center gap-1.5 text-[12px] md:text-[13px] {notifications.current ? 'text-ink' : 'text-dim hover:text-ink'}"
        >
            <Key hint={notifications.key} icon={notifications.icon} />{notifications.label}
            {#if (ledger.value?.unread ?? 0) > 0}<span class="text-neg" title="還沒看過的通知"><Dot /> {ledger.value?.unread}</span>{/if}
        </a>
    </header>

    <main class="flex grow flex-col gap-3 transition-opacity {slow && !elsewhere ? 'opacity-60' : ''}" aria-busy={slow}>
        {#if slow && elsewhere}
            <PageSkeleton route={navigating.to?.route.id ?? null} />
        {:else}
            <!-- A page shows one entity: another id remounts it, so no draft or message carries over (entityKey). -->
            {#key entityKey(page.params)}
                {@render children()}
            {/key}
        {/if}
    </main>

    <footer class="hidden flex-wrap gap-x-5 gap-y-1 border-t border-dashed border-line pt-2.5 text-[12px] text-mute md:flex">
        <span><Dot class="text-pos" /> backend {version === "dev" ? "dev" : `v${version}`} ok</span>
        {#if parser}<span>parser v{parser}</span>{/if}
        {#if lastUpload}<span>last upload {shortDateTime(lastUpload.receivedAt)}</span>{/if}
        {#if ledger.value}<span>{ledger.value.statements.length} statements · {shownCount(ledger.value.documentsByState)} documents</span>{/if}
        <span class="ml-auto"><button type="button" class="text-mute hover:text-ink" onclick={() => (help = true)}><span class="text-accent">[?]</span> 快捷鍵</button> · <button
                type="button"
                class="text-mute hover:text-ink"
                onclick={() => (palette = true)}
            >
                <span class="text-accent">[⌘K]</span> 指令
            </button></span>
    </footer>
</div>

<!-- About as much room below the labels as above them: a touch more, or what the phone keeps for its home indicator. -->
<nav aria-label="主要" class="fixed inset-x-0 bottom-0 z-10 grid grid-cols-4 border-t border-line bg-panel pb-[max(env(safe-area-inset-bottom),4px)] md:hidden">
    {#each pages as item (item.key)}
        <a
            href={item.href}
            aria-current={item.current ? "page" : undefined}
            class="flex min-h-14 flex-col items-center justify-center gap-0.5 border-t-2 {item.current ? 'border-accent text-accent' : 'border-transparent text-dim'}"
        >
            <item.icon class="size-5" />
            <span class="text-[11px]">{item.label}</span>
        </a>
    {/each}
</nav>

<HelpDialog bind:open={help} />
<CommandPalette bind:open={palette} {commands} />
