import type {
    Account,
    Balance,
    Position,
} from "#lib/api/client.js";
import {
    describe,
    expect,
    it,
} from "vitest";
import {
    accountPage,
    accountsHref,
    accountSort,
    accountStatus,
    daysAgo,
    describePrice,
    historyByDay,
    holdingsByWorth,
    isSmallAccount,
    lapsesSoon,
    leftOut,
    listedBalances,
    readAccountsView,
    shownNumber,
    shownPositions,
    sortAccounts,
} from "./accounts.js";

function account(id: number, worth?: string): Account {
    return { id, institution: "esun", kind: "deposit", number: String(id), balances: [], continuity: "checked", gaps: [], expiries: [], worth };
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
        expiries: [],
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
        expect(leftOut(view)).toBe("另 2 種 ≤ NT$3,193、1 種沒有價格");
        expect(listedBalances(coins, false).listed).toHaveLength(6);
    });

    it("lists nothing when no balance is known to reach NT$1,000, and says why", () => {
        // MAX: its own token has no rate; nothing tells what it is worth on its own.
        const max = holding([money("TWD", "0.27", "0.27"), money("BTC", "0.00002639", "65.55"), money("MAX", "570.12")], "4251.35");
        const view = listedBalances(max, true);
        expect(view.listed).toEqual([]);
        expect(leftOut(view)).toBe("另 2 種 ≤ NT$66、1 種沒有價格");
        expect(leftOut(listedBalances(max, false))).toBe("");
    });

    it("lists the first few of an account with nothing to weigh by worth, such as points", () => {
        const points = holding([money("小樹點(信用卡)", "3253"), money("亞洲萬里通里數", "500"), money("A", "1"), money("B", "1")]);
        const view = listedBalances(points, true);
        expect(view.listed).toHaveLength(3);
        expect(leftOut(view)).toBe("另 1 種沒有價格");
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

describe("lapsing points", () => {
    it("stands out within sixty days", () => {
        expect(lapsesSoon("2027-01-08", "2026-11-09")).toBe(true);
        expect(lapsesSoon("2027-01-09", "2026-11-09")).toBe(false);
        expect(lapsesSoon("2026-11-30", "2026-11-09", 14)).toBe(false);
    });
});

describe("describePrice", () => {
    it("says what one unit comes to in NT$, where the price came from, and when it held", () => {
        expect(describePrice("USD", { asOf: "2026-11-06T16:00:00Z", source: "fawaz", quote: "TWD", quoted: "32.8538370571241164", twd: "32.8538370571241164" }, "TWD"))
            .toBe("1 USD ≈ NT$32.85 · 匯率 2026-11-07");
        expect(describePrice("範例電子", { asOf: "2026-11-06T05:30:00Z", source: "twse", quote: "TWD", quoted: "50.5", twd: "50.5" }, "TWD"))
            .toBe("1 範例電子 ≈ NT$50.50 · 證交所收盤 2026-11-06 13:30");
    });

    it("goes through the quote unit, or says it has no price in NT$", () => {
        expect(describePrice("BGB", { asOf: "2026-11-09T06:00:00Z", source: "bitget", quote: "USDT", quoted: "0.85", twd: "27.9325" }, "TWD"))
            .toBe("1 BGB = 0.85 USDT ≈ NT$27.93 · Bitget 2026-11-09 14:00");
        expect(describePrice("哩程", { asOf: "2026-11-09T06:00:00Z", source: "owner", quote: "EUR", quoted: "0.01" }, "TWD"))
            .toBe("1 哩程 = 0.01 EUR（EUR 沒有新臺幣價格） · 自訂 2026-11-09 14:00");
    });
});

describe("shownNumber", () => {
    it("shows the owner's full number, else the source's, even where the source prints none", () => {
        expect(shownNumber({ number: "1234567***890", fullNumber: "1234567000890" })).toBe("1234567000890");
        expect(shownNumber({ number: "1234567***890" })).toBe("1234567***890");
        expect(shownNumber({ number: "", fullNumber: "MB-1234" })).toBe("MB-1234");
        expect(shownNumber({ number: "" })).toBe("");
    });
});

describe("accountStatus", () => {
    const at = (kind: Account["kind"], asOf: string, rest: Partial<Account> = {}): Account => ({
        ...account(1),
        kind,
        balances: [{ unit: "TWD", asOf: "2026-08-31", amount: "1" }, { unit: "USD", asOf, amount: "1" }],
        ...rest,
    });

    it("counts the days since the newest balance", () => {
        expect(accountStatus(at("deposit", "2026-10-31"), "2026-11-10")).toMatchObject({ asOf: "2026-10-31", days: 10, stale: false });
        expect(accountStatus({ ...account(1), balances: [] }, "2026-11-10")).toEqual({ stale: false, gaps: 0, checked: true });
    });

    it("calls a statement that has not come in 50 days stale, and an exchange not read for a day", () => {
        expect(accountStatus(at("deposit", "2026-09-30"), "2026-11-19").stale).toBe(false);
        expect(accountStatus(at("deposit", "2026-09-30"), "2026-11-20").stale).toBe(true);
        expect(accountStatus(at("points", "2026-09-30"), "2026-11-20").stale).toBe(true);
        expect(accountStatus(at("securities", "2026-08-04"), "2026-11-10").stale).toBe(true);
        expect(accountStatus(at("exchange", "2026-11-09"), "2026-11-10").stale).toBe(false);
        expect(accountStatus(at("exchange", "2026-11-08"), "2026-11-10").stale).toBe(true);
    });

    it("never calls an account the owner keeps stale", () => {
        expect(accountStatus(at("deposit", "2026-01-01", { institution: "owner" }), "2026-11-10")).toMatchObject({ stale: false, note: "手動" });
    });

    it("counts the gaps, and knows when there is nothing to check", () => {
        const gaps = [{ from: "2026-01-31", to: "2026-03-31", unit: "TWD", unseen: "5" }];
        expect(accountStatus(at("deposit", "2026-11-01", { gaps }), "2026-11-10")).toMatchObject({ gaps: 1, checked: true });
        expect(accountStatus(at("exchange", "2026-11-10", { continuity: "unknown" }), "2026-11-10").checked).toBe(false);
    });

    it("says how long ago in words", () => {
        expect([daysAgo(0), daysAgo(1), daysAgo(41)]).toEqual(["今天", "昨天", "41 天前"]);
    });
});

describe("accounts URL", () => {
    it("reads back as the view it links to", () => {
        for (const view of [{ sort: "bank", all: true }, { sort: "balance", all: false }, { sort: "balance", all: true }] as const) {
            expect(readAccountsView(new URL(accountsHref(view), "https://finchie.test").searchParams)).toEqual(view);
        }
        expect(accountsHref({ sort: "balance", all: false })).toBe("/accounts");
    });
});

describe("accountPage", () => {
    it("shows each kind its own parts", () => {
        expect(accountPage({ kind: "deposit", institution: "esun" })).toMatchObject({ headline: "balances", lines: { trade: false }, history: "statements", form: false });
        expect(accountPage({ kind: "securities", institution: "skis" })).toMatchObject({ headline: "worth", positions: true, lines: { trade: true } });
        expect(accountPage({ kind: "exchange", institution: "max" })).toMatchObject({ headline: "worth", holdings: true, lines: undefined, history: "days" });
        expect(accountPage({ kind: "points", institution: "esun" })).toMatchObject({ expiring: true, gaps: expect.stringContaining("過期") });
    });

    it("gives an account the owner keeps a form and no lines", () => {
        expect(accountPage({ kind: "points", institution: "owner" })).toMatchObject({ expiring: true, lines: undefined, form: true });
    });
});

describe("shownPositions", () => {
    const position = (unit: string, shares: string, realized?: string) => ({ unit, shares, realized }) as Position;

    it("lists what is held, then what was sold out with a known gain or loss", () => {
        const shown = shownPositions([position("A", "0", "120"), position("B", "100"), position("C", "0"), position("D", "0", "0")]);
        expect(shown.map(p => p.unit)).toEqual(["B", "A"]);
        expect(shownPositions(undefined)).toEqual([]);
    });
});
