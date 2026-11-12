<script lang="ts">
import {
    documentContentUrl,
    previewBodyUrl,
    previewFileUrl,
} from "#lib/api/client.js";
import { deleteDocument } from "#lib/api/mutations.js";
import Dialog from "#lib/components/Dialog.svelte";
import Tag from "#lib/components/Tag.svelte";
import {
    formatBytes,
    shortDateTime,
} from "#lib/dates.js";
import { outcome } from "#lib/documents.js";
import { onKeys } from "#lib/keys.svelte.js";
import { Reparse } from "#lib/reparse.svelte.js";
import { resolve } from "$app/paths";
import type { PageProps } from "./$types";

let { data }: PageProps = $props();

// Loaded with the page, which reloads with the ledger: pending while a parse runs, then its result.
const doc = $derived(data.document);
const email = $derived(data.preview?.email);
const files = $derived(data.preview?.files ?? []);
const statement = $derived(data.statements.find(s => s.documentId === doc.id));
const result = $derived(outcome(doc, statement));
// A deposit statement that does not add up says which account in its warnings: the result already.
const warnings = $derived(doc.status === "needs_review" && !statement ? [] : doc.warnings);

const reparse = new Reparse(() => doc.id);
const parsing = $derived(reparse.requesting || doc.status === "pending");

// Only a document that holds nothing of the ledger can be deleted (`deletable`): one uploaded by mistake,
// one no parser takes, one that failed, a duplicate. Then the list, which no longer has it.
let confirming = $state(false);
let deleting = $state(false);
let deleteError = $state<string>();

function askToDelete() {
    deleteError = undefined;
    confirming = true;
}

async function remove() {
    deleting = true;
    const { error } = await deleteDocument(doc.id, resolve("/documents"));
    deleting = false;
    deleteError = error;
}

function open(index: number) {
    window.open(previewFileUrl(doc.id, index), "_blank", "noreferrer");
}

// [1]–[9] open that file; [f] the statement and [r] re-parses, as on a statement's page.
const attachment = (n: number) => () => {
    if (n <= files.length) open(n - 1);
};
onKeys("document", {
    r: () => void reparse.run(),
    f: () => {
        const statementFile = files.find(f => f.primary);
        if (statementFile) open(statementFile.index);
    },
    1: attachment(1),
    2: attachment(2),
    3: attachment(3),
    4: attachment(4),
    5: attachment(5),
    6: attachment(6),
    7: attachment(7),
    8: attachment(8),
    9: attachment(9),
});
</script>

<div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
    <h1 class="m-0 min-w-0 text-[18px] font-bold break-words">{email?.subject || doc.filename}</h1>
    <Tag status={doc.status} label />
    <span class="ml-auto flex flex-wrap gap-x-4 text-[12px]">
        <a href={resolve("/documents")} class="text-dim hover:text-ink">← 全部文件</a>
        <!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- an API URL, not a route -->
        <a href={documentContentUrl(doc.id)} class="text-dim hover:text-ink" data-sveltekit-reload>下載原始檔 ↓</a>
        {#if doc.deletable}<button type="button" class="text-dim hover:text-neg" onclick={askToDelete}>刪除</button>{/if}
    </span>
</div>

<section class="panel p-3.5">
    <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
        <h2 class="label">PARSE · 解析結果</h2>
        {#if reparse.askedAt && !parsing}<span class="ml-auto text-[12px] text-mute">{reparse.askedAt} 重新解析完成</span>{/if}
        <button type="button" class="control min-h-9" disabled={parsing} onclick={() => void reparse.run()}>
            <span class="text-accent">[r]</span> {parsing ? "解析中…" : "重新解析"}
        </button>
    </div>
    <dl class="m-0 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
        <dt class="text-mute">result</dt>
        <dd class="m-0 min-w-0 break-words {result.tone === 'neg' ? 'text-neg' : result.tone === 'dim' ? 'text-dim' : 'text-ink'}">
            {result.text}
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
        {#if reparse.error}
            <dt class="text-mute">re-parse</dt>
            <dd class="m-0 min-w-0 break-words text-neg">{reparse.error}</dd>
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

<Dialog bind:open={confirming} title="刪除文件">
    <div class="flex items-center justify-between border-b border-line px-4 py-3">
        <h2 class="label">DELETE · 刪除文件</h2>
        <button type="button" class="text-mute" aria-label="關閉" onclick={() => (confirming = false)}><span class="text-accent">[esc]</span></button>
    </div>
    <div class="flex flex-col gap-2 p-4">
        <p class="m-0 break-words">刪除 #{doc.id} {doc.filename}？原始檔一起刪掉，不能復原。</p>
        {#if doc.status === "duplicate"}
            <p class="m-0 text-dim">它重複的帳單{doc.duplicateOf ? `在 #${doc.duplicateOf}` : ""}，不受影響。</p>
        {/if}
        {#if deleteError}<p class="m-0 break-words text-neg">{deleteError}</p>{/if}
        <div class="flex justify-end gap-2 pt-2">
            <button type="button" class="control" onclick={() => (confirming = false)}>取消</button>
            <button type="button" class="control border-neg text-neg" disabled={deleting} onclick={() => void remove()}>{deleting ? "刪除中…" : "刪除"}</button>
        </div>
    </div>
</Dialog>
