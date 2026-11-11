import type {
    AccountDetail,
    Check,
    Statement,
    Transaction,
} from "#lib/api/client.js";
import {
    describe,
    expect,
    it,
} from "vitest";
import {
    billPoints,
    checkDelta,
    checksText,
    checkText,
    deltaMeaning,
    failingChecks,
    statementStatus,
    statementSummary,
} from "./statements";

const check = (name: string, stated: string, computed: string, unit = "TWD"): Check => ({ name, unit, stated, computed, balanced: stated === computed });

const bill = (...checks: Check[]): Pick<Statement, "balanced" | "checks" | "currency"> => ({ balanced: checks.every(c => c.balanced), checks, currency: "TWD" });

const even = bill(check("本期新增款項", "100", "100"));
const off = bill(check("本期新增款項", "1200", "1080.5"), check("繳款小計", "-500", "-500"), check("e point", "64", "84", "e point"));

describe("statementStatus", () => {
    it("says what became of the document, over whether the bill adds up", () => {
        // A re-parse that failed keeps the statement of the parse before.
        expect(statementStatus(even, { status: "failed" })).toBe("failed");
        expect(statementStatus(off, { status: "needs_review" })).toBe("needs_review");
    });

    it("falls back on its checks without the document", () => {
        expect(statementStatus(even)).toBe("parsed");
        expect(statementStatus(off)).toBe("needs_review");
    });

    it("is pending while a re-parse asked for has not finished", () => {
        expect(statementStatus(even, { status: "parsed" }, { reparsing: true })).toBe("pending");
    });
});

describe("checks", () => {
    it("is what the bill prints less what its lines come to, check by check", () => {
        expect(checkDelta(off.checks[0])).toBe("119.5");
        expect(failingChecks(off).map(c => c.name)).toEqual(["本期新增款項", "e point"]);
    });

    it("names each check that does not balance, with a unit other than the bill's", () => {
        expect(checksText(off)).toBe("本期新增款項 Δ +119.50；e point Δ -20 e point");
        expect(checksText(even)).toBe("balanced");
        expect(checkText(check("新增款項", "1", "0", ""), "TWD")).toBe("新增款項 Δ +1");
    });

    it("says which side has more", () => {
        expect(deltaMeaning("120")).toContain("漏抓");
        expect(deltaMeaning("-120")).toContain("多抓");
    });
});

describe("statementSummary", () => {
    it("says how many lines, whether they add up, when it is due, and whether it was paid", () => {
        const s = { transactionCount: 12, dueDate: "2026-11-25", totalDue: "23104", paidOn: undefined };
        expect(statementSummary({ ...s, ...even })).toBe("12 筆 · balanced · 11/25 到期 23,104");
        expect(statementSummary({ ...s, ...bill(check("本期新增款項", "23104", "22984")), paidOn: "2026-11-20" }))
            .toBe("12 筆 · 本期新增款項 Δ +120 · 需確認 · 11/25 到期 23,104 · 已繳 11/20");
    });
});

describe("billPoints", () => {
    const account = {
        id: 9,
        kind: "points",
        // Newest first: bill 2 is the newest.
        history: [
            { unit: "e point", amount: "120", asOf: "2026-10-31", statementId: 2 },
            { unit: "e point", amount: "100", asOf: "2026-09-30", statementId: 1 },
            { unit: "miles", amount: "5", asOf: "2026-09-30", statementId: 1 },
        ],
        expiries: [{ unit: "e point", amount: "30", by: "2026-12-31" }],
    } as unknown as AccountDetail;
    const earn = { id: 1, accountId: 9, accountKind: "points", unit: "e point", kind: "earn", amount: "20" } as Transaction;

    it("gives each program its lines and its balance after the bill, and what lapses on the newest bill", () => {
        expect(billPoints(2, [account], [earn])).toEqual([
            { account, unit: "e point", lines: [earn], balance: account.history[0], expiries: account.expiries },
        ]);
    });

    it("keeps a program without lines, and no expiries on an older bill", () => {
        const older = billPoints(1, [account], []);
        expect(older.map(p => [p.unit, p.balance?.amount, p.expiries])).toEqual([["e point", "100", []], ["miles", "5", []]]);
    });
});
