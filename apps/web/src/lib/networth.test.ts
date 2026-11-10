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
    exactness,
    trendColumns,
    trendQuery,
} from "./networth";

const account = (id: number) => ({ id }) as Account;
const point = (on: string, net: string, complete = true): NetWorthPoint => ({ on, assets: "0", liabilities: "0", net, unvalued: 0, complete });

describe("exactness", () => {
    it("is exact on a day every account had reported by", () => {
        expect(exactness({ on: "2026-10-31", completeAsOf: "2026-11-02", behind: [] }, [])).toEqual({ exact: true, asOf: "2026-11-02", behind: [] });
    });

    it("is an estimate after it, naming the accounts that are behind", () => {
        const accounts = [account(1), account(2), account(3)];
        expect(exactness({ on: "2026-11-11", completeAsOf: "2026-10-31", behind: [3, 1] }, accounts)).toEqual({ exact: false, asOf: "2026-10-31", behind: [accounts[0], accounts[2]] });
        expect(exactness({ on: "2026-11-11", behind: [] }, accounts).exact).toBe(false);
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
