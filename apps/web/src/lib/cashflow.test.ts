import type {
    Account,
    UnitFlow,
} from "#lib/api/client.js";
import {
    describe,
    expect,
    it,
} from "vitest";
import {
    cashflowMonths,
    monthFlows,
    otherUnits,
    reportedThrough,
    yearFlow,
} from "./cashflow";

const account = (id: number, kind: Account["kind"], asOf: string, institution = "esun"): Account => ({
    id,
    institution,
    kind,
    side: kind === "credit_card" ? "liability" : "asset",
    number: String(id),
    continuity: "checked",
    gaps: [],
    expiries: [],
    balances: [{ unit: "TWD", asOf, amount: "1" }],
});
const flow = (unit: string, received: string, spent: string): UnitFlow => ({ unit, received, spent, moved: "0", movedCount: 0 });

describe("cashflowMonths", () => {
    it("is the last twelve months, oldest first, this one last", () => {
        const months = cashflowMonths("2026-11");
        expect(months).toHaveLength(12);
        expect([months[0], months.at(-1)]).toEqual(["2025-12", "2026-11"]);
    });
});

describe("reportedThrough", () => {
    it("is the oldest newest balance of the deposit accounts and cards", () => {
        const accounts = [
            account(1, "deposit", "2026-09-30"),
            account(2, "credit_card", "2026-11-01"),
            account(3, "deposit", "2026-10-12"),
            // Neither spends nor receives: whatever they last reported.
            account(4, "securities", "2026-08-31"),
            account(5, "exchange", "2026-01-01"),
        ];
        expect(reportedThrough(accounts, "2026-11-13")).toBe("2026-09-30");
    });

    it("waits for no account that is overdue, nor one the owner keeps", () => {
        const accounts = [account(1, "deposit", "2026-10-31"), account(2, "deposit", "2026-08-01"), account(3, "deposit", "2026-01-01", "owner")];
        expect(reportedThrough(accounts, "2026-11-13")).toBe("2026-10-31");
        expect(reportedThrough([], "2026-11-13")).toBeUndefined();
    });
});

describe("monthFlows", () => {
    it("takes each month's base currency, complete once every source reported past its end", () => {
        const flows = monthFlows(["2026-09", "2026-10"], [{ flows: [flow("TWD", "80766", "-95707"), flow("USD", "3.89", "0")] }, { flows: [] }], "TWD", "2026-09-30");
        expect(flows).toEqual([
            { month: "2026-09", received: "80766", spent: "-95707", net: "-14941", complete: true },
            { month: "2026-10", received: "0", spent: "0", net: "0", complete: false },
        ]);
    });
});

describe("otherUnits", () => {
    it("adds up every unit but the base one that moved at all", () => {
        const searches = [{ flows: [flow("TWD", "1", "-1"), flow("USD", "5.80", "-9.00"), flow("JPY", "0.00", "0.00")] }, { flows: [flow("USD", "3.89", "0")] }];
        expect(otherUnits(searches, "TWD")).toEqual([{ unit: "USD", received: "9.69", spent: "-9.00" }]);
    });
});

describe("yearFlow", () => {
    it("adds up the year's months only", () => {
        const months = monthFlows(["2025-12", "2026-01", "2026-02"], [
            { flows: [flow("TWD", "92071", "-76753")] },
            { flows: [flow("TWD", "81399", "-36170")] },
            { flows: [flow("TWD", "336902", "-60829")] },
        ], "TWD");
        expect(yearFlow(months, "2026")).toEqual({ received: "418301", spent: "-96999", net: "321302", months: 2 });
    });
});
