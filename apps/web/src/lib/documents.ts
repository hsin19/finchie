import {
    type Document,
    documentContentUrl,
    DOCUMENTS_PAGE_SIZE,
    type DocumentState,
    type DocumentStatus,
    previewPdfUrl,
    type StateCount,
    type Statement,
} from "#lib/api/client.js";
import { monthOf } from "#lib/dates.js";
import {
    documentStatus,
    institution,
} from "#lib/labels.js";
import {
    formatAmount,
    subtract,
} from "#lib/money.js";
import { resolve } from "$app/paths";

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

// ---- The document list (/documents)

/** The list's filters: each shows the documents in these states. */
export const documentFilters = [
    { value: "todo", label: "要處理", states: ["failed", "needs_review"] },
    { value: "pending", label: "解析中", states: ["pending"] },
    { value: "parsed", label: "已入帳", states: ["parsed"] },
    { value: "duplicate", label: "重複", states: ["duplicate"] },
    { value: "skipped", label: "不支援", states: ["skipped"] },
] as const satisfies readonly { value: string; label: string; states: readonly DocumentState[]; }[];

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
export function shownCount(byState: { state: DocumentState; count: number; }[], filter?: DocumentFilter): number {
    const states: readonly DocumentState[] | undefined = filter?.states;
    return byState.filter(c => !states || states.includes(c.state)).reduce((n, c) => n + c.count, 0);
}

/** Updates `byState` for any document in `items` whose status in `live` has changed since the page loaded. */
export function liveByState(byState: StateCount[], items: Document[], live: ReadonlyMap<number, Document>): StateCount[] {
    const counts = new Map(byState.map(c => [c.state, c.count]));
    for (const doc of items) {
        const current = live.get(doc.id);
        if (!current) continue;
        const before = documentStatus(doc);
        const after = documentStatus(current);
        if (before !== after) {
            counts.set(before, (counts.get(before) ?? 0) - 1);
            counts.set(after, (counts.get(after) ?? 0) + 1);
        }
    }
    return [...counts].filter(([, count]) => count > 0).map(([state, count]) => ({ state, count }));
}

// ---- What became of parsing a document

/** What to do about a failure, by the gRPC code the worker recorded ("FailedPrecondition: ..."). */
export function failureHint(error: string): string {
    if (error.startsWith("Unimplemented")) return "沒有 parser 認領這封信：寄件者網域或主旨對不上";
    if (error.startsWith("FailedPrecondition")) return "PDF 打不開：訊息會寫要設哪個 parser 變數，設好後按「重新解析」";
    if (error.startsWith("InvalidArgument")) return "不是 e-mail 或讀不出來：上傳銀行寄來的原始信（.eml）";
    if (error.startsWith("Internal")) return "版面跟 parser 認得的不一樣：修好 parser 後按「重新解析」";
    return "按「重新解析」再試一次";
}

/** The document a duplicate repeats, as the backend's reason names it ("same statement as document 335"). */
export function duplicateOf(doc: Pick<Document, "status" | "error">): number | undefined {
    if (doc.status !== "duplicate") return undefined;
    const match = /document (\d+)$/.exec(doc.error ?? "");
    return match ? Number(match[1]) : undefined;
}

export interface Outcome {
    text: string;
    tone: "ink" | "dim" | "neg";
    /** What to do about it, if anything. */
    hint?: string;
    /** Where what it produced is. */
    link?: { href: string; text: string; };
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
            const delta = formatAmount(subtract(statement.reconciliation.stated, statement.reconciliation.computed), { signed: true });
            return { text: `明細加總跟帳單差 ${delta}`, tone: "neg", hint: "打開帳單看是哪一列沒抓到", link };
        }
        case "duplicate": {
            const original = duplicateOf(doc);
            return original
                ? { text: `跟 #${original} 是同一張，這份不再寫入`, tone: "dim", link: { href: resolve("/documents/[id]", { id: String(original) }), text: `→ #${original}` } }
                : { text: doc.error ?? "同一張帳單已經入帳，這份不再寫入", tone: "dim" };
        }
        case "failed":
            return {
                text: doc.error ?? "failed",
                tone: documentStatus(doc) === "skipped" ? "dim" : "neg",
                hint: failureHint(doc.error ?? ""),
            };
    }
}
