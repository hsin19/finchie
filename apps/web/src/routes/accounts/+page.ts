import { readAccountsView } from "#lib/accounts.js";
import {
    api,
    attempt,
    LEDGER,
    need,
} from "#lib/api/client.js";
import { today } from "#lib/dates.js";
import { trendQuery } from "#lib/networth.js";
import type { PageLoad } from "./$types";

export const load = (async ({ fetch, depends, url }) => {
    depends(LEDGER);
    const client = api(fetch);
    const [accounts, netWorth, history] = await Promise.all([
        client.GET("/api/accounts"),
        client.GET("/api/net-worth"),
        client.GET("/api/net-worth/history", { params: { query: trendQuery(today()) } }),
    ]);
    // The page is of use without the trend.
    const trend = attempt(history);
    return {
        accounts: need(accounts),
        // Assets less liabilities (ADR 0022), and how exact that is (ADR 0023): the backend's to work out.
        netWorth: need(netWorth),
        history: trend.data,
        historyError: trend.error,
        // ?sort=bank, and ?all: accounts and balances under NT$1,000 are left out unless asked for.
        ...readAccountsView(url.searchParams),
    };
}) satisfies PageLoad;
