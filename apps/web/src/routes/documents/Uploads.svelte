<script lang="ts">
import {
    type Document,
    MAIL_FILTERS_URL,
    type Statement,
} from "#lib/api/client.js";
import { reparse } from "#lib/api/mutations.js";
import Bar from "#lib/components/Bar.svelte";
import Tag from "#lib/components/Tag.svelte";
import { formatBytes } from "#lib/dates.js";
import {
    uploadRow,
    uploadSummary,
} from "#lib/documents.js";
import { onKeys } from "#lib/keys.svelte.js";
import {
    statuses,
    type TagStatus,
} from "#lib/statuses.js";
import { tone } from "#lib/tone.js";
import {
    type Upload,
    Uploads,
} from "#lib/upload.svelte.js";
import { resolve } from "$app/paths";

/**
 * Where statement e-mails are dropped, and how each upload of this visit goes. `documents` and
 * `statementOf` are the layout's, which follow a document while it parses; `uploads` belongs to the page,
 * which lists these documents here and leaves them out of its list.
 */
let { uploads, maxBytes, documents, statementOf }: {
    uploads: Uploads;
    maxBytes: number;
    documents: ReadonlyMap<number, Document>;
    statementOf: ReadonlyMap<number, Statement>;
} = $props();

let dragging = $state(false);
let input: HTMLInputElement | undefined = $state();
let zone: HTMLElement | undefined = $state();

const summary = $derived(uploadSummary(uploads.entries.map(entry => uploadRow(entry, entry.documentId ? documents.get(entry.documentId) : undefined))));

function barColor(tag: TagStatus): string {
    const t = statuses[tag].tone;
    return t === "pos" || t === "neg" || t === "mute" ? t : "accent";
}

async function again(id: number, entry: Upload) {
    const { error } = await reparse(id);
    entry.error = error;
}

function ondrop(event: DragEvent) {
    event.preventDefault();
    dragging = false;
    if (event.dataTransfer?.files.length) void uploads.add(event.dataTransfer.files);
}

// Dropped beside the zone, the browser would open the file in place of the app.
function refuseDrop(event: DragEvent) {
    if (event.target instanceof Node && zone?.contains(event.target)) return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = "none";
}

onKeys("documents", {
    Enter: () => input?.click(),
});
</script>

<svelte:window ondragover={refuseDrop} ondrop={refuseDrop} />

{#snippet upload(entry: Upload, doc: Document | undefined)}
    {@const r = uploadRow(entry, doc, doc ? statementOf.get(doc.id) : undefined)}
    <tr class="border-t border-rule align-top">
        <td class="max-w-[280px] truncate py-2.5 pr-2" title={entry.name}>
            {#if doc}<a href={resolve("/documents/[id]", { id: String(doc.id) })} class="text-ink hover:text-accent">{entry.name}</a>{:else}{entry.name}{/if}
        </td>
        <td class="px-2 py-2.5 text-right whitespace-nowrap text-dim">{formatBytes(entry.size)}</td>
        <td class="px-2 py-2.5">
            <div class="w-[140px]"><Bar height={10} percent={r.percent} color="var(--color-{barColor(r.tag)})" /></div>
            <div class="mt-1 text-[12px] text-mute">{r.stage}</div>
        </td>
        <td class="px-2 py-2.5"><Tag status={r.tag} /></td>
        <td class="py-2.5 pl-2">
            <div class={tone[r.tone]}>{r.detail}</div>
            {#if r.link}
                <a class="text-[12px]" href={r.link.href}>{r.link.text}</a>
            {:else if r.hint}
                <div class="mt-0.5 text-[12px] text-mute">{r.hint}</div>
            {/if}
            {#if doc && r.tag === "failed"}
                <button type="button" class="control mt-1.5 min-h-8" onclick={() => void again(doc.id, entry)}>重新解析</button>
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
        <div class="text-[12px] text-dim">只收 e-mail，手動轉寄的也行 · 一次可以多個檔案 · 單檔上限 {formatBytes(maxBytes)} · 同一個檔案重複上傳不會重複入帳</div>
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
            if (files?.length) void uploads.add([...files]);
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

{#if uploads.entries.length > 0}
    <section class="panel px-3.5 pt-1 pb-2.5">
        <div class="flex flex-wrap justify-between gap-2 pt-2.5 pb-1">
            <h2 class="label">THIS SESSION · 本次上傳（{uploads.entries.length}）</h2>
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
                    {#each uploads.entries as entry (entry.key)}
                        {@render upload(entry, entry.documentId ? documents.get(entry.documentId) : undefined)}
                    {/each}
                </tbody>
            </table>
        </div>
    </section>
{/if}
