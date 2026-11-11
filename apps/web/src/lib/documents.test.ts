import type {
    Document,
    Statement,
} from "#lib/api/client.js";
import {
    describe,
    expect,
    it,
} from "vitest";
import {
    documentFilters,
    documentQuery,
    documentsById,
    finishedOutside,
    liveByState,
    type Openable,
    openUrl,
    outcome,
    pendingKey,
    readDocumentFilters,
    replaceDocuments,
    shownCount,
    uploadRow,
    uploadSummary,
} from "./documents";

const pdf = (status: Openable["status"], error?: string): Openable => ({ id: 7, contentType: "application/pdf", status, error });
const email = (status: Openable["status"], error?: string): Openable => ({ id: 7, contentType: "message/rfc822", status, error });

describe("openUrl", () => {
    it("opens a PDF decrypted, whatever became of parsing it", () => {
        expect(openUrl(pdf("parsed"))).toBe("/api/documents/7/preview/pdf");
        expect(openUrl(pdf("failed", "FailedPrecondition: wrong password"))).toBe("/api/documents/7/preview/pdf");
        expect(openUrl(pdf("pending"))).toBe("/api/documents/7/preview/pdf");
    });

    it("opens the statement PDF of an e-mail a parser recognized", () => {
        expect(openUrl(email("parsed"))).toBe("/api/documents/7/preview/pdf");
        expect(openUrl(email("needs_review"))).toBe("/api/documents/7/preview/pdf");
        expect(openUrl(email("duplicate"))).toBe("/api/documents/7/preview/pdf");
    });

    it("opens any other e-mail on its own page", () => {
        expect(openUrl(email("failed", "Internal: layout changed"))).toBe("/documents/7");
        expect(openUrl(email("failed", "Unimplemented: no parser for this sender"))).toBe("/documents/7");
        expect(openUrl(email("pending"))).toBe("/documents/7");
    });

    it("downloads anything else as uploaded", () => {
        expect(openUrl({ id: 7, contentType: "image/png", status: "failed" })).toBe("/api/documents/7/content");
    });
});

const doc = (status: Document["status"], more: Partial<Document> = {}): Document => ({
    id: 9,
    filename: "a.eml",
    contentType: "message/rfc822",
    sizeBytes: 1,
    sha256: "",
    receivedAt: "2026-11-09T00:00:00+08:00",
    status,
    warnings: [],
    lineCount: 0,
    accountCount: 0,
    ...more,
});

describe("the document list", () => {
    it("reads its filters from the URL, ignoring one it does not know", () => {
        expect(readDocumentFilters(new URLSearchParams("show=todo&q=%20台新%20"))).toEqual({ show: documentFilters[0], q: "台新" });
        expect(readDocumentFilters(new URLSearchParams("show=nope"))).toEqual({ show: undefined, q: "" });
    });

    it("asks for the states a filter shows, continuing after a document", () => {
        const todo = readDocumentFilters(new URLSearchParams("show=todo"));
        expect(documentQuery(todo, 42)).toEqual({ limit: 50, state: ["failed", "needs_review"], q: undefined, before: 42 });
        expect(documentQuery({ q: "" })).toEqual({ limit: 50, state: undefined, q: undefined, before: undefined });
    });

    it("counts what a filter shows: to do is the failed and those that need review", () => {
        const byState = [{ state: "parsed", count: 5 }, { state: "needs_review", count: 2 }, { state: "failed", count: 3 }, { state: "skipped", count: 4 }] as const;
        expect(shownCount([...byState], documentFilters[0])).toBe(5);
        expect(shownCount([...byState])).toBe(14);
    });

    it("moves a document's count to its new state when a re-parse finishes", () => {
        const byState = [{ state: "parsed" as const, count: 5 }, { state: "failed" as const, count: 1 }];
        const live = new Map([[9, doc("parsed")]]);
        expect(liveByState(byState, [doc("failed")], live)).toEqual([{ state: "parsed", count: 6 }]);
    });
});

describe("outcome", () => {
    it("names the document a duplicate repeats and links to it", () => {
        expect(outcome(doc("duplicate", { duplicateOf: 335 })).link).toEqual({ href: "/documents/335", text: "→ #335" });
        expect(outcome(doc("duplicate")).link).toBeUndefined();
    });

    it("tells a failure to fix from a file no parser handles, by what the backend says has to change", () => {
        expect(outcome(doc("failed", { failure: "layout", error: "layout changed" }))).toMatchObject({ text: "layout changed", tone: "neg", hint: expect.stringContaining("重新解析") });
        expect(outcome(doc("skipped", { failure: "unsupported", error: "no parser claims it" }))).toMatchObject({ tone: "dim", hint: expect.stringContaining("沒有 parser") });
        expect(outcome(doc("failed", { failure: "unavailable" })).hint).toContain("自己再試");
    });

    it("names each check of a bill that does not add up", () => {
        const statement = {
            id: 5,
            institution: "esun",
            closingDate: "2026-10-07",
            currency: "TWD",
            balanced: false,
            checks: [
                { name: "本期新增款項", unit: "TWD", stated: "1200", computed: "1080", balanced: false },
                { name: "繳款小計", unit: "TWD", stated: "-500", computed: "-500", balanced: true },
            ],
        } as Statement;
        expect(outcome(doc("needs_review"), statement)).toMatchObject({ text: "明細加總跟帳單對不上：本期新增款項 Δ +120", tone: "neg", link: { href: "/statements/5" } });
    });
});

describe("what the layout knows of documents", () => {
    const doc = (id: number, status: Document["status"]) => ({ id, status }) as Document;

    it("compares pending documents by their ids, in any order", () => {
        expect(pendingKey([doc(12, "pending"), doc(3, "pending")])).toBe(pendingKey([doc(3, "pending"), doc(12, "pending")]));
        expect(pendingKey([])).toBe("");
    });

    it("has the newest and every pending one, the pending state winning", () => {
        const byId = documentsById([doc(40, "parsed"), doc(39, "failed")], [doc(39, "pending"), doc(2, "pending")]);
        expect([...byId.keys()].sort((a, b) => a - b)).toEqual([2, 39, 40]);
        expect(byId.get(39)?.status).toBe("pending");
    });
});

describe("uploads", () => {
    const upload = { key: 0, name: "a.eml", size: 10, sent: 0.42 };
    const doc = (status: Document["status"]) => ({ id: 7, status, lineCount: 3, accountCount: 0, warnings: [] }) as unknown as Document;

    it("goes from sending to what parsing gave", () => {
        expect(uploadRow(upload)).toMatchObject({ tag: "uploading", stage: "uploading 42%", percent: 42 });
        expect(uploadRow({ ...upload, sent: 1, documentId: 7 }, doc("pending"))).toMatchObject({ tag: "pending", percent: 80 });
        expect(uploadRow({ ...upload, sent: 1, documentId: 7 }, doc("parsed"))).toMatchObject({ tag: "parsed", detail: "3 筆 · balanced", percent: 100 });
        expect(uploadRow({ ...upload, error: "not an e-mail" })).toMatchObject({ tag: "failed", detail: "not an e-mail", tone: "neg" });
    });

    it("says when the same bytes came before", () => {
        expect(uploadRow({ ...upload, documentId: 7, same: true }, doc("parsed"))).toMatchObject({ tag: "same", detail: "上傳過了，沿用 #7 · 3 筆 · balanced", tone: "dim" });
    });

    it("counts how they went", () => {
        expect(uploadSummary([{ tag: "parsed" }, { tag: "pending" }, { tag: "uploading" }, { tag: "needs_review" }, { tag: "same" }])).toEqual({ done: 1, running: 2, failed: 1, other: 1 });
    });
});

describe("documents older than the layout's window", () => {
    const doc = (id: number, status: Document["status"]) => ({ id, status }) as Document;

    it("asks again for one the layout followed while pending and no longer has", () => {
        const live = new Map([[40, doc(40, "parsed")], [3, doc(3, "pending")]]);
        expect(finishedOutside([3, 2, 40], live)).toEqual([2]);
        expect(finishedOutside([], live)).toEqual([]);
    });

    it("puts the fresh one in its place in the list", () => {
        const items = [doc(9, "parsed"), doc(2, "failed"), doc(1, "parsed")];
        expect(replaceDocuments(items, [doc(2, "parsed")]).map(d => [d.id, d.status])).toEqual([[9, "parsed"], [2, "parsed"], [1, "parsed"]]);
    });
});
