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
    it("takes the backend's flow, seen from the owner", () => {
        expect(flowOf({ flow: "-23450.56" })).toBe("-23450.56");
        expect(flowOf({ flow: "60000" })).toBe("60000");
    });

    it("counts a linked line or a card payment as a transfer", () => {
        expect(isTransfer({ kind: "payment" })).toBe(true);
        expect(isTransfer({ kind: "other", counterpart: { id: 1, statementId: 1, date: "2026-09-25", accountKind: "deposit", institution: "taishin" } })).toBe(true);
        expect(isTransfer({ kind: "other" })).toBe(false);
    });

    it("adds up each unit apart, TWD first, leaving transfers out", () => {
        expect(netByUnit([
            { accountKind: "deposit", kind: "other", flow: "50", unit: "USD" },
            { accountKind: "credit_card", kind: "purchase", flow: "-289", unit: "TWD" },
            { accountKind: "deposit", kind: "other", flow: "60000", unit: "TWD" },
            { accountKind: "credit_card", kind: "payment", flow: "500", unit: "TWD" },
            // Shares bought, points earned: no money.
            { accountKind: "securities", kind: "buy", flow: "1000", unit: "範例電子" },
            { accountKind: "points", kind: "earn", flow: "64", unit: "範例點數" },
        ], "TWD")).toEqual([{ unit: "TWD", amount: "59711" }, { unit: "USD", amount: "50" }]);
    });
});
