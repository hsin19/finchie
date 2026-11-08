<script lang="ts">
import "./layout.css";
import { LEDGER } from "#lib/api/client.js";
import {
    commandLine,
    commandText,
} from "#lib/command.js";
import CommandPalette, { type Command } from "#lib/components/CommandPalette.svelte";
import HelpDialog from "#lib/components/HelpDialog.svelte";
import {
    type Month,
    shortDateTime,
} from "#lib/dates.js";
import { onKeys } from "#lib/keys.svelte.js";
import { carriedQueries } from "#lib/nav.js";
import { poll } from "#lib/poll.js";
import {
    goto,
    invalidate,
    refreshAll,
} from "$app/navigation";
import { resolve } from "$app/paths";
import { page } from "$app/state";
import type { LayoutProps } from "./$types";

let { data, children }: LayoutProps = $props();
let help = $state(false);

// While the parser works, keep refreshing: every page shows document status.
const polling = poll(() => void invalidate(LEDGER), { hidden: document.hidden });
$effect(() => polling.update(data.documents.filter(d => d.status === "pending").map(d => d.id).join()));
$effect(() => () => polling.stop());

let palette = $state(false);

// Switching between statements and transactions keeps the selected month; the overview has none.
const carried = $derived(carriedQueries(page.route.id, page.url.searchParams));
const nav = $derived([
    { key: "o", label: "總覽", href: resolve("/"), current: page.route.id === "/" },
    { key: "s", label: "帳單", href: resolve(`/statements${carried.statements}`), current: /^\/(statements|documents)/.test(page.route.id ?? "") },
    { key: "t", label: "交易", href: resolve(`/transactions${carried.transactions}`), current: page.route.id === "/transactions" },
    { key: "u", label: "上傳", href: resolve("/upload"), current: page.route.id === "/upload" },
]);

const prompt = $derived(commandLine(page.url.pathname, page.url.searchParams, (page.data as { command?: string; }).command));

// "Switch month": on the transactions page it keeps the other filters; elsewhere it opens that
// month's statements.
function monthHref(m: Month) {
    if (page.route.id !== "/transactions") return resolve(`/statements?m=${m}`);
    const rest = [...page.url.searchParams].filter(([key]) => key !== "m" && key !== "page");
    return resolve(`/transactions?${new URLSearchParams([["m", m], ...rest]).toString()}`);
}

const lastUpload = $derived(data.documents[0]);
const parser = $derived(data.documents.find(d => d.parserVersion)?.parserVersion);

const commands: Command[] = $derived([
    ...nav.map(item => ({ id: `go-${item.key}`, label: `前往 ${item.label}`, hint: `[${item.key}]`, run: () => void goto(item.href) })),
    ...data.months.map(m => ({
        id: `month-${m}`,
        label: `period ${m}`,
        hint: "切換月份",
        run: () => void goto(monthHref(m)),
    })),
    { id: "refresh", label: "重新載入資料", hint: "refresh", run: () => void refreshAll() },
    { id: "help", label: "快捷鍵說明", hint: "[?]", run: () => (help = true) },
]);

async function searchTransactions() {
    await goto(resolve(`/transactions${carried.transactions}`));
    document.getElementById("q")?.focus();
}

function go(key: string) {
    const item = nav.find(i => i.key === key);
    if (item) void goto(item.href);
}

onKeys({
    "o": () => go("o"),
    "s": () => go("s"),
    "t": () => go("t"),
    "u": () => go("u"),
    "/": () => void searchTransactions(),
    "?": () => (help = !help),
}, { global: true });

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
            <a href={resolve("/")} aria-label="finchie" class="font-bold text-accent"><svg aria-hidden="true" viewBox="6 6.5 19.5 19" class="-mr-0.5 inline-block size-[18px] align-[-4px]"><path d="M7.6 22.8c.7 2 3.8 2.2 4.4-.8l1.2-9.5c.4-3.3 2.8-4.7 5.8-4.3l5.2 2.2-4.6 2.2c-2 3.8-5.4 6.2-11.4 6.2h11.6" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" /><circle cx="17.8" cy="10.4" r="1.25" fill="currentColor" /></svg>inchie</a>
            {prompt.command}{#each prompt.flags as flag (flag.name)}
                <span class="text-mute">{` ${flag.name}`}</span>{flag.value ? ` ${flag.value}` : ""}
            {/each}
        </div>
        <nav aria-label="主要" class="hidden gap-1.5 md:flex">
            {#each nav.slice(0, 3) as item (item.key)}
                <a
                    href={item.href}
                    aria-current={item.current ? "page" : undefined}
                    class="rounded-[4px] border px-2.5 py-2 {item.current ? 'border-accent bg-accent/10 text-ink' : 'border-line text-dim hover:text-ink'}"
                ><span class="text-accent">[{item.key}]</span> {item.label}</a>
            {/each}
        </nav>
        <a
            href={nav[3].href}
            aria-current={nav[3].current ? "page" : undefined}
            class="hidden rounded-[4px] border border-accent px-3 py-2 text-ink hover:bg-accent/10 hover:text-ink md:block"
        ><span class="text-accent">[u]</span> 上傳</a>
    </header>

    <main class="flex grow flex-col gap-3">
        {@render children()}
    </main>

    <footer class="hidden flex-wrap gap-x-5 gap-y-1 border-t border-dashed border-line pt-2.5 text-[12px] text-mute md:flex">
        <span><span class="text-pos">●</span> backend ok</span>
        {#if parser}<span>parser v{parser}</span>{/if}
        {#if lastUpload}<span>last upload {shortDateTime(lastUpload.receivedAt)}</span>{/if}
        <span>{data.statements.length} statements · {data.documents.length} documents</span>
        <span class="ml-auto"><button type="button" class="text-mute hover:text-ink" onclick={() => (help = true)}><span class="text-accent">[?]</span> 快捷鍵</button> · <button
                type="button"
                class="text-mute hover:text-ink"
                onclick={() => (palette = true)}
            >
                <span class="text-accent">[⌘K]</span> 指令
            </button></span>
    </footer>
</div>

<nav aria-label="主要" class="fixed inset-x-0 bottom-0 z-10 grid grid-cols-4 border-t border-line bg-panel pb-[max(env(safe-area-inset-bottom),12px)] md:hidden">
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
