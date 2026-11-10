import type { Document } from "#lib/api/client.js";
import {
    describe,
    expect,
    it,
} from "vitest";
import {
    documentFilters,
    documentQuery,
    duplicateOf,
    liveByState,
    type Openable,
    openUrl,
    outcome,
    readDocumentFilters,
    shownCount,
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

const doc = (status: Document["status"], error?: string): Document => ({
    id: 9,
    filename: "a.eml",
    contentType: "message/rfc822",
    sizeBytes: 1,
    sha256: "",
    receivedAt: "2026-11-09T00:00:00+08:00",
    status,
    error,
    warnings: [],
    lineCount: 0,
    accountCount: 0,
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
        const duplicate = doc("duplicate", "same statement as document 335");
        expect(duplicateOf(duplicate)).toBe(335);
        expect(outcome(duplicate).link).toEqual({ href: "/documents/335", text: "→ #335" });
        expect(duplicateOf(doc("failed", "same statement as document 335"))).toBeUndefined();
    });

    it("tells a failure to fix from a file no parser handles", () => {
        expect(outcome(doc("failed", "Internal: layout changed"))).toMatchObject({ tone: "neg", hint: expect.stringContaining("重新解析") });
        expect(outcome(doc("failed", "Unimplemented: no parser claims it"))).toMatchObject({ tone: "dim", hint: expect.stringContaining("沒有 parser") });
    });
});
