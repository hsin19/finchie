import type {
    AccountDetail,
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
    deltaMeaning,
    reconciliationDelta,
    reconciliationText,
    statementStatus,
    statementSummary,
} from "./statements";

const bill = (stated: string, computed: string): Pick<Statement, "reconciliation"> => ({
    reconciliation: { balanced: stated === computed, stated, computed, statedLabel: "本期應繳" },
});

describe("statementStatus", () => {
    it("says what became of the document, over whether the bill adds up", () => {
        // A re-parse that failed keeps the statement of the parse before.
        expect(statementStatus(bill("100", "100"), { status: "failed" })).toBe("failed");
        expect(statementStatus(bill("100", "90"), { status: "needs_review" })).toBe("needs_review");
    });

    it("falls back on the reconciliation without the document", () => {
        expect(statementStatus(bill("100", "100"))).toBe("parsed");
        expect(statementStatus(bill("100", "90"))).toBe("needs_review");
    });

    it("is pending while a re-parse asked for has not finished", () => {
        expect(statementStatus(bill("100", "100"), { status: "parsed" }, { reparsing: true })).toBe("pending");
    });
});

describe("reconciliation", () => {
    it("is what the bill prints less what its lines add up to", () => {
        expect(reconciliationDelta(bill("1200", "1080.5"))).toBe("119.5");
        expect(reconciliationText(bill("1200", "1080.5"))).toBe("Δ +119.50");
        expect(reconciliationText(bill("-80", "-80"))).toBe("balanced");
    });

    it("says which side has more", () => {
        expect(deltaMeaning("120")).toContain("漏抓");
        expect(deltaMeaning("-120")).toContain("多抓");
    });
});

describe("statementSummary", () => {
    it("says how many lines, whether they add up, when it is due, and whether it was paid", () => {
        const s = { transactionCount: 12, dueDate: "2026-11-25", totalDue: "23104", paidOn: undefined, ...bill("23104", "23104") };
        expect(statementSummary(s)).toBe("12 筆 · balanced · 11/25 到期 23,104");
        expect(statementSummary({ ...s, ...bill("23104", "22984"), paidOn: "2026-11-20" })).toBe("12 筆 · Δ +120 · 需確認 · 11/25 到期 23,104 · 已繳 11/20");
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
