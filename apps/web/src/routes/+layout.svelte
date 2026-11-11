<script lang="ts">
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
import { onKeys } from "#lib/keys.svelte.js";
import {
    carriedLinks,
    entityKey,
    monthHref,
} from "#lib/nav.js";
import { poll } from "#lib/poll.js";
import { focusSearch } from "#lib/search.svelte.js";
import type { KeyOf } from "#lib/shortcuts.js";
import { version } from "$app/env";
import {
    goto,
    invalidate,
    refreshAll,
} from "$app/navigation";
import {
    asset,
    resolve,
} from "$app/paths";
import { page } from "$app/state";
import type { ResolvedPathname } from "$app/types";
import type { LayoutProps } from "./$types";

let { data, children }: LayoutProps = $props();
let help = $state(false);

// While the parser works, look at what is still pending, and reload the ledger once that changes: a
// document finished (or a new one came in by mail). A look that fails is tried again later; it never
// takes the page down.
const polling = poll(async () => {
    const { items } = unwrap(await api().GET("/api/documents", { params: { query: { state: ["pending"], limit: 500 } } }));
    const pending = pendingKey(items);
    if (pending !== pendingKey(data.pending)) await invalidate(LEDGER);
    return pending;
}, { hidden: document.hidden });
$effect(() => polling.update(pendingKey(data.pending)));
$effect(() => () => polling.stop());

let palette = $state(false);

// Switching between statements and transactions keeps the selected month; the overview has none.
const carried = $derived(carriedLinks(page.route.id, page.url.searchParams));
const nav: { key: KeyOf<"global">; label: string; href: ResolvedPathname; current: boolean; }[] = $derived([
    { key: "o", label: "總覽", href: resolve("/"), current: page.route.id === "/" },
    { key: "s", label: "帳單", href: carried.statements, current: /^\/statements/.test(page.route.id ?? "") },
    { key: "t", label: "交易", href: carried.transactions, current: page.route.id === "/transactions" },
    { key: "a", label: "帳戶", href: resolve("/accounts"), current: /^\/accounts/.test(page.route.id ?? "") },
    { key: "d", label: "文件", href: resolve("/documents"), current: /^\/documents/.test(page.route.id ?? "") },
]);
// The last one, documents (where uploads go), sits apart on wide screens.
const documents = $derived(nav[nav.length - 1]);

const prompt = $derived(commandLine(page.url.pathname, page.url.searchParams, (page.data as { command?: string; }).command));

const lastUpload = $derived(data.documents[0]);
const parser = $derived(data.documents.find(d => d.parserVersion)?.parserVersion);

const commands: Command[] = $derived([
    ...nav.map(item => ({ id: `go-${item.key}`, label: `前往 ${item.label}`, hint: `[${item.key}]`, run: () => void goto(item.href) })),
    ...data.months.map(m => ({
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
<svelte:document onvisibilitychange={() => polling.visible(!document.hidden)} />

<svelte:head>
    <title>{commandText(prompt)}</title>
</svelte:head>

<div class="mx-auto flex min-h-dvh max-w-[1240px] flex-col gap-3 px-3.5 pt-3.5 pb-28 md:px-5 md:pt-5 md:pb-6">
    <header class="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-line pb-2.5 md:panel md:px-3.5 md:py-2.5">
        <div class="min-w-0 grow truncate text-[14px]">
            <a href={resolve("/")} aria-label="finchie" class="font-bold text-accent"><img src={asset("favicon.svg")} alt="" class="-mr-0.5 inline-block size-[18px] align-[-4px]" />inchie</a>
            {prompt.command}{#each prompt.flags as flag (flag.name)}
                <span class="text-mute">{` ${flag.name}`}</span>{flag.value ? ` ${flag.value}` : ""}
            {/each}
        </div>
        <nav aria-label="主要" class="hidden gap-1.5 md:flex">
            {#each nav.slice(0, -1) as item (item.key)}
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
            class="hidden rounded-[4px] border px-3 py-2 text-ink hover:bg-accent/10 hover:text-ink md:block {documents.current ? 'border-accent bg-accent/10' : 'border-accent'}"
        ><span class="text-accent">[{documents.key}]</span> {documents.label}</a>
    </header>

    <main class="flex grow flex-col gap-3">
        <!-- A page shows one entity: another id remounts it, so no draft or message carries over (entityKey). -->
        {#key entityKey(page.params)}
            {@render children()}
        {/key}
    </main>

    <footer class="hidden flex-wrap gap-x-5 gap-y-1 border-t border-dashed border-line pt-2.5 text-[12px] text-mute md:flex">
        <span><span class="text-pos">●</span> backend {version === "dev" ? "dev" : `v${version}`} ok</span>
        {#if parser}<span>parser v{parser}</span>{/if}
        {#if lastUpload}<span>last upload {shortDateTime(lastUpload.receivedAt)}</span>{/if}
        <span>{data.statements.length} statements · {shownCount(data.documentsByState)} documents</span>
        <span class="ml-auto"><button type="button" class="text-mute hover:text-ink" onclick={() => (help = true)}><span class="text-accent">[?]</span> 快捷鍵</button> · <button
                type="button"
                class="text-mute hover:text-ink"
                onclick={() => (palette = true)}
            >
                <span class="text-accent">[⌘K]</span> 指令
            </button></span>
    </footer>
</div>

<nav aria-label="主要" class="fixed inset-x-0 bottom-0 z-10 grid grid-cols-5 border-t border-line bg-panel pb-[max(env(safe-area-inset-bottom),12px)] md:hidden">
    {#each nav as item (item.key)}
        <a
            href={item.href}
            aria-current={item.current ? "page" : undefined}
            class="flex min-h-14 flex-col items-center justify-center gap-0.5 border-t-2 {item.current ? 'border-accent text-accent' : 'border-transparent text-dim'}"
        >
            <span class="text-[12px] font-bold">[{item.key}]</span>
            <span class="text-[11px]">{item.label}</span>
        </a>
    {/each}
</nav>

<HelpDialog bind:open={help} />
<CommandPalette bind:open={palette} {commands} />
