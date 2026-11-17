import { readAccountsView } from "#lib/accounts.js";
import {
    api,
    attempt,
    LEDGER,
    need,
} from "#lib/api/client.js";
import { today } from "#lib/dates.js";
import { investmentQuery } from "#lib/investments.js";
import { trendQuery } from "#lib/networth.js";
import { defer } from "#lib/stream.svelte.js";
import type { PageLoad } from "./$types";

export const load = (async ({ fetch, depends, url }) => {
    depends(LEDGER);
    const client = api(fetch);
    // Read before the load returns: SvelteKit only sees what the load reads until then.
    const view = readAccountsView(url.searchParams);
    const content = (async () => {
        const [accounts, netWorth, history, investments] = await Promise.all([
            client.GET("/api/accounts"),
            client.GET("/api/net-worth"),
            client.GET("/api/net-worth/history", { params: { query: trendQuery(today()) } }),
            client.GET("/api/investments/history", { params: { query: investmentQuery(today()) } }),
        ]);
        // The page is of use without the trends.
        const trend = attempt(history);
        const invested = attempt(investments);
        return {
            accounts: need(accounts),
            // Assets less liabilities (ADR 0022), and how exact that is (ADR 0023): the backend's to work out.
            netWorth: need(netWorth),
            history: trend.data,
            historyError: trend.error,
            investments: invested.data,
            investmentsError: invested.error,
            // ?sort=bank, and ?all: accounts and balances under NT$1,000 are left out unless asked for.
            ...view,
        };
    })();
    return { content: await defer(content) };
}) satisfies PageLoad;
