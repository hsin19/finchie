<script lang="ts">
import {
    api,
    attempt,
    type Document,
    DOCUMENTS_PAGE_SIZE,
    LEDGER,
} from "#lib/api/client.js";
import { reparse } from "#lib/api/mutations.js";
import { postDocument } from "#lib/api/upload.js";
import SearchBox from "#lib/components/SearchBox.svelte";
import Tag from "#lib/components/Tag.svelte";
import {
    formatBytes,
    shortDateTime,
} from "#lib/dates.js";
import {
    type DocumentFilter,
    documentFilters,
    documentQuery,
    documentsById,
    documentsQueryHref,
    finishedOutside,
    liveByState,
    outcome,
    readDocumentFilters,
    replaceDocuments,
    shownCount,
} from "#lib/documents.js";
import type { FilterChanges } from "#lib/filters.js";
import { QueryRequests } from "#lib/requests.svelte.js";
import { Uploads as UploadQueue } from "#lib/upload.svelte.js";
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
import { SvelteSet } from "svelte/reactivity";
import type {
    PageProps,
    Snapshot,
} from "./$types";
import Uploads from "./Uploads.svelte";

let { data }: PageProps = $props();

// ---- Uploading: the queue lives as long as the page, the zone and its table are Uploads.svelte.

// svelte-ignore state_referenced_locally
const uploads = new UploadQueue({ maxBytes: data.settings.maxUploadBytes, send: postDocument, done: () => invalidate(LEDGER) });

// What the page shows of a document comes from the layout when it can: it has the newest and every
// pending one, and reloads once a parse is done, so a parse in progress moves along on screen.
const live = $derived(documentsById(data.documents, data.pending));
const statementOf = $derived(new Map(data.statements.map(s => [s.documentId, s])));
const sessionIds = $derived(new Set(uploads.entries.map(e => e.documentId)));

let reparseError = $state<string>();
const reparsing = new SvelteSet<number>();

async function again(id: number) {
    reparseError = undefined;
    reparsing.add(id);
    const { error } = await reparse(id);
    reparsing.delete(id);
    if (error) reparseError = `#${id}: ${error}`;
}

// ---- The list: filters in the URL, more pages as it scrolls

const requests = new QueryRequests(() => page.url.searchParams);
afterNavigate(() => requests.settle());
const filters = $derived(readDocumentFilters(requests.params));

function navigate(changes: FilterChanges, { replace = false } = {}) {
    const next = requests.request(changes, { replace });
    return goto(documentsQueryHref(next.query), { replace: next.replace, reset: false });
}

function show(filter?: DocumentFilter) {
    void navigate({ show: filter?.value ?? null });
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

// A listed document older than the layout's window drops out of it once its parse is done: the page
// follows each one the layout shows pending, then asks for it again by id.
// Bookkeeping, not state: nothing is drawn from it, and the effect must not rerun on its own writes.
// eslint-disable-next-line svelte/prefer-svelte-reactivity
const followed = new Set<number>();
$effect(() => {
    for (const d of items) if (live.get(d.id)?.status === "pending") followed.add(d.id);
    const done = finishedOutside(followed, live);
    if (done.length === 0) return;
    for (const id of done) followed.delete(id);
    untrack(() => void refresh(done));
});

async function refresh(ids: number[]) {
    const from = data.first;
    const result = attempt(await api().GET("/api/documents", { params: { query: { id: ids, limit: ids.length } } }));
    // Another filter meanwhile, or no answer: the next load shows them as they are.
    if (from === data.first && result.data) items = replaceDocuments(items, result.data.items);
}

const shown = $derived(items.filter(d => !sessionIds.has(d.id)).map(d => live.get(d.id) ?? d));
</script>

<Uploads {uploads} maxBytes={data.settings.maxUploadBytes} documents={live} {statementOf} />

<section class="panel flex flex-col gap-2.5 p-3 md:p-3.5">
    <div class="flex flex-wrap items-baseline justify-between gap-2">
        <h2 class="label">DOCUMENTS · 文件（{total}）</h2>
        {#if reparseError}<span class="text-[12px] text-neg">{reparseError}</span>{/if}
    </div>
    <SearchBox asked={() => filters.q} onsearch={q => void navigate({ q: q || null }, { replace: true })} placeholder="檔名，例如 台新、2026年10月" />
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
            {@const state = doc.status}
            <div class="flex flex-wrap items-start gap-x-3 gap-y-1 border-t border-rule py-2.5">
                <Tag status={state} />
                <div class="flex min-w-0 grow basis-[20rem] flex-col gap-0.5">
                    <a href={resolve("/documents/[id]", { id: String(doc.id) })} class="truncate text-ink hover:text-accent">#{doc.id} {doc.filename}</a>
                    <!-- What needs doing is read in full; what does not (skipped, duplicate) keeps to a line. -->
                    <span class="text-[12px] {r.tone === 'neg' ? 'break-words text-neg' : r.tone === 'dim' ? 'truncate text-mute' : 'text-dim'}" title={r.tone === "dim" ? r.text : undefined}>
                        {r.text}
                        {#if r.link}<a href={r.link.href} class="ml-1">{r.link.text}</a>{/if}
                    </span>
                    {#if r.tone === "neg" && r.hint}<span class="text-[12px] text-mute">{r.hint}</span>{/if}
                </div>
                <span class="text-[12px] whitespace-nowrap text-mute">{formatBytes(doc.sizeBytes)} · {shortDateTime(doc.receivedAt)}</span>
                {#if state === "failed"}
                    <button type="button" class="control min-h-8" disabled={reparsing.has(doc.id)} onclick={() => void again(doc.id)}>重新解析</button>
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
