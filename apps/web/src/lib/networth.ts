// What the owner has less what they owe (ADR 0022), on a day and over time (ADR 0023): worked out by the
// backend; this is how the accounts page shows it.

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
import { approx } from "#lib/money.js";

/**
 * Whether the figure is exact: every account had reported by its day. Otherwise it is an estimate past
 * `completeAsOf`, and `behind` are the accounts that have not reported since, in the order listed.
 */
export function exactness(netWorth: Pick<NetWorth, "on" | "completeAsOf" | "behind">, accounts: readonly Account[]): { exact: boolean; asOf?: string; behind: Account[]; } {
    const exact = netWorth.completeAsOf !== undefined && netWorth.completeAsOf >= netWorth.on;
    const behind = exact ? [] : accounts.filter(a => netWorth.behind.includes(a.id));
    return { exact, asOf: netWorth.completeAsOf, behind };
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
