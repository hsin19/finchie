import type { Account } from "#lib/api/client.js";
import {
    describe,
    expect,
    it,
} from "vitest";
import {
    accountSort,
    sortAccounts,
    totalWorth,
} from "./accounts.js";

function account(id: number, worth?: string): Account {
    return { id, institution: "esun", kind: "deposit", number: String(id), balances: [], continuity: "checked", gaps: [], worth };
}

describe("accounts", () => {
    const accounts = [account(1, "500"), account(2), account(3, "15124.5"), account(4, "-300"), account(5, "500")];

    it("keeps the bank order unless asked to sort by balance", () => {
        expect(accountSort(null)).toBe("bank");
        expect(accountSort("nonsense")).toBe("bank");
        expect(sortAccounts(accounts, "bank").map(a => a.id)).toEqual([1, 2, 3, 4, 5]);
    });

    it("sorts by worth, largest first, an account without a worth last and ties in bank order", () => {
        expect(sortAccounts(accounts, accountSort("balance")).map(a => a.id)).toEqual([3, 1, 5, 4, 2]);
    });

    it("adds up worths exactly and counts the accounts without one", () => {
        expect(totalWorth(accounts)).toEqual({ total: "15824.5", missing: 1 });
    });
});
