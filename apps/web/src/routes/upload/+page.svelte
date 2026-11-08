<script lang="ts">
import {
    api,
    type Document,
    LEDGER,
    MAIL_FILTERS_URL,
    unwrap,
} from "#lib/api/client.js";
import Bar from "#lib/components/Bar.svelte";
import Tag from "#lib/components/Tag.svelte";
import {
    formatBytes,
    monthOf,
    shortDateTime,
} from "#lib/dates.js";
import { onKeys } from "#lib/keys.svelte.js";
import {
    documentStatus,
    institution,
    statuses,
} from "#lib/labels.js";
import {
    formatAmount,
    subtract,
} from "#lib/money.js";
import { invalidate } from "$app/navigation";
import { resolve } from "$app/paths";
import type { PageProps } from "./$types";

let { data }: PageProps = $props();

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

const documents = $derived(new Map(data.documents.map(d => [d.id, d])));
const statementOf = $derived(new Map(data.statements.map(s => [s.documentId, s])));
const sessionIds = $derived(new Set(entries.map(e => e.documentId)));
const recent = $derived(data.documents.filter(d => !sessionIds.has(d.id)).slice(0, 10));

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

async function reparse(entry: Entry | undefined, id: number) {
    try {
        unwrap(await api().POST("/api/documents/{id}/reparse", { params: { path: { id } } }));
        if (entry) entry.error = undefined;
        await invalidate(LEDGER);
    } catch (error) {
        if (entry) entry.error = (error as Error).message;
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

// What to do about a failure, by the gRPC code the worker recorded ("FailedPrecondition: ...").
function hint(error: string): string {
    if (error.startsWith("Unimplemented")) return "沒有 parser 認領這封信：寄件者網域或主旨對不上";
    if (error.startsWith("FailedPrecondition")) return "PDF 打不開：訊息會寫要設哪個 parser 變數，設好後按「重新解析」";
    if (error.startsWith("InvalidArgument")) return "不是 e-mail 或讀不出來：上傳銀行寄來的原始信（.eml）";
    if (error.startsWith("Internal")) return "版面跟 parser 認得的不一樣：修好 parser 後按「重新解析」";
    return "按「重新解析」再試一次";
}

type Row = { tag: keyof typeof statuses; stage: string; detail: string; tone: "ink" | "dim" | "neg"; hint: string; link?: { href: string; text: string; }; percent: number; };

function describe(doc: Document | undefined, upload?: Entry): Row {
    if (upload?.error) return { tag: "failed", stage: "upload → failed", detail: upload.error, tone: "neg", hint: "", percent: 100 };
    if (!doc) {
        const sent = Math.round((upload?.sent ?? 0) * 100);
        return { tag: "uploading", stage: `uploading ${sent}%`, detail: "上傳中…", tone: "ink", hint: "", percent: sent };
    }
    const statement = statementOf.get(doc.id);
    // A card bill has a page of its own; a deposit or securities statement's lines are on its accounts' pages.
    const deposit = doc.statementKind === "deposit" || doc.statementKind === "securities";
    const link = statement
        ? { href: resolve("/statements/[id]", { id: String(statement.id) }), text: `→ 查看帳單 ${institution(statement.institution).name} ${monthOf(statement.closingDate)}` }
        : deposit
        ? { href: resolve("/accounts"), text: `→ 查看帳戶` }
        : undefined;
    const accounts = deposit ? ` · ${doc.accountCount} 個帳戶` : "";
    const sameNote = upload?.same ? `上傳過了，沿用 #${doc.id} · ` : "";
    switch (doc.status) {
        case "pending":
            return { tag: upload?.same ? "same" : "pending", stage: "stored → parsing", detail: `${sameNote}parse_document #${doc.id}`, tone: "ink", hint: "解析中，通常幾秒；滙豐要 OCR 商店名，約 10 秒", percent: 80 };
        case "parsed":
            return {
                tag: upload?.same ? "same" : "parsed",
                stage: "parsed",
                detail: `${sameNote}${doc.parser} v${doc.parserVersion} · ${doc.lineCount} 筆${accounts} · balanced`,
                tone: upload?.same ? "dim" : "ink",
                hint: "",
                link,
                percent: 100,
            };
        case "needs_review": {
            if (!statement) {
                // A deposit statement names each account whose lines do not add up in the document's warnings.
                const detail = doc.warnings.join("；") || "有帳戶的明細加總對不上";
                return { tag: "needs_review", stage: "parsed → needs_review", detail: `${sameNote}${detail}`, tone: "neg", hint: "打開帳戶看是哪一列沒抓到", link, percent: 100 };
            }
            const delta = formatAmount(subtract(statement.reconciliation.stated, statement.reconciliation.computed), { signed: true });
            return { tag: "needs_review", stage: "parsed → needs_review", detail: `${sameNote}明細加總跟帳單差 ${delta}`, tone: "neg", hint: "打開帳單看是哪一列沒抓到", link, percent: 100 };
        }
        case "duplicate":
            return { tag: "duplicate", stage: "parsed → duplicate", detail: `${sameNote}${doc.error ?? "同一張帳單已經入帳"}`, tone: "dim", hint: "這份不再寫入", percent: 100 };
        case "failed":
            if (documentStatus(doc) === "skipped") {
                return { tag: "skipped", stage: "parsing → failed", detail: `${sameNote}${doc.error}`, tone: "dim", hint: hint(doc.error ?? ""), percent: 100 };
            }
            return { tag: "failed", stage: "parsing → failed", detail: `${sameNote}${doc.error ?? "failed"}`, tone: "neg", hint: hint(doc.error ?? ""), percent: 100 };
    }
}

const rows = $derived(entries.map(entry => ({ entry, doc: entry.documentId ? documents.get(entry.documentId) : undefined })));
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

onKeys({ Enter: () => input?.click() });
</script>

<svelte:window ondragover={refuseDrop} ondrop={refuseDrop} />

{#snippet row(doc: Document | undefined, name: string, size: number, entry?: Entry)}
    {@const r = describe(doc, entry)}
    <tr class="border-t border-rule align-top">
        <td class="max-w-[280px] truncate py-2.5 pr-2" title={name}>{name}</td>
        <td class="px-2 py-2.5 text-right whitespace-nowrap text-dim">{formatBytes(size)}</td>
        <td class="px-2 py-2.5">
            <div class="w-[140px]"><Bar height={10} percent={r.percent} color="var(--color-{statuses[r.tag].tone === 'pos' ? 'pos' : statuses[r.tag].tone === 'neg' ? 'neg' : statuses[r.tag].tone === 'mute' ? 'mute' : 'accent'})" /></div>
            <div class="mt-1 text-[12px] text-mute">{r.stage}</div>
        </td>
        <td class="px-2 py-2.5"><Tag status={r.tag} /></td>
        <td class="py-2.5 pl-2">
            <div class={r.tone === "neg" ? "text-neg" : r.tone === "dim" ? "text-dim" : "text-ink"}>{r.detail}</div>
            {#if r.link}
                <!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- resolved where the row is described -->
                <a class="text-[12px]" href={r.link.href}>{r.link.text}</a>
            {:else if r.hint}
                <div class="mt-0.5 text-[12px] text-mute">{r.hint}</div>
            {/if}
            {#if doc && r.tag === "failed"}
                <button type="button" class="control mt-1.5 min-h-8" onclick={() => void reparse(entry, doc.id)}>重新解析</button>
            {/if}
        </td>
    </tr>
{/snippet}

<section
    bind:this={zone}
    aria-label="上傳區"
    class="flex flex-col items-center gap-3 rounded-[6px] border-[1.5px] border-dashed border-accent px-5 py-10 text-center {dragging ? 'bg-accent/10' : 'bg-accent/[0.04]'}"
    ondragover={event => {
        event.preventDefault();
        dragging = true;
    }}
    ondragleave={() => (dragging = false)}
    {ondrop}
>
    <svg aria-hidden="true" width="36" height="36" viewBox="0 0 24 24" fill="none" stroke="var(--color-accent)" stroke-width="1.5" stroke-linecap="square">
        <path d="M12 16V4" />
        <path d="M7 9l5-5 5 5" />
        <path d="M4 15v5h16v-5" />
    </svg>
    <div class="text-[16px] font-bold">把帳單 e-mail（.eml）拖到這裡</div>
    <div class="text-dim">只收 e-mail，手動轉寄的也行 · 一次可以多個檔案 · 單檔上限 32 MB · 同一個檔案重複上傳不會重複入帳</div>
    <button type="button" class="control min-h-11 border-accent px-4.5" onclick={() => input?.click()}><span class="text-accent">[enter]</span> 選擇檔案</button>
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

<p class="text-dim">
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
                        {@render row(doc, entry.name, entry.size, entry)}
                    {/each}
                </tbody>
            </table>
        </div>
    </section>
{/if}

{#if recent.length > 0}
    <section class="panel px-3.5 pt-1 pb-2.5">
        <div class="pt-2.5 pb-1"><h2 class="label">RECENT · 之前上傳的</h2></div>
        <div class="overflow-x-auto">
            <table class="w-full min-w-[860px] table-fixed border-collapse">
                <colgroup>
                    <col class="w-[30%]" />
                    <col class="w-[84px]" />
                    <col class="w-[172px]" />
                    <col class="w-[84px]" />
                    <col />
                </colgroup>
                <tbody>
                    {#each recent as doc (doc.id)}
                        {@render row(doc, `#${doc.id} ${doc.filename} · ${shortDateTime(doc.receivedAt)}`, doc.sizeBytes)}
                    {/each}
                </tbody>
            </table>
        </div>
    </section>
{/if}

<section class="flex flex-col gap-1.5 rounded-[6px] border border-dashed border-line px-3.5 py-3 text-dim">
    <div class="label">LIFECYCLE · 狀態流程</div>
    <div>
        uploading → stored → parsing → <span class="text-pos">parsed</span> | <span class="text-neg">needs_review</span> | <span class="text-mute">duplicate</span> |
        <span class="text-neg">failed</span>
    </div>
    <div class="text-mute">parser 連不上時會自動重試；檔案、密碼、版面的問題不重試，修好後按「重新解析」。</div>
</section>
