import {
    api,
    attempt,
    LEDGER,
    need,
} from "#lib/api/client.js";
import { cashflowMonths } from "#lib/cashflow.js";
import {
    monthRange,
    thisMonth,
    today,
} from "#lib/dates.js";
import { trendQuery } from "#lib/networth.js";
import type { PageLoad } from "./$types";

export const load = (async ({ fetch, depends }) => {
    depends(LEDGER);
    const client = api(fetch);
    const months = cashflowMonths(thisMonth());
    const [accounts, netWorth, history, ...searches] = await Promise.all([
        client.GET("/api/accounts"),
        client.GET("/api/net-worth"),
        // Last month's end and today: how net worth moved this month.
        client.GET("/api/net-worth/history", { params: { query: trendQuery(today(), 2) } }),
        // A month's totals only (limit 0), as the backend adds them up.
        ...months.map(m => client.GET("/api/transactions", { params: { query: { ...monthRange(m), limit: 0 } } })),
    ]);
    return {
        accounts: need(accounts),
        netWorth: need(netWorth),
        // The page is of use without it.
        history: attempt(history).data,
        months,
        searches: searches.map(need),
    };
}) satisfies PageLoad;
