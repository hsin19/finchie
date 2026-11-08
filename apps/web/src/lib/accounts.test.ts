import type {
    Account,
    Statement,
} from "#lib/api/client.js";
import {
    describe,
    expect,
    it,
} from "vitest";
import {
    accountSort,
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
