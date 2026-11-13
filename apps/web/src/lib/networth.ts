// What the owner has less what they owe (ADR 0022), on a day and over time (ADR 0023): worked out by the
// backend; this is how the accounts page shows it.

import { accountStatus } from "#lib/accounts.js";
import type {
    Account,
    NetWorth,
    NetWorthPoint,
} from "#lib/api/client.js";
import {
    type Month,
    monthOf,
    shiftMonth,
} from "#lib/dates.js";
import {
    approx,
    subtract,
} from "#lib/money.js";

/**
 * The accounts the figure takes an old balance of because they are overdue as of `today` (accountStatus),
 * in the order listed: what is worth saying about it. A figure past the day every account had reported
 * is an estimate (ADR 0023), but that is every day: a bank's statement comes once a month, so nearly every
 * account is behind as a matter of course, and saying so, or naming them, says nothing.
 */
export function overdueBehind(netWorth: Pick<NetWorth, "behind">, accounts: readonly Account[], today: string): Account[] {
    return accounts.filter(a => netWorth.behind.includes(a.id) && accountStatus(a, today).stale);
}

/** The trend's query: each month end of the last year, and `today`. */
export function trendQuery(today: string, months = 12): { from: string; to: string; step: "month"; } {
    const first: Month = shiftMonth(monthOf(today), -(months - 1));
    return { from: `${first}-01`, to: today, step: "month" };
}

export interface TrendColumn {
    point: NetWorthPoint;
    /** Where the column starts and how tall it is, in percent of the chart, from the bottom. */
    bottom: number;
    height: number;
    negative: boolean;
}

/**
 * Columns of net worth around a zero line: up for what is owned beyond what is owed, down for the
 * opposite. Proportions only (approx); the amounts shown come from the points themselves.
 */
export function trendColumns(points: readonly NetWorthPoint[]): { columns: TrendColumn[]; zero: number; } {
    const nets = points.map(p => approx(p.net));
    const top = Math.max(0, ...nets);
    const floor = Math.min(0, ...nets);
    const span = top - floor || 1;
    const zero = ((0 - floor) / span) * 100;
    const columns = points.map((point, i) => ({
        point,
        bottom: ((Math.min(0, nets[i]) - floor) / span) * 100,
        height: (Math.abs(nets[i]) / span) * 100,
        negative: nets[i] < 0,
    }));
    return { columns, zero };
}

/**
 * How net worth moved from the point before the last to the last; none when the two left out a different
 * number of accounts for want of a price (`unvalued`): one that had no worth then and has one now would
 * read as money made.
 */
export function netWorthChange(points: readonly NetWorthPoint[]): string | undefined {
    const [before, after] = points.slice(-2);
    if (!before || !after || before.unvalued !== after.unvalued) return undefined;
    return subtract(after.net, before.net);
}
