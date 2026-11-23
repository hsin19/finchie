import type {
    Account,
    NetWorthPoint,
} from "#lib/api/client.js";
import {
    describe,
    expect,
    it,
} from "vitest";
import {
    behindNote,
    netWorthChange,
    overdueBehind,
    trendColumns,
    trendQuery,
} from "./networth";

/** A deposit account whose newest balance is of `asOf`, overdue or not by the backend's rule. */
const reported = (id: number, asOf: string, overdue = false): Account => ({
    id,
    institution: "esun",
    kind: "deposit",
    side: "asset",
    number: String(id),
    continuity: "checked",
    gaps: [],
    expiries: [],
    balances: [{ unit: "TWD", asOf, amount: "1" }],
    overdue,
    excused: false,
    freshFor: 50,
});
const point = (on: string, net: string, complete = true): NetWorthPoint => ({ on, assets: "0", liabilities: "0", net, unvalued: 0, complete, behind: [] });

describe("overdueBehind", () => {
    it("names only the accounts behind the figure that are overdue", () => {
        // A statement that closed last month is behind today as a matter of course; one 60 days old is overdue.
        const accounts = [reported(1, "2026-09-12", true), reported(2, "2026-10-31"), reported(3, "2026-10-31")];
        expect(overdueBehind({ behind: [3, 1] }, accounts, "2026-11-11")).toEqual([accounts[0]]);
        expect(overdueBehind({ behind: [2, 3] }, accounts, "2026-11-11")).toEqual([]);
    });

    it("names none that is not behind, however old", () => {
        expect(overdueBehind({ behind: [] }, [reported(1, "2026-09-12", true)], "2026-11-11")).toEqual([]);
    });
});

describe("behindNote", () => {
    const accounts = [1, 2, 3, 4].map(id => reported(id, "2026-10-31"));

    it("names the accounts when there are at most three", () => {
        expect(behindNote([1], accounts)).toBe("估計：玉山 存款 還沒回報到這天");
        expect(behindNote([1, 2, 3], accounts)).toBe("估計：玉山 存款、玉山 存款、玉山 存款 還沒回報到這天");
    });

    it("gives the count when there are more than three", () => {
        expect(behindNote([1, 2, 3, 4], accounts)).toBe("估計：還有 4 個帳戶沒回報到這天");
    });

    it("falls back to a generic note when none are given", () => {
        expect(behindNote([], accounts)).toBe("估計：還有帳戶沒回報到這天");
    });
});

describe("trendQuery", () => {
    it("asks for the month ends of the last year, and today", () => {
        expect(trendQuery("2026-11-11")).toEqual({ from: "2025-12-01", to: "2026-11-11", step: "month" });
    });
});

describe("trendColumns", () => {
    it("stands columns on a zero line, up for a positive net and down for a negative one", () => {
        const { columns, zero } = trendColumns([point("2026-09-30", "300"), point("2026-10-31", "-100", false)]);
        expect(zero).toBe(25);
        expect(columns.map(c => [c.bottom, c.height, c.negative])).toEqual([[25, 75, false], [0, 25, true]]);
        expect(columns[1].point.complete).toBe(false);
    });

    it("keeps a flat line at the bottom when everything is zero", () => {
        expect(trendColumns([point("2026-10-31", "0")])).toEqual({ columns: [{ point: point("2026-10-31", "0"), bottom: 0, height: 0, negative: false }], zero: 0 });
    });
});

describe("netWorthChange", () => {
    it("is the last point less the one before", () => {
        expect(netWorthChange([point("2026-09-30", "900"), point("2026-10-31", "1000"), point("2026-11-13", "1250.50")])).toBe("250.50");
    });

    it("is none when the two valued a different number of accounts, or there are not two", () => {
        const then = { ...point("2026-10-31", "1000"), unvalued: 9 };
        expect(netWorthChange([then, { ...point("2026-11-13", "2000"), unvalued: 4 }])).toBeUndefined();
        expect(netWorthChange([point("2026-11-13", "2000")])).toBeUndefined();
    });
});
