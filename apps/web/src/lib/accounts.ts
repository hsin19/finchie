import type {
    Account,
    Balance,
    Statement,
} from "#lib/api/client.js";
import {
    formatAmount,
    negate,
    roundWhole,
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

/** Below this worth in NT$, either way, an account or a balance is small: the accounts page leaves it out unless ?all. */
export const SMALL_WORTH = "1000";

/** How many balances an account lists while small ones are hidden. */
export const LISTED_BALANCES = 3;

function small(worth: string): boolean {
    return sign(subtract(sign(worth) < 0 ? negate(worth) : worth, SMALL_WORTH)) < 0;
}

/**
 * An account worth less than NT$1,000 either way; one whose worth is not known is not. Gaps do not keep one
 * listed: the page names every account with gaps above the list.
 */
export function isSmallAccount(account: Account): boolean {
    return account.worth !== undefined && small(account.worth);
}

export interface ListedBalances {
    listed: Balance[];
    /** Balances with a worth left out, and the largest worth among them. */
    others: number;
    othersAtMost?: string;
    /** Balances left out for having no rate. */
    unpriced: number;
}

/**
 * The balances an account lists, largest worth first; one at 0 is none at all. Hiding small ones keeps the
 * three largest known to be worth NT$1,000 or more, and tells what it leaves out: how many others, the
 * most any of them is worth, and how many have no rate. A balance worth nothing once rounded is not
 * counted either.
 */
export function listedBalances(account: Account, hideSmall: boolean): ListedBalances {
    const sorted = holdingsByWorth(account.balances.filter(b => sign(b.amount) !== 0));
    if (!hideSmall) return { listed: sorted, others: 0, unpriced: 0 };
    const priced = sorted.filter(b => b.worth !== undefined);
    const listed = priced.filter(b => !small(b.worth!)).slice(0, LISTED_BALANCES);
    const others = priced.filter(b => !listed.includes(b) && sign(b.worth!) !== 0);
    return { listed, others: others.length, othersAtMost: others[0]?.worth, unpriced: sorted.length - priced.length };
}

/** "另 11 種 ≤ NT$3,193、32 種沒有匯率", or "" when nothing is left out. */
export function leftOut({ others, othersAtMost, unpriced }: ListedBalances): string {
    const parts: string[] = [];
    if (others > 0 && othersAtMost !== undefined) parts.push(`${others} 種 ≤ NT$${formatAmount(roundWhole(othersAtMost))}`);
    if (unpriced > 0) parts.push(`${unpriced} 種沒有匯率`);
    return parts.length > 0 ? `另 ${parts.join("、")}` : "";
}

/** Holdings by worth in NT$, largest first; those without a rate go last, by unit. */
export function holdingsByWorth(balances: Balance[]): Balance[] {
    return [...balances].sort((a, b) => {
        if (a.worth === undefined || b.worth === undefined) {
            if (a.worth !== b.worth) return a.worth === undefined ? 1 : -1;
            return a.unit.localeCompare(b.unit);
        }
        return sign(subtract(b.worth, a.worth)) || a.unit.localeCompare(b.unit);
    });
}

/**
 * A snapshot history by day, newest first, each day's units in the order the API lists them (ADR 0018).
 * A unit at 0 is one sold out that day.
 */
export function historyByDay(history: Balance[]): { asOf: string; balances: Balance[]; }[] {
    const days = new Map<string, Balance[]>();
    for (const b of history) days.set(b.asOf, [...(days.get(b.asOf) ?? []), b]);
    return [...days].sort(([a], [b]) => b.localeCompare(a)).map(([asOf, balances]) => ({ asOf, balances }));
}
