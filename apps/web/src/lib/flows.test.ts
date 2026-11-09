import {
    describe,
    expect,
    it,
} from "vitest";
import {
    flowOf,
    isTransfer,
    netByUnit,
} from "./flows";

describe("flows", () => {
    it("turns a card line's sign, net of its children, and keeps a deposit line's", () => {
        expect(flowOf({ accountKind: "credit_card", kind: "purchase", amount: "23104", children: [{ id: 2, statementId: 1, transactionDate: "2026-10-08", kind: "foreign_transaction_fee", amount: "346.56" }] }))
            .toBe("-23450.56");
        expect(flowOf({ accountKind: "deposit", kind: "other", amount: "60000" })).toBe("60000");
        expect(flowOf({ accountKind: "credit_card", kind: "purchase", amount: "100", flow: "-100" })).toBe("-100");
    });

    it("counts a linked line or a card payment as a transfer", () => {
        expect(isTransfer({ kind: "payment" })).toBe(true);
        expect(isTransfer({ kind: "other", counterpart: { id: 1, statementId: 1, date: "2026-09-25", accountKind: "deposit", institution: "taishin" } })).toBe(true);
        expect(isTransfer({ kind: "other" })).toBe(false);
    });

    it("adds up each unit apart, TWD first, leaving transfers out", () => {
        expect(netByUnit([
            { accountKind: "deposit", kind: "other", amount: "50", unit: "USD" },
            { accountKind: "credit_card", kind: "purchase", amount: "289", flow: "-289", unit: "TWD" },
            { accountKind: "deposit", kind: "other", amount: "60000", unit: "TWD" },
            { accountKind: "credit_card", kind: "payment", amount: "-500", flow: "500", unit: "TWD" },
            // Shares bought, points earned: no money.
            { accountKind: "securities", kind: "buy", amount: "1000", flow: "1000", unit: "範例電子" },
            { accountKind: "points", kind: "earn", amount: "64", flow: "64", unit: "範例點數" },
        ])).toEqual([{ unit: "TWD", amount: "59711" }, { unit: "USD", amount: "50" }]);
    });
});
