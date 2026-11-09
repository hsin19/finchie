import type {
    Account,
    Balance,
    Statement,
} from "#lib/api/client.js";
import {
    describe,
    expect,
    it,
} from "vitest";
import {
    accountSort,
    historyByDay,
    holdingsByWorth,
    isSmallAccount,
    leftOut,
    listedBalances,
    sortAccounts,
    totalWorth,
    unpaidBills,
} from "./accounts.js";

function account(id: number, worth?: string): Account {
    return { id, institution: "esun", kind: "deposit", number: String(id), balances: [], continuity: "checked", gaps: [], worth };
}

describe("accounts", () => {
    const accounts = [account(1, "500"), account(2), account(3, "15124.5"), account(4, "-300"), account(5, "500")];

    it("sorts by balance unless asked for the bank order", () => {
        expect(accountSort(null)).toBe("balance");
        expect(accountSort("nonsense")).toBe("balance");
        expect(accountSort("bank")).toBe("bank");
        expect(sortAccounts(accounts, "bank").map(a => a.id)).toEqual([1, 2, 3, 4, 5]);
    });

    it("sorts by worth, largest first, an account without a worth last and ties in bank order", () => {
        expect(sortAccounts(accounts, accountSort("balance")).map(a => a.id)).toEqual([3, 1, 5, 4, 2]);
    });

    it("adds up worths exactly and counts the accounts without one", () => {
        expect(totalWorth(accounts)).toEqual({ total: "15824.5", missing: 1 });
    });

    it("owes each bank's newest bill that is not seen paid and owes something", () => {
        const bill = (id: number, institution: string, closingDate: string, totalDue: string, paidOn?: string): Statement => ({
            id,
            documentId: id,
            institution,
            closingDate,
            dueDate: closingDate,
            currency: "TWD",
            totalDue,
            reconciliation: { balanced: true, computed: "0", stated: "0", statedLabel: "" },
            transactionCount: 0,
            paidOn,
        });
        const bills = [
            bill(1, "esun", "2026-09-07", "6350", "2026-09-22"),
            bill(2, "esun", "2026-10-07", "780"),
            bill(3, "taishin", "2026-10-07", "32646", "2026-10-22"),
            bill(4, "cathay", "2026-09-01", "500"),
            bill(5, "cathay", "2026-10-01", "0"),
        ];
        expect(unpaidBills(bills).map(b => b.id)).toEqual([2]);
    });
});

describe("exchange holdings", () => {
    const coin = (unit: string, asOf: string, amount: string, worth?: string): Balance => ({ unit, asOf, amount, worth });

    it("lists holdings by worth, those without a rate last", () => {
        const held = [coin("DUST", "2026-11-09", "3"), coin("USDT", "2026-11-09", "100", "3200"), coin("BTC", "2026-11-09", "0.1", "320000"), coin("AAA", "2026-11-09", "1")];
        expect(holdingsByWorth(held).map(b => b.unit)).toEqual(["BTC", "USDT", "AAA", "DUST"]);
    });

    it("groups a snapshot history by day, newest first", () => {
        const history = [coin("BTC", "2026-11-09", "0"), coin("USDT", "2026-11-09", "500"), coin("BTC", "2026-11-08", "0.1")];
        expect(historyByDay(history)).toEqual([
            { asOf: "2026-11-09", balances: [history[0], history[1]] },
            { asOf: "2026-11-08", balances: [history[2]] },
        ]);
    });
});

describe("small accounts and balances", () => {
    const money = (unit: string, amount: string, worth?: string): Balance => ({ unit, asOf: "2026-09-30", amount, worth });
    const holding = (balances: Balance[], worth?: string, gaps = 0): Account => ({
        id: 1,
        institution: "nextbank",
        kind: "deposit",
        number: "1",
        balances,
        continuity: "checked",
        gaps: Array.from({ length: gaps }, () => ({ unit: "TWD", from: "2026-08-31", to: "2026-09-30", unseen: "1" })),
        worth,
    });

    it("lists balances by worth, and only those of NT$1,000 or more when small ones are hidden", () => {
        const foreign = holding([money("EUR", "0", "0"), money("JPY", "0", "0"), money("USD", "9040.21", "296380.87")], "296380.87");
        // A balance at 0 is none at all, whether small ones are hidden or not.
        expect(listedBalances(foreign, false).listed.map(b => b.unit)).toEqual(["USD"]);
        const hidden = listedBalances(foreign, true);
        expect(hidden).toEqual({ listed: [foreign.balances[2]], others: 0, othersAtMost: undefined, unpriced: 0 });
        expect(leftOut(hidden)).toBe("");
        // Dust worth nothing once rounded is not counted either.
        const dusty = holding([money("USD", "9040.21", "296380.87"), money("PEPE", "1", "0.00")], "296380.87");
        expect(leftOut(listedBalances(dusty, true))).toBe("");
    });

    it("lists the three largest and says the most any other is worth", () => {
        const coins = holding(
            [money("BNB", "5.3", "121513.22"), money("USDT", "97.5", "3193.38"), money("ETH", "1", "73813.25"), money("USDC", "231", "7589.22"), money("SOL", "0.001", "3.68"), money("TAO", "0.01")],
            "215889",
        );
        const view = listedBalances(coins, true);
        expect(view.listed.map(b => b.unit)).toEqual(["BNB", "ETH", "USDC"]);
        expect(leftOut(view)).toBe("另 2 種 ≤ NT$3,193、1 種沒有匯率");
        expect(listedBalances(coins, false).listed).toHaveLength(6);
    });

    it("lists nothing when no balance is known to reach NT$1,000, and says why", () => {
        // MAX: its own token has no rate; nothing tells what it is worth on its own.
        const max = holding([money("TWD", "0.27", "0.27"), money("BTC", "0.00002639", "65.55"), money("MAX", "570.12")], "4251.35");
        const view = listedBalances(max, true);
        expect(view.listed).toEqual([]);
        expect(leftOut(view)).toBe("另 2 種 ≤ NT$66、1 種沒有匯率");
        expect(leftOut(listedBalances(max, false))).toBe("");
    });

    it("counts an account as small under NT$1,000 either way, gaps or not, unless its worth is unknown", () => {
        expect(isSmallAccount(holding([], "999.99"))).toBe(true);
        expect(isSmallAccount(holding([], "-999.99"))).toBe(true);
        expect(isSmallAccount(holding([], "1000"))).toBe(false);
        expect(isSmallAccount(holding([], "-5000"))).toBe(false);
        expect(isSmallAccount(holding([]))).toBe(false);
        expect(isSmallAccount(holding([], "0", 1))).toBe(true);
    });
});
