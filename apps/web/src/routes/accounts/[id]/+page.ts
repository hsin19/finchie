import { accountPage } from "#lib/accounts.js";
import {
    api,
    attempt,
    LEDGER,
    need,
} from "#lib/api/client.js";
import { today } from "#lib/dates.js";
import { investmentQuery } from "#lib/investments.js";
import { defer } from "#lib/stream.svelte.js";
import { error } from "@sveltejs/kit";
import type { PageLoad } from "./$types";

export const load = (async ({ fetch, params, depends }) => {
    depends(LEDGER);
    const id = Number(params.id);
    if (!Number.isSafeInteger(id) || id <= 0) error(404, `account ${params.id} not found`);

    const client = api(fetch);
    const content = (async () => {
        const [response, lines] = await Promise.all([
            client.GET("/api/accounts/{id}", { params: { path: { id } } }),
            client.GET("/api/accounts/{id}/lines", { params: { path: { id }, query: { limit: 500 } } }),
        ]);
        const account = need(response);
        // Shares and coins: their worth against what went into them. The page is of use without it.
        const invested = accountPage(account).invested
            ? attempt(await client.GET("/api/investments/history", { params: { query: { ...investmentQuery(today()), account: id } } }))
            : undefined;
        return { account, lines: need(lines), invested: invested?.data, investedError: invested?.error };
    })();
    return { content: await defer(content) };
}) satisfies PageLoad;
