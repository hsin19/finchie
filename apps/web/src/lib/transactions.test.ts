import type { Transaction } from "#lib/api/client.js";
import {
    describe,
    expect,
    it,
} from "vitest";
import {
    amountText,
    amountTone,
    byDay,
    counterpartLink,
    dayTotal,
    original,
    rate,
    unitSuffix,
} from "./transactions";

const line = (fields: Partial<Transaction>): Transaction => ({
    id: 1,
    statementId: 1,
    institution: "esun",
    transactionDate: "2026-10-08",
    description: "",
    kind: "purchase",
    amount: "100",
    flow: "-100",
    unit: "TWD",
    accountKind: "credit_card",
    ...fields,
} as Transaction);

describe("a transaction's row", () => {
    it("shows the amount from the owner's side, colored by what it is", () => {
        const purchase = line({ amount: "100", flow: "-100" });
        expect(amountText(purchase)).toBe("-100");
        expect(amountTone(purchase)).toBe("text-ink");
        expect(amountTone(line({ kind: "refund", amount: "-100", flow: "100" }))).toBe("text-pos");
        expect(amountTone(line({ kind: "payment", amount: "-500", flow: "500" }))).toBe("text-mute");
        expect(amountTone(line({ kind: "annual_fee", amount: "1800", flow: "-1800" }))).toBe("text-neg");
    });

    it("names a unit only when it is not the base currency", () => {
        expect(unitSuffix({ unit: "TWD" }, "TWD")).toBe("");
        expect(unitSuffix({ unit: "USD" }, "TWD")).toBe("USD");
    });

    it("shows the foreign amount and the rate applied", () => {
        const fx = { amount: "321", originalAmount: "10", originalCurrency: "USD" };
        expect(original(fx)).toBe("USD 10");
        expect(rate(fx, "TWD")).toBe("32.10 TWD / USD");
        expect(rate({ amount: "1" }, "TWD")).toBeUndefined();
    });

    it("groups lines by day and adds each day up", () => {
        const items = [line({ id: 1, flow: "-100" }), line({ id: 2, flow: "-20" }), line({ id: 3, transactionDate: "2026-10-07", flow: "-5" })];
        expect(byDay(items).map(d => [d.date, d.items.length])).toEqual([["2026-10-08", 2], ["2026-10-07", 1]]);
        expect(dayTotal(items.slice(0, 2), "TWD")).toBe("-120");
    });

    it("links where a transfer's money went", () => {
        expect(counterpartLink({ id: 4, statementId: 2, date: "2026-10-01", accountKind: "deposit", institution: "esun", accountId: 7, accountName: "臺幣活存" }))
            .toEqual({ href: "/accounts/7", text: "玉山 臺幣活存" });
        expect(counterpartLink({ id: 4, statementId: 2, date: "2026-10-01", accountKind: "credit_card", institution: "hsbc" }))
            .toEqual({ href: "/statements/2?line=4", text: "滙豐 信用卡帳單" });
    });
});
