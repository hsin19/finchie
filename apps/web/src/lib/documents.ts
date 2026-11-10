import {
    type Document,
    documentContentUrl,
    type DocumentFailure,
    DOCUMENTS_PAGE_SIZE,
    type DocumentStatus,
    previewPdfUrl,
    type StateCount,
    type Statement,
} from "#lib/api/client.js";
import { monthOf } from "#lib/dates.js";
import { institution } from "#lib/institutions.js";
import { formatAmount } from "#lib/money.js";
import { reconciliationDelta } from "#lib/statements.js";
import type { TagStatus } from "#lib/statuses.js";
import type { Upload } from "#lib/upload.svelte.js";
import { resolve } from "$app/paths";
import type { ResolvedPathname } from "$app/types";

export type Openable = Pick<Document, "id" | "contentType" | "status" | "error">;

/** A parser recognized the e-mail, so it knows which attachment is the statement PDF. */
const RECOGNIZED: DocumentStatus[] = ["parsed", "needs_review", "duplicate"];

/**
 * Where a document opens (ADR 0012): its statement PDF, decrypted. An e-mail no parser recognized
 * (failed, skipped, or still parsing) has no statement PDF to pick, so it opens on its own page to
 * see what it is; anything else downloads as uploaded.
 */
export function openUrl(doc: Openable): string {
    if (doc.contentType === "application/pdf") return previewPdfUrl(doc.id);
    if (doc.contentType === "message/rfc822") {
        return RECOGNIZED.includes(doc.status) ? previewPdfUrl(doc.id) : resolve("/documents/[id]", { id: String(doc.id) });
    }
    return documentContentUrl(doc.id);
}

// ---- What the layout knows of documents

/** Which documents are pending, as one comparable value: their ids, in order ("" for none). */
export function pendingKey(pending: readonly Pick<Document, "id">[]): string {
    return pending.map(d => d.id).sort((a, b) => a - b).join();
}

/**
 * The layout's documents by id: the newest (a window, not all of them) and every pending one, which
 * may be older. A document outside both is not here: a page that shows one loads it itself.
 */
export function documentsById(newest: readonly Document[], pending: readonly Document[]): Map<number, Document> {
    return new Map([...newest, ...pending].map(d => [d.id, d]));
}

/**
 * Documents the layout followed while they were pending (`followed`) that it no longer has: done, and
 * older than its window. A page that lists them asks for them again (`?id=`).
 */
export function finishedOutside(followed: Iterable<number>, live: ReadonlyMap<number, Pick<Document, "id">>): number[] {
    return [...followed].filter(id => !live.has(id));
}

/** `items` with each document in `fresh` in place of the one it was. */
export function replaceDocuments<T extends Pick<Document, "id">>(items: readonly T[], fresh: readonly T[]): T[] {
    const byId = new Map(fresh.map(d => [d.id, d]));
    return items.map(d => byId.get(d.id) ?? d);
}

// ---- The document list (/documents)

/** The list's filters: each shows the documents in these states. */
export const documentFilters = [
    { value: "todo", label: "要處理", states: ["failed", "needs_review"] },
    { value: "pending", label: "解析中", states: ["pending"] },
    { value: "parsed", label: "已入帳", states: ["parsed"] },
    { value: "duplicate", label: "重複", states: ["duplicate"] },
    { value: "skipped", label: "不支援", states: ["skipped"] },
] as const satisfies readonly { value: string; label: string; states: readonly DocumentStatus[]; }[];

export type DocumentFilter = (typeof documentFilters)[number];

export interface DocumentFilters {
    show?: DocumentFilter;
    /** Text in the filename. */
    q: string;
}

/** The list's filters from the URL: `?show=` one of documentFilters, `?q=`. */
export function readDocumentFilters(params: Pick<URLSearchParams, "get">): DocumentFilters {
    return {
        show: documentFilters.find(f => f.value === params.get("show")),
        q: params.get("q")?.trim() ?? "",
    };
}

/** The list with this query, as built by withChanges. */
export function documentsQueryHref(query: string): ResolvedPathname {
    return query ? resolve(`/documents?${query}`) : resolve("/documents");
}

/** The query for one page of the list; the next page continues after the last document, `before`. */
export function documentQuery(filters: DocumentFilters, before?: number) {
    return {
        limit: DOCUMENTS_PAGE_SIZE,
        state: filters.show ? [...filters.show.states] : undefined,
        q: filters.q || undefined,
        before,
    };
}

/** How many documents a filter shows, by the list's counts; without one, every document. */
export function shownCount(byState: { state: DocumentStatus; count: number; }[], filter?: DocumentFilter): number {
    const states: readonly DocumentStatus[] | undefined = filter?.states;
    return byState.filter(c => !states || states.includes(c.state)).reduce((n, c) => n + c.count, 0);
}

/** Updates `byState` for any document in `items` whose status in `live` has changed since the page loaded. */
export function liveByState(byState: StateCount[], items: Document[], live: ReadonlyMap<number, Document>): StateCount[] {
    const counts = new Map(byState.map(c => [c.state, c.count]));
    for (const doc of items) {
        const current = live.get(doc.id);
        if (!current) continue;
        const before = doc.status;
        const after = current.status;
        if (before !== after) {
            counts.set(before, (counts.get(before) ?? 0) - 1);
            counts.set(after, (counts.get(after) ?? 0) + 1);
        }
    }
    return [...counts].filter(([, count]) => count > 0).map(([state, count]) => ({ state, count }));
}

// ---- What became of parsing a document

/** What to do about a failure, by what the backend says has to change first. */
const failureHints: Record<DocumentFailure, string> = {
    unsupported: "沒有 parser 認領這封信：寄件者網域或主旨對不上",
    locked: "PDF 打不開：訊息會寫要設哪個 parser 變數，設好後按「重新解析」",
    unreadable: "不是 e-mail 或讀不出來：上傳銀行寄來的原始信（.eml）",
    layout: "版面跟 parser 認得的不一樣：修好 parser 後按「重新解析」",
    unavailable: "parser 一直連不上：backend 每小時會自己再試",
    invalid_response: "parser 回的結果不合規格：修好 parser 後按「重新解析」",
    other: "按「重新解析」再試一次",
};

export function failureHint(failure: DocumentFailure | undefined): string {
    return failureHints[failure ?? "other"];
}

export interface Outcome {
    text: string;
    tone: "ink" | "dim" | "neg";
    /** What to do about it, if anything. */
    hint?: string;
    /** Where what it produced is. */
    link?: { href: ResolvedPathname; text: string; };
}

/** What parsing the document gave, in a line. `statement` is its card bill, if it produced one. */
export function outcome(doc: Document, statement?: Statement): Outcome {
    // A card bill has a page of its own; a deposit or securities statement's lines are on its accounts' pages.
    const deposit = doc.statementKind === "deposit" || doc.statementKind === "securities";
    const link = statement
        ? { href: resolve("/statements/[id]", { id: String(statement.id) }), text: `→ 帳單 ${institution(statement.institution).name} ${monthOf(statement.closingDate)}` }
        : deposit
        ? { href: resolve("/accounts"), text: "→ 帳戶" }
        : undefined;
    switch (doc.status) {
        case "pending":
            return { text: "解析中…", tone: "ink", hint: "通常幾秒；滙豐要 OCR 商店名，約 10 秒" };
        case "parsed": {
            const accounts = deposit ? ` · ${doc.accountCount} 個帳戶` : "";
            return { text: `${doc.lineCount} 筆${accounts} · balanced`, tone: "ink", link };
        }
        case "needs_review": {
            if (!statement) {
                // A deposit statement names each account whose lines do not add up in the document's warnings.
                return { text: doc.warnings.join("；") || "有帳戶的明細加總對不上", tone: "neg", hint: "打開帳戶看是哪一列沒抓到", link };
            }
            const delta = formatAmount(reconciliationDelta(statement), { signed: true });
            return { text: `明細加總跟帳單差 ${delta}`, tone: "neg", hint: "打開帳單看是哪一列沒抓到", link };
        }
        case "duplicate": {
            const original = doc.duplicateOf;
            return original
                ? { text: `跟 #${original} 是同一張，這份不再寫入`, tone: "dim", link: { href: resolve("/documents/[id]", { id: String(original) }), text: `→ #${original}` } }
                : { text: "同一張帳單已經入帳，這份不再寫入", tone: "dim" };
        }
        case "failed":
            return { text: doc.error ?? "failed", tone: "neg", hint: failureHint(doc.failure) };
        case "skipped":
            return { text: doc.error ?? "skipped", tone: "dim", hint: failureHint(doc.failure) };
    }
}

// ---- An upload of this visit, as it goes

export interface UploadRow {
    tag: TagStatus;
    stage: string;
    detail: string;
    tone: Outcome["tone"];
    hint: string;
    link?: Outcome["link"];
    /** How far along, 0 to 100. */
    percent: number;
}

const stages: Record<DocumentStatus, string> = {
    pending: "stored → parsing",
    parsed: "parsed",
    needs_review: "parsed → needs_review",
    duplicate: "parsed → duplicate",
    failed: "parsing → failed",
    skipped: "parsing → skipped",
};

/** An upload, by what became of it: `doc` once the backend has it, `statement` its card bill. */
export function uploadRow(upload: Upload, doc?: Document, statement?: Statement): UploadRow {
    if (upload.error) return { tag: "failed", stage: "upload → failed", detail: upload.error, tone: "neg", hint: "", percent: 100 };
    if (!doc) {
        const sent = Math.round(upload.sent * 100);
        return { tag: "uploading", stage: `uploading ${sent}%`, detail: "上傳中…", tone: "ink", hint: "", percent: sent };
    }
    const result = outcome(doc, statement);
    const same = upload.same && (doc.status === "pending" || doc.status === "parsed");
    return {
        tag: same ? "same" : doc.status,
        stage: stages[doc.status],
        detail: `${upload.same ? `上傳過了，沿用 #${doc.id} · ` : ""}${result.text}`,
        tone: same && doc.status === "parsed" ? "dim" : result.tone,
        hint: result.hint ?? "",
        link: result.link,
        percent: doc.status === "pending" ? 80 : 100,
    };
}

/** How the uploads went, by their tags: done, still going, to look at, and the rest. */
export function uploadSummary(rows: readonly Pick<UploadRow, "tag">[]): { done: number; running: number; failed: number; other: number; } {
    const count = { done: 0, running: 0, failed: 0, other: 0 };
    for (const { tag } of rows) {
        if (tag === "parsed") count.done++;
        else if (tag === "uploading" || tag === "pending") count.running++;
        else if (tag === "failed" || tag === "needs_review") count.failed++;
        else count.other++;
    }
    return count;
}
