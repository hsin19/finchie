import type { Account } from "#lib/api/client.js";
import {
    sign,
    subtract,
    sum,
} from "#lib/money.js";

export type AccountSort = "bank" | "balance";

export function accountSort(value: string | null): AccountSort {
    return value === "balance" ? "balance" : "bank";
}

/**
 * By bank, as the API lists them; or by worth in NT$, largest first (ADR 0015). An account without a
 * worth, holding a unit with no rate, goes last; ties keep the bank order.
 */
export function sortAccounts(accounts: Account[], by: AccountSort): Account[] {
    if (by === "bank") return accounts;
    return [...accounts].sort((a, b) => {
        if (a.worth === undefined || b.worth === undefined) return a.worth === undefined ? (b.worth === undefined ? 0 : 1) : -1;
        return sign(subtract(b.worth, a.worth));
    });
}

/** The accounts' worth in NT$ added up, and how many had none to add. */
export function totalWorth(accounts: Account[]): { total: string; missing: number; } {
    const worths = accounts.flatMap(a => (a.worth === undefined ? [] : [a.worth]));
    return { total: sum(worths), missing: accounts.length - worths.length };
}
