// The securities and exchange accounts' worth against what went into them: worked out by the backend
// (`/api/investments/history`); this is how the accounts page draws it.

import type {
    InvestmentHistoryQuery,
    InvestmentPoint,
} from "#lib/api/client.js";
import { addDays } from "#lib/dates.js";
import { approx } from "#lib/money.js";

/**
 * The query: every day up to `today`, as many as the API gives (1000). Prices go back only to when Finchie
 * started, so the series starts on the first day the investments have a worth, and a securities account
 * counts its worth that day as put in: a shorter span would move that day along with it. By month ends it
 * would be a point or two for months.
 */
export function investmentQuery(today: string, days = 1000): Required<Pick<InvestmentHistoryQuery, "from" | "to" | "step">> {
    return { from: addDays(today, -(days - 1)), to: today, step: "day" };
}

export interface InvestmentLines {
    /** SVG polyline points in a 100 × 100 box, y down. */
    worth: string;
    invested: string;
}

/**
 * Worth and what went in, as two lines on one scale. Proportions only (approx); the amounts shown come
 * from the points themselves. A little room above and below, so neither line runs along an edge.
 */
export function investmentLines(points: readonly InvestmentPoint[]): InvestmentLines {
    const worth = points.map(p => approx(p.worth));
    const invested = points.map(p => approx(p.invested));
    const all = [...worth, ...invested];
    const low = Math.min(...all);
    const high = Math.max(...all);
    const pad = (high - low) * 0.1 || Math.abs(high) * 0.1 || 1;
    const span = high - low + 2 * pad;
    const y = (v: number) => 100 - ((v - low + pad) / span) * 100;
    const line = (values: number[]) => values.map((v, i) => `${round(investmentX(i, points.length))},${round(y(v))}`).join(" ");
    return { worth: line(worth), invested: line(invested) };
}

/** Where the `i`th of `n` days is across the chart, in percent: the first at the left edge, the last at the right. */
export function investmentX(i: number, n: number): number {
    return n === 1 ? 50 : (i / (n - 1)) * 100;
}

function round(n: number): number {
    return Math.round(n * 100) / 100;
}
