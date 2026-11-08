import type {
    Account,
    Statement,
} from "#lib/api/client.js";
import {
    sign,
    subtract,
    sum,
} from "#lib/money.js";

export type AccountSort = "bank" | "balance";

/** By worth unless the URL asks for the bank order. */
export function accountSort(value: string | null): AccountSort {
    return value === "bank" ? "bank" : "balance";
}

/**
 * By worth in NT$, largest first (ADR 0015); or by bank, as the API lists them. An account without a
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

/**
 * What the owner still owes on cards: each bank's newest bill that owes something and no deposit
 * account or later bill has shown paid (ADR 0016). An older unpaid bill is the newer one's past.
 */
export function unpaidBills(statements: Statement[]): Statement[] {
    const newest = new Map<string, Statement>();
    for (const s of statements) {
        const seen = newest.get(s.institution);
        if (!seen || s.closingDate > seen.closingDate) newest.set(s.institution, s);
    }
    return [...newest.values()].filter(s => !s.paidOn && sign(s.totalDue) > 0);
}
