import {
    describe,
    expect,
    it,
} from "vitest";
import type {
    Account,
    KindTotal,
    Statement,
    Transaction,
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
    reward,
    rewardText,
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
        balanced: true,
        checks: [{ name: "本期新增款項", unit: "TWD", computed: totalDue, stated: totalDue, balanced: true }],
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

describe("reward", () => {
    const rebate = (amount: string, count: number): KindTotal => ({ kind: "rebate", count, amount, flow: amount });
    const points = (unit: string, kind: Transaction["kind"], amount: string, id = 1): Transaction => ({
        id,
        statementId: 7,
        institution: "cathay",
        transactionDate: "2026-10-07",
        description: "本期新增",
        kind,
        amount,
        flow: "0",
        unit,
        accountKind: "points",
    });
    /** A points account holding `unit` at `twd` NT$ a point, or with no price at all. */
    const priced = (unit: string, twd?: string): Account => ({
        id: 9,
        institution: "cathay",
        kind: "points",
        side: "asset",
        number: "",
        continuity: "checked",
        gaps: [],
        expiries: [],
        balances: [{ unit, asOf: "2026-10-07", amount: "5000", ...(twd ? { price: { asOf: "2026-10-01T00:00:00Z", source: "owner", quote: "TWD", quoted: twd, twd } } : {}) }],
    });

    it("is the bills' cash rebates when they report no points", () => {
        // A rebate is credited to the bill, so it is printed negative.
        expect(reward(rebate("-300", 2), [], [])).toEqual({ cash: "300", cashCount: 2, points: [], total: "300" });
        expect(reward(undefined, [], [])).toEqual({ cash: "0", cashCount: 0, points: [], total: "0" });
    });

    it("adds the points earned on them, campaigns and adjustments included, at each program's price", () => {
        const lines = [points("小樹點(信用卡)", "earn", "1000", 1), points("小樹點(信用卡)", "adjust", "200", 2), points("小樹點(信用卡)", "adjust", "-50", 3)];
        expect(reward(rebate("-300", 1), lines, [priced("小樹點(信用卡)", "0.5")])).toEqual({
            cash: "300",
            cashCount: 1,
            points: [{ unit: "小樹點(信用卡)", amount: "1150", worth: "575.0" }],
            total: "875.0",
        });
    });

    it("keeps a program without a price out of the total, and leaves out points used", () => {
        const lines = [points("e point", "earn", "800", 1), points("e point", "redeem", "-500", 2), points("小樹點(信用卡)", "earn", "100", 3)];
        const result = reward(undefined, lines, [priced("e point"), priced("小樹點(信用卡)", "1")]);
        expect(result.points).toEqual([{ unit: "e point", amount: "800" }, { unit: "小樹點(信用卡)", amount: "100", worth: "100" }]);
        expect(result.total).toBe("100");
    });

    it("says what it is made of: cash, points by their worth, and points without a price", () => {
        const lines = [points("小樹點(信用卡)", "earn", "1150", 1), points("e point", "earn", "800", 2)];
        expect(rewardText(reward(rebate("-300", 2), lines, [priced("小樹點(信用卡)", "0.5")]))).toBe("2 筆現金回饋 · 點數 ≈ NT$575 · +800 e point 沒有價格");
        expect(rewardText(reward(undefined, [], []))).toBe("這期沒有回饋");
    });

    it("leaves out a program whose points net to nothing", () => {
        expect(reward(undefined, [points("e point", "earn", "100", 1), points("e point", "adjust", "-100", 2)], []).points).toEqual([]);
    });
});
