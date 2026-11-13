import {
    api,
    LEDGER,
    need,
    type TransactionQuery,
} from "#lib/api/client.js";
import {
    closingMonths,
    closingRange,
} from "#lib/bills.js";
import {
    shiftMonth,
    thisMonth,
} from "#lib/dates.js";
import { readStatementsView } from "#lib/urls.js";
import type { PageLoad } from "./$types";

// ?all lists every statement; otherwise one month's bill: the statements that closed in it (?m=,
// the newest such month by default) and the transactions on them.
export const load = (async ({ fetch, url, parent, depends }) => {
    depends(LEDGER);
    const { statements } = await parent();
    const view = readStatementsView(url.searchParams);
    if (view.all) return { view: "all" as const };

    const months = closingMonths(statements);
    const month = view.month ?? months[0] ?? thisMonth();

    // Totals only (limit 0), of card lines alone, whose amounts add up as the bills print them. With
    // kind=purchase, `sum` and byCard are spending, while byKind (which ignores the kind filter) still has
    // fees, rebates, and payments.
    const client = api(fetch);
    const purchases = (query: TransactionQuery) => client.GET("/api/transactions", { params: { query: { accountKind: "credit_card", kind: ["purchase"], limit: 0, ...query } } });
    // The points the month's bills report earned (ADR 0019), and the accounts, for each program's price.
    const [current, previous, overseas, points, accounts] = await Promise.all([
        purchases(closingRange(month)),
        purchases(closingRange(shiftMonth(month, -1))),
        purchases({ ...closingRange(month), overseas: true }),
        client.GET("/api/transactions", { params: { query: { accountKind: "points", kind: ["earn", "adjust"], limit: 500, ...closingRange(month) } } }),
        client.GET("/api/accounts"),
    ]);
    return {
        view: "period" as const,
        month,
        closingMonths: months.includes(month) ? months : [month, ...months].sort().reverse(),
        current: need(current),
        previous: need(previous),
        overseas: need(overseas),
        pointLines: need(points).items,
        accounts: need(accounts),
    };
}) satisfies PageLoad;
