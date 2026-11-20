// What came in and what went out each month across every account (ADR 0013), as the backend adds it up
// (`flows`): money that only moved between the owner's own accounts, a card bill's payment, money sent to
// an exchange, trades, and points are neither (ADR 0016, ADR 0017, ADR 0018, ADR 0019).

import {
    accountStatus,
    isOwned,
} from "#lib/accounts.js";
import type {
    Account,
    MonthFlows,
    UnitFlow,
} from "#lib/api/client.js";
import {
    type Month,
    monthRange,
    shiftMonth,
} from "#lib/dates.js";
import {
    sign,
    sum,
} from "#lib/money.js";

/** The months the overview shows, oldest first: the last `count`, this one included. */
export function cashflowMonths(now: Month, count = 12): Month[] {
    return Array.from({ length: count }, (_, i) => shiftMonth(now, i - count + 1));
}

/**
 * The day up to which every source of what is spent and received has reported: the oldest newest
 * balance of the deposit accounts and cards, which a statement prints on its closing day. One that is
 * overdue (accountStatus) is left out, or a single quiet account would hold every month back; so is one
 * the owner said not to look at again yet, and one they keep.
 */
export function reportedThrough(accounts: readonly Account[], today: string): string | undefined {
    return accounts
        .filter(a => (a.kind === "deposit" || a.kind === "credit_card") && !isOwned(a))
        .map(a => accountStatus(a, today))
        .filter(status => !status.stale && !status.excused && status.asOf !== undefined)
        .map(status => status.asOf!)
        .sort()[0];
}

export interface MonthFlow {
    month: Month;
    /** In the base currency: what came in, positive, and what went out, negative or zero. */
    received: string;
    spent: string;
    net: string;
    /** Every source has reported past the month's end: its figures will not change. */
    complete: boolean;
}

/** Each month's figures in the base currency, from a search split by month (`byMonth`); a month it has no line in is 0. */
export function monthFlows(months: readonly Month[], byMonth: readonly MonthFlows[], base: string, through?: string): MonthFlow[] {
    return months.map(month => {
        const flow = byMonth.find(m => m.month === month)?.flows.find(f => f.unit === base);
        const received = flow?.received ?? "0";
        const spent = flow?.spent ?? "0";
        return { month, received, spent, net: sum([received, spent]), complete: through !== undefined && monthRange(month).to <= through };
    });
}

/** What the other units came to over the whole search, each that moved at all: "USD". */
export function otherUnits(flows: readonly UnitFlow[], base: string): { unit: string; received: string; spent: string; }[] {
    return flows
        .filter(f => f.unit !== base && (sign(f.received) !== 0 || sign(f.spent) !== 0))
        .map(({ unit, received, spent }) => ({ unit, received, spent }));
}

/** A year's months so far, added up. */
export function yearFlow(flows: readonly MonthFlow[], year: string): { received: string; spent: string; net: string; months: number; } {
    const of = flows.filter(f => f.month.startsWith(`${year}-`));
    const received = sum(of.map(f => f.received));
    const spent = sum(of.map(f => f.spent));
    return { received, spent, net: sum([received, spent]), months: of.length };
}
