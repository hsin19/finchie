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
import { defer } from "#lib/stream.svelte.js";
import type { PageLoad } from "./$types";

export const load = (async ({ fetch, depends }) => {
    depends(LEDGER);
    const client = api(fetch);
    const months = cashflowMonths(thisMonth());
    const content = (async () => {
        const [accounts, netWorth, history, cashflow] = await Promise.all([
            client.GET("/api/accounts"),
            client.GET("/api/net-worth"),
            // Last month's end and today: how net worth moved this month.
            client.GET("/api/net-worth/history", { params: { query: trendQuery(today(), 2) } }),
            // The year's totals only (limit 0), as the backend adds them up, split by month.
            client.GET("/api/transactions", {
                params: { query: { from: monthRange(months[0]).from, to: monthRange(months.at(-1)!).to, limit: 0, byMonth: true } },
            }),
        ]);
        return {
            accounts: need(accounts),
            netWorth: need(netWorth),
            // The page is of use without it.
            history: attempt(history).data,
            months,
            cashflow: need(cashflow),
        };
    })();
    return { content: await defer(content) };
}) satisfies PageLoad;
