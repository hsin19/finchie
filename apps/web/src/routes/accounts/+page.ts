import { readAccountsView } from "#lib/accounts.js";
import {
    api,
    LEDGER,
    need,
} from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load = (async ({ fetch, depends, url }) => {
    depends(LEDGER);
    const client = api(fetch);
    const [accounts, netWorth] = await Promise.all([client.GET("/api/accounts"), client.GET("/api/net-worth")]);
    return {
        accounts: need(accounts),
        // Worth added up, and what the cards still owe: by the backend, the one place those rules are.
        netWorth: need(netWorth),
        // ?sort=bank, and ?all: accounts and balances under NT$1,000 are left out unless asked for.
        ...readAccountsView(url.searchParams),
    };
}) satisfies PageLoad;
