<script lang="ts">
import {
    api,
    attempt,
    type Document,
    DOCUMENTS_PAGE_SIZE,
    LEDGER,
    MAIL_FILTERS_URL,
    unwrap,
} from "#lib/api/client.js";
import Bar from "#lib/components/Bar.svelte";
import Tag from "#lib/components/Tag.svelte";
import {
    formatBytes,
    shortDateTime,
} from "#lib/dates.js";
import {
    type DocumentFilter,
    documentFilters,
    documentQuery,
    liveByState,
    outcome,
    readDocumentFilters,
    shownCount,
} from "#lib/documents.js";
import type { FilterChanges } from "#lib/filters.js";
import { composing } from "#lib/keys.js";
import { onKeys } from "#lib/keys.svelte.js";
import {
    documentStatus,
    statuses,
} from "#lib/labels.js";
import { QueryRequests } from "#lib/requests.svelte.js";
import {
    afterNavigate,
    goto,
    invalidate,
} from "$app/navigation";
import { resolve } from "$app/paths";
import { page } from "$app/state";
import {
    tick,
    untrack,
} from "svelte";
import type {
    PageProps,
    Snapshot,
} from "./$types";

let { data }: PageProps = $props();

// ---- Uploading

interface Entry {
    key: number;
    name: string;
    size: number;
    /** 0 to 1 while the bytes go up. */
    sent: number;
    documentId?: number;
    /** The same bytes were uploaded before: the backend answered with the existing document. */
    same?: boolean;
    error?: string;
}

let entries = $state<Entry[]>([]);
let dragging = $state(false);
let input: HTMLInputElement | undefined = $state();
let zone: HTMLElement | undefined = $state();
let nextKey = 0;

// The layout keeps the newest documents fresh while any is parsing: what this page shows of a
// document comes from there when it can, so a parse in progress moves along on screen.
const live = $derived(new Map(data.documents.map(d => [d.id, d])));
const statementOf = $derived(new Map(data.statements.map(s => [s.documentId, s])));
const sessionIds = $derived(new Set(entries.map(e => e.documentId)));

// fetch() cannot report upload progress; XMLHttpRequest can.
function send(entry: Entry, file: File): Promise<void> {
    return new Promise(done => {
        const xhr = new XMLHttpRequest();
        xhr.open("POST", "/api/documents");
        xhr.responseType = "json";
        xhr.upload.onprogress = event => {
            if (event.lengthComputable) entry.sent = event.loaded / event.total;
        };
        xhr.onload = () => {
            const body = xhr.response as (Document & { message?: string; }) | null;
            if ((xhr.status === 200 || xhr.status === 201) && body) {
                entry.sent = 1;
                entry.documentId = body.id;
                entry.same = xhr.status === 200;
            } else {
                entry.error = body?.message ?? `HTTP ${xhr.status}`;
            }
            done();
        };
        xhr.onerror = () => {
            entry.error = "連不到 backend";
            done();
        };
        const form = new FormData();
        form.append("file", file);
        xhr.send(form);
    });
}

// One at a time: the parser works through them in order anyway.
async function add(files: FileList | File[]) {
    const queued = [...files].map(file => {
        entries.unshift({ key: nextKey++, name: file.name, size: file.size, sent: 0 });
        return { entry: entries[0], file };
    });
    for (const { entry, file } of queued.reverse()) {
        await send(entry, file);
        await invalidate(LEDGER);
    }
}

let reparseError = $state<string>();

async function reparse(id: number, entry?: Entry) {
    reparseError = undefined;
    try {
        unwrap(await api().POST("/api/documents/{id}/reparse", { params: { path: { id } } }));
        if (entry) entry.error = undefined;
        await invalidate(LEDGER);
    } catch (error) {
        const message = (error as Error).message;
        if (entry) entry.error = message;
        else reparseError = `#${id}: ${message}`;
    }
}

function ondrop(event: DragEvent) {
    event.preventDefault();
    dragging = false;
    if (event.dataTransfer?.files.length) void add(event.dataTransfer.files);
}

// Dropped beside the zone, the browser would open the file in place of the app.
function refuseDrop(event: DragEvent) {
    if (event.target instanceof Node && zone?.contains(event.target)) return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = "none";
}

type Row = { tag: keyof typeof statuses; stage: string; detail: string; tone: "ink" | "dim" | "neg"; hint: string; link?: { href: string; text: string; }; percent: number; };

const stages: Record<Document["status"], string> = {
    pending: "stored → parsing",
    parsed: "parsed",
    needs_review: "parsed → needs_review",
    duplicate: "parsed → duplicate",
    failed: "parsing → failed",
};

/** An upload of this session, as it goes. */
function describe(doc: Document | undefined, upload: Entry): Row {
    if (upload.error) return { tag: "failed", stage: "upload → failed", detail: upload.error, tone: "neg", hint: "", percent: 100 };
    if (!doc) {
        const sent = Math.round(upload.sent * 100);
        return { tag: "uploading", stage: `uploading ${sent}%`, detail: "上傳中…", tone: "ink", hint: "", percent: sent };
    }
    const result = outcome(doc, statementOf.get(doc.id));
    const same = upload.same && (doc.status === "pending" || doc.status === "parsed");
    return {
        tag: same ? "same" : documentStatus(doc),
        stage: stages[doc.status],
        detail: `${upload.same ? `上傳過了，沿用 #${doc.id} · ` : ""}${result.text}`,
        tone: same && doc.status === "parsed" ? "dim" : result.tone,
        hint: result.hint ?? "",
        link: result.link,
        percent: doc.status === "pending" ? 80 : 100,
    };
}

const rows = $derived(entries.map(entry => ({ entry, doc: entry.documentId ? live.get(entry.documentId) : undefined })));
const summary = $derived.by(() => {
    const count = { done: 0, running: 0, failed: 0, other: 0 };
    for (const { entry, doc } of rows) {
        const tag = describe(doc, entry).tag;
        if (tag === "parsed") count.done++;
        else if (tag === "uploading" || tag === "pending") count.running++;
        else if (tag === "failed" || tag === "needs_review") count.failed++;
        else count.other++;
    }
    return count;
});

// ---- The list: filters in the URL, more pages as it scrolls

const requests = new QueryRequests(() => page.url.searchParams);
afterNavigate(() => requests.settle());
const filters = $derived(readDocumentFilters(requests.params));

function navigate(changes: FilterChanges, { replace = false } = {}) {
    const next = requests.request(changes, { replace });
    return goto(next.query ? resolve(`/documents?${next.query}`) : resolve("/documents"), { replace: next.replace, reset: false });
}

function show(filter?: DocumentFilter) {
    void navigate({ show: filter?.value ?? null });
}

// Search applies while typing, a moment after the last key; as on /transactions, the box follows the
// URL except when the change is the search it sent itself.
const urlQuery = $derived(page.url.searchParams.get("q") ?? "");
let search = $state(page.url.searchParams.get("q") ?? "");
let searchBox: HTMLInputElement | undefined = $state();
let searchTimer: ReturnType<typeof setTimeout> | undefined;
let sentQuery: string | undefined;

$effect(() => {
    const q = urlQuery;
    if (q === sentQuery) {
        sentQuery = undefined;
        return;
    }
    clearTimeout(searchTimer);
    search = q;
});
$effect(() => () => clearTimeout(searchTimer));

function sendSearch() {
    clearTimeout(searchTimer);
    const q = search.trim();
    if (q === filters.q) return;
    sentQuery = q;
    void navigate({ q: q || null }, { replace: true });
}
function onSearchKeydown(event: KeyboardEvent) {
    if (composing(event)) return;
    if (event.key === "Escape") {
        search = "";
        sendSearch();
    } else if (event.key === "Enter") {
        event.preventDefault();
        sendSearch();
    }
}

// The pages loaded so far; a new first page (another filter) starts over.
let items = $derived(data.first.items);
let end = $derived(data.first.items.length < DOCUMENTS_PAGE_SIZE);
// Every filter with something in it, and the one picked even when it has nothing. Without a search,
// the layout's counts stay fresh as uploads and re-parses finish; with one, changes among the loaded
// items still move between states.
const byState = $derived(data.filters.q ? liveByState(data.first.byState, items, live) : data.documentsByState);
const total = $derived(shownCount(byState));
const chips = $derived(documentFilters.filter(f => f === filters.show || shownCount(byState, f) > 0));
let loading = $state(false);
let loadError = $state<string>();
/** The bottom of the list is on screen, or nearly. */
let nearEnd = $state(false);

async function loadMore() {
    const from = data.first;
    loading = true;
    loadError = undefined;
    const result = attempt(await api().GET("/api/documents", { params: { query: documentQuery(data.filters, items.at(-1)?.id) } }));
    loading = false;
    // The filters changed while it loaded: this page belongs to the list before.
    if (from !== data.first) return;
    if (!result.data) {
        loadError = result.error;
        return;
    }
    items = [...items, ...result.data.items];
    end = result.data.items.length < DOCUMENTS_PAGE_SIZE;
}

// Keeps loading while the bottom stays in sight: a page may not fill a tall screen.
$effect(() => {
    if (nearEnd && !loading && !end && !loadError) untrack(() => void loadMore());
});

function watchEnd(node: HTMLElement) {
    const observer = new IntersectionObserver(([entry]) => (nearEnd = entry.isIntersecting), { rootMargin: "0px 0px 600px 0px" });
    observer.observe(node);
    return () => {
        observer.disconnect();
        nearEnd = false;
    };
}

// Back from a document returns to the list as it was scrolled: the pages loaded so far are kept with
// the history entry. SvelteKit scrolls back (and runs afterNavigate) before it restores them, when the
// first page alone may not reach that far, so the list scrolls there itself once they are drawn.
export const snapshot: Snapshot<{ items: Document[]; end: boolean; scrollY: number; }> = {
    capture: () => ({ items, end, scrollY: window.scrollY }),
    restore: value => {
        items = value.items;
        end = value.end;
        void tick().then(() => window.scrollTo(0, value.scrollY));
    },
};

const shown = $derived(items.filter(d => !sessionIds.has(d.id)).map(d => live.get(d.id) ?? d));

onKeys({
    "Enter": () => input?.click(),
    "/": () => searchBox?.focus(),
});
</script>

<svelte:window ondragover={refuseDrop} ondrop={refuseDrop} />

{#snippet upload(doc: Document | undefined, entry: Entry)}
    {@const r = describe(doc, entry)}
    <tr class="border-t border-rule align-top">
        <td class="max-w-[280px] truncate py-2.5 pr-2" title={entry.name}>
            {#if doc}<a href={resolve("/documents/[id]", { id: String(doc.id) })} class="text-ink hover:text-accent">{entry.name}</a>{:else}{entry.name}{/if}
        </td>
        <td class="px-2 py-2.5 text-right whitespace-nowrap text-dim">{formatBytes(entry.size)}</td>
        <td class="px-2 py-2.5">
            <div class="w-[140px]"><Bar height={10} percent={r.percent} color="var(--color-{statuses[r.tag].tone === 'pos' ? 'pos' : statuses[r.tag].tone === 'neg' ? 'neg' : statuses[r.tag].tone === 'mute' ? 'mute' : 'accent'})" /></div>
            <div class="mt-1 text-[12px] text-mute">{r.stage}</div>
        </td>
        <td class="px-2 py-2.5"><Tag status={r.tag} /></td>
        <td class="py-2.5 pl-2">
            <div class={r.tone === "neg" ? "text-neg" : r.tone === "dim" ? "text-dim" : "text-ink"}>{r.detail}</div>
            {#if r.link}
                <!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- resolved where the outcome is described -->
                <a class="text-[12px]" href={r.link.href}>{r.link.text}</a>
            {:else if r.hint}
                <div class="mt-0.5 text-[12px] text-mute">{r.hint}</div>
            {/if}
            {#if doc && r.tag === "failed"}
                <button type="button" class="control mt-1.5 min-h-8" onclick={() => void reparse(doc.id, entry)}>重新解析</button>
            {/if}
        </td>
    </tr>
{/snippet}

<section
    bind:this={zone}
    aria-label="上傳區"
    class="flex flex-wrap items-center gap-x-4 gap-y-2.5 rounded-[6px] border-[1.5px] border-dashed border-accent px-4 py-3.5 {dragging ? 'bg-accent/10' : 'bg-accent/[0.04]'}"
    ondragover={event => {
        event.preventDefault();
        dragging = true;
    }}
    ondragleave={() => (dragging = false)}
    {ondrop}
>
    <svg aria-hidden="true" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="var(--color-accent)" stroke-width="1.5" stroke-linecap="square" class="shrink-0">
        <path d="M12 16V4" />
        <path d="M7 9l5-5 5 5" />
        <path d="M4 15v5h16v-5" />
    </svg>
    <div class="min-w-0 grow basis-[24rem]">
        <div class="font-bold">把帳單 e-mail（.eml）拖到這裡</div>
        <div class="text-[12px] text-dim">只收 e-mail，手動轉寄的也行 · 一次可以多個檔案 · 單檔上限 32 MB · 同一個檔案重複上傳不會重複入帳</div>
    </div>
    <button type="button" class="control min-h-10 border-accent" onclick={() => input?.click()}><span class="text-accent">[enter]</span> 選擇檔案</button>
    <input
        bind:this={input}
        type="file"
        multiple
        accept=".eml, message/rfc822"
        class="hidden"
        onchange={event => {
            const files = event.currentTarget.files;
            if (files?.length) void add([...files]);
            event.currentTarget.value = "";
        }}
    />
</section>

<p class="m-0 text-[12px] text-dim">
    要自動收帳單：
    <!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- an API URL, not a route -->
    <a href={MAIL_FILTERS_URL} download data-sveltekit-reload>下載 Gmail 篩選器 ↓</a>
    ，在 Gmail 設定 → 篩選器和封鎖的地址 → 匯入篩選器。之前自己設的轉寄篩選器要關掉轉寄，不然每封會收到兩次。
</p>

{#if entries.length > 0}
    <section class="panel px-3.5 pt-1 pb-2.5">
        <div class="flex flex-wrap justify-between gap-2 pt-2.5 pb-1">
            <h2 class="label">THIS SESSION · 本次上傳（{entries.length}）</h2>
            <span class="text-dim">
                <span class="text-pos">●</span> {summary.done} 完成 · <span class="text-accent">●</span> {summary.running} 進行中 · <span class="text-neg">●</span> {summary.failed} 要處理 ·
                <span class="text-mute">●</span> {summary.other} 略過
            </span>
        </div>
        <div class="overflow-x-auto">
            <table class="w-full min-w-[860px] table-fixed border-collapse">
                <colgroup>
                    <col class="w-[30%]" />
                    <col class="w-[84px]" />
                    <col class="w-[172px]" />
                    <col class="w-[84px]" />
                    <col />
                </colgroup>
                <thead>
                    <tr class="text-left text-mute">
                        <th class="py-2 pr-2 font-medium">FILE</th>
                        <th class="px-2 py-2 text-right font-medium">SIZE</th>
                        <th class="px-2 py-2 font-medium">PROGRESS</th>
                        <th class="px-2 py-2 font-medium">STATUS</th>
                        <th class="py-2 pl-2 font-medium">DETAIL</th>
                    </tr>
                </thead>
                <tbody>
                    {#each rows as { entry, doc } (entry.key)}
                        {@render upload(doc, entry)}
                    {/each}
                </tbody>
            </table>
        </div>
    </section>
{/if}

<section class="panel flex flex-col gap-2.5 p-3 md:p-3.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2">
        <h2 class="label">DOCUMENTS · 文件（{total}）</h2>
        {#if reparseError}<span class="text-[12px] text-neg">{reparseError}</span>{/if}
    </div>
    <div class="flex items-center gap-2 rounded-[4px] border border-line-strong bg-ground px-2.5">
        <label for="document-q" class="whitespace-nowrap"><span class="text-accent">[/]</span> 搜尋</label>
        <input
            id="document-q"
            bind:this={searchBox}
            type="search"
            autocomplete="off"
            placeholder="檔名，例如 台新、2026年10月"
            class="min-h-10 min-w-0 grow border-none bg-transparent text-ink outline-none"
            bind:value={search}
            oninput={() => {
                clearTimeout(searchTimer);
                searchTimer = setTimeout(sendSearch, 250);
            }}
            onkeydown={onSearchKeydown}
        />
        <span class="hidden text-[12px] text-mute sm:inline"><span class="text-accent">[esc]</span> 清除</span>
    </div>
    <div role="group" aria-label="狀態" class="flex flex-wrap gap-1.5">
        <button type="button" aria-pressed={!filters.show} class="control {filters.show ? 'text-mute' : 'pressed'}" onclick={() => show()}>
            全部 <span class="text-mute">{total}</span>
        </button>
        {#each chips as filter (filter.value)}
            {@const on = filters.show === filter}
            <button type="button" aria-pressed={on} class="control {on ? 'pressed' : 'text-mute'}" onclick={() => show(on ? undefined : filter)}>
                {filter.label} <span class="text-mute">{shownCount(byState, filter)}</span>
            </button>
        {/each}
    </div>

    <div>
        {#each shown as doc (doc.id)}
            {@const r = outcome(doc, statementOf.get(doc.id))}
            {@const state = documentStatus(doc)}
            <div class="flex flex-wrap items-start gap-x-3 gap-y-1 border-t border-rule py-2.5">
                <Tag status={state} />
                <div class="flex min-w-0 grow basis-[20rem] flex-col gap-0.5">
                    <a href={resolve("/documents/[id]", { id: String(doc.id) })} class="truncate text-ink hover:text-accent">#{doc.id} {doc.filename}</a>
                    <!-- What needs doing is read in full; what does not (skipped, duplicate) keeps to a line. -->
                    <span class="text-[12px] {r.tone === 'neg' ? 'break-words text-neg' : r.tone === 'dim' ? 'truncate text-mute' : 'text-dim'}" title={r.tone === "dim" ? r.text : undefined}>
                        {r.text}
                        <!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- resolved where the outcome is described -->
                        {#if r.link}<a href={r.link.href} class="ml-1">{r.link.text}</a>{/if}
                    </span>
                    {#if r.tone === "neg" && r.hint}<span class="text-[12px] text-mute">{r.hint}</span>{/if}
                </div>
                <span class="text-[12px] whitespace-nowrap text-mute">{formatBytes(doc.sizeBytes)} · {shortDateTime(doc.receivedAt)}</span>
                {#if state === "failed"}
                    <button type="button" class="control min-h-8" onclick={() => void reparse(doc.id)}>重新解析</button>
                {/if}
            </div>
        {/each}

        <div class="border-t border-rule pt-2.5 text-[12px] text-mute">
            {#if loadError}
                <span class="text-neg">載入失敗：{loadError}</span>
                <button type="button" class="control ml-2 min-h-8" onclick={() => void loadMore()}>重試</button>
            {:else if !end}
                <div {@attach watchEnd}>{loading ? "載入中…" : "往下捲載入更多"}</div>
            {:else if items.length === 0}
                {total === 0 && !filters.q ? "還沒有文件：把第一封帳單 e-mail 拖到上面。" : "沒有符合的文件。"}
            {:else}
                就這些了，共 {items.length} 份
            {/if}
        </div>
    </div>
</section>
