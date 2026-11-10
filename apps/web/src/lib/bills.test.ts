import {
    describe,
    expect,
    it,
} from "vitest";
import type {
    KindTotal,
    Statement,
    TransactionSearch,
} from "./api/client";
import {
    closingMonths,
    closingRange,
    dueBills,
    fxFeeShare,
    monthlyBills,
    pendingInstitutions,
    percentChange,
} from "./bills";

function statement(institution: string, closingDate: string, totalDue: string): Statement {
    return {
        id: 0,
        documentId: 0,
        institution,
        closingDate,
        dueDate: closingDate,
        currency: "TWD",
        totalDue,
        transactionCount: 0,
        reconciliation: { balanced: true, computed: totalDue, stated: totalDue, statedLabel: "" },
    };
}

const statements = [
    statement("hsbc", "2026-11-03", "1200"),
    statement("esun", "2026-10-27", "300.5"),
    statement("hsbc", "2026-10-03", "800"),
    statement("taishin", "2026-10-01", "5000"),
    statement("taishin", "2026-09-01", "4000"),
];

describe("bills", () => {
    it("lists the months with a statement, newest first", () => {
        expect(closingMonths(statements)).toEqual(["2026-11", "2026-10", "2026-09"]);
    });

    it("sums each month by bank, banks in chart order, empty months included", () => {
        const [october, november, december] = monthlyBills(statements, ["2026-10", "2026-11", "2026-12"]);
        expect(october).toEqual({
            month: "2026-10",
            total: "6100.5",
            parts: [
                { institution: "taishin", amount: "5000" },
                { institution: "hsbc", amount: "800" },
                { institution: "esun", amount: "300.5" },
            ],
        });
        expect(november.parts).toEqual([{ institution: "hsbc", amount: "1200" }]);
        expect(december).toEqual({ month: "2026-12", total: "0", parts: [] });
    });

    it("knows which banks have not closed this month yet", () => {
        expect(pendingInstitutions(statements, "2026-11", "2026-10")).toEqual(["taishin", "esun"]);
        expect(pendingInstitutions(statements, "2026-10", "2026-09")).toEqual([]);
    });

    it("filters transactions by the statement's closing month", () => {
        expect(closingRange("2026-02")).toEqual({ closingFrom: "2026-02-01", closingTo: "2026-02-28" });
    });

    it("compares only with a positive amount", () => {
        expect(percentChange("150", "100")).toBe(50);
        expect(percentChange("150", "0")).toBeUndefined();
    });

    it("takes the fee share of overseas purchases as printed, refunds or not", () => {
        // kind=purchase searches: `sum` is net of the folded fee and refund, byKind counts lines as printed.
        const search = (sum: string, byKind: KindTotal[]): TransactionSearch => ({ items: [], total: 1, sum, flows: [], byKind, byCard: [], byAccount: [] });
        const fee: KindTotal = { kind: "foreign_transaction_fee", count: 1, amount: "63", flow: "-63" };
        const booking: KindTotal = { kind: "purchase", count: 1, amount: "4200", flow: "-4200" };
        const refund: KindTotal = { kind: "refund", count: 1, amount: "-4200", flow: "4200" };
        const all = search("1063", [{ kind: "purchase", count: 2, amount: "5200", flow: "-5200" }, fee, refund]);
        expect(fxFeeShare(all, search("63", [booking, refund]))).toEqual({ percent: 1.5, of: "4200" });
        expect(fxFeeShare(all, search("0", []))).toBeUndefined();
    });
});

describe("dueBills", () => {
    it("adds up the bills due today or later that are not seen paid, and says the next due date", () => {
        const due = (id: number, dueDate: string, totalDue: string, paidOn?: string) => ({ ...statement("esun", "2026-10-07", totalDue), id, dueDate, paidOn });
        expect(dueBills([due(1, "2026-11-10", "100"), due(2, "2026-11-25", "200.5"), due(3, "2026-11-09", "999"), due(4, "2026-11-20", "50", "2026-11-08")], "2026-11-10"))
            .toEqual({ amount: "300.5", next: "2026-11-10", count: 2 });
        expect(dueBills([], "2026-11-10")).toEqual({ amount: "0", next: undefined, count: 0 });
    });
});
