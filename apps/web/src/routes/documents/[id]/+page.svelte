<script lang="ts">
import {
    api,
    documentContentUrl,
    LEDGER,
    previewBodyUrl,
    previewFileUrl,
    unwrap,
} from "#lib/api/client.js";
import Tag from "#lib/components/Tag.svelte";
import {
    formatBytes,
    shortDateTime,
} from "#lib/dates.js";
import { outcome } from "#lib/documents.js";
import { onKeys } from "#lib/keys.svelte.js";
import { documentStatus } from "#lib/labels.js";
import { invalidate } from "$app/navigation";
import { resolve } from "$app/paths";
import type { PageProps } from "./$types";

let { data }: PageProps = $props();

// The layout keeps data.documents fresh while the document is parsing.
const doc = $derived(data.documents.find(d => d.id === data.document.id) ?? data.document);
const email = $derived(data.preview?.email);
const files = $derived(data.preview?.files ?? []);
const statement = $derived(data.statements.find(s => s.documentId === doc.id));
const result = $derived(outcome(doc, statement));
// A deposit statement that does not add up says which account in its warnings: the result already.
const warnings = $derived(doc.status === "needs_review" && !statement ? [] : doc.warnings);

// Queued: the document turns pending, and the layout follows it until the parser is done. A parse
// that fails the same way again can be over before the page shows it pending, so the page says when
// it asked.
let reparseError = $state<string>();
let reparsedAt = $state<string>();
async function reparse() {
    if (doc.status === "pending") return;
    reparseError = undefined;
    try {
        unwrap(await api().POST("/api/documents/{id}/reparse", { params: { path: { id: doc.id } } }));
        reparsedAt = new Date().toLocaleTimeString("en-GB", { hour12: false });
        await invalidate(LEDGER);
    } catch (error) {
        reparseError = (error as Error).message;
    }
}

function open(index: number) {
    window.open(previewFileUrl(doc.id, index), "_blank", "noreferrer");
}

// [1]–[9] open that file; [f] the statement and [r] re-parses, as on a statement's page.
const keys: Record<string, () => void> = {
    r: () => void reparse(),
    f: () => {
        const statementFile = files.find(f => f.primary);
        if (statementFile) open(statementFile.index);
    },
};
for (let n = 1; n <= 9; n++) {
    keys[String(n)] = () => {
        if (n <= files.length) open(n - 1);
    };
}
onKeys(keys);
</script>

<div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
    <h1 class="m-0 min-w-0 text-[18px] font-bold break-words">{email?.subject || doc.filename}</h1>
    <Tag status={documentStatus(doc)} label />
    <span class="ml-auto flex flex-wrap gap-x-4 text-[12px]">
        <a href={resolve("/documents")} class="text-dim hover:text-ink">← 全部文件</a>
        <!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- an API URL, not a route -->
        <a href={documentContentUrl(doc.id)} class="text-dim hover:text-ink" data-sveltekit-reload>下載原始檔 ↓</a>
    </span>
</div>

<section class="panel p-3.5">
    <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
        <h2 class="label">PARSE · 解析結果</h2>
        {#if reparsedAt && doc.status !== "pending"}<span class="ml-auto text-[12px] text-mute">{reparsedAt} 重新解析完成</span>{/if}
        <button type="button" class="control min-h-9" disabled={doc.status === "pending"} onclick={() => void reparse()}>
            <span class="text-accent">[r]</span> {doc.status === "pending" ? "解析中…" : "重新解析"}
        </button>
    </div>
    <dl class="m-0 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
        <dt class="text-mute">result</dt>
        <dd class="m-0 min-w-0 break-words {result.tone === 'neg' ? 'text-neg' : result.tone === 'dim' ? 'text-dim' : 'text-ink'}">
            {result.text}
            <!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- resolved where the outcome is described -->
            {#if result.link}<a href={result.link.href} class="ml-1">{result.link.text}</a>{/if}
        </dd>
        {#if result.hint}
            <dt class="text-mute">next</dt>
            <dd class="m-0 text-dim">{result.hint}</dd>
        {/if}
        <!-- A failure names no parser, but when it happened still tells that a re-parse ran. -->
        {#if doc.parsedAt}
            <dt class="text-mute">parsed</dt>
            <dd class="m-0 text-dim">{shortDateTime(doc.parsedAt)}{doc.parser ? ` · ${doc.parser} v${doc.parserVersion}` : ""}</dd>
        {/if}
        {#each warnings as warning, i (i)}
            <dt class="text-mute">{i === 0 ? "warnings" : ""}</dt>
            <dd class="m-0 min-w-0 break-words text-neg">{warning}</dd>
        {/each}
        {#if reparseError}
            <dt class="text-mute">re-parse</dt>
            <dd class="m-0 min-w-0 break-words text-neg">{reparseError}</dd>
        {/if}
    </dl>
</section>

<div class="grid gap-3 md:grid-cols-[1fr_1.3fr]">
    <section class="panel p-3.5">
        <h2 class="label mb-2.5">{email ? "HEADERS · 信件" : "META"}</h2>
        <dl class="m-0 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
            {#if email}
                <dt class="text-mute">from</dt>
                <dd class="m-0 min-w-0 break-words">{email.sender || "—"}</dd>
                <dt class="text-mute">to</dt>
                <dd class="m-0 min-w-0 break-words text-dim">{email.recipient || "—"}</dd>
                <dt class="text-mute">date</dt>
                <dd class="m-0 text-dim">{email.date ? shortDateTime(email.date) : "—"}</dd>
            {/if}
            <dt class="text-mute">document</dt>
            <dd class="m-0 min-w-0 truncate">#{doc.id} {doc.filename} · {formatBytes(doc.sizeBytes)}</dd>
            <dt class="text-mute">received</dt>
            <dd class="m-0 text-dim">{shortDateTime(doc.receivedAt)}</dd>
        </dl>
    </section>

    <section class="panel p-3.5">
        <h2 class="label mb-1">FILES · 附件{data.preview ? `（${files.length}）` : ""}</h2>
        {#if data.previewError}
            <p class="m-0 pt-1.5 break-words text-neg">打不開這份文件：{data.previewError}</p>
            <p class="m-0 pt-1 text-mute">可以下載原始檔，或稍後再開一次。</p>
        {/if}
        {#each files as file (file.index)}
            <div class="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 border-t border-rule py-2 first-of-type:border-t-0">
                <span class="w-[3ch] text-accent">{file.index < 9 ? `[${file.index + 1}]` : ""}</span>
                <!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- an API URL, not a route -->
                <a href={previewFileUrl(doc.id, file.index)} target="_blank" rel="noreferrer" data-sveltekit-reload class="min-w-0 grow break-all text-ink hover:text-accent">{file.filename}</a>
                <span class="text-[12px] {file.locked ? 'text-neg' : 'text-mute'}">
                    {#if file.primary}<span class="text-accent">[f]</span> 帳單 ·{/if}
                    {formatBytes(file.sizeBytes)} · {file.contentType !== "application/pdf" ? "下載" : file.locked ? "需要密碼" : "免密碼"}
                </span>
            </div>
        {:else}
            {#if data.preview}<p class="m-0 pt-1.5 text-mute">沒有附件</p>{/if}
        {/each}
    </section>
</div>

{#if email?.hasBody}
    <section class="panel overflow-hidden">
        <h2 class="label px-3.5 py-2.5">BODY · 內文</h2>
        <!-- Its own response sandboxes the body as well (ADR 0012); links open in a new tab. -->
        <iframe
            title="信件內文"
            src={previewBodyUrl(doc.id)}
            sandbox="allow-popups allow-popups-to-escape-sandbox"
            referrerpolicy="no-referrer"
            class="block h-[75vh] w-full border-0 bg-white"
        ></iframe>
    </section>
{/if}
