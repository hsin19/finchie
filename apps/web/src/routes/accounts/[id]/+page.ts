import {
    api,
    LEDGER,
    need,
} from "#lib/api/client.js";
import { defer } from "#lib/stream.svelte.js";
import { error } from "@sveltejs/kit";
import type { PageLoad } from "./$types";

export const load = (async ({ fetch, params, depends }) => {
    depends(LEDGER);
    const id = Number(params.id);
    if (!Number.isSafeInteger(id) || id <= 0) error(404, `account ${params.id} not found`);

    const client = api(fetch);
    const content = Promise.all([
        client.GET("/api/accounts/{id}", { params: { path: { id } } }),
        client.GET("/api/accounts/{id}/lines", { params: { path: { id }, query: { limit: 500 } } }),
    ]).then(([account, lines]) => ({ account: need(account), lines: need(lines) }));
    return { content: await defer(content) };
}) satisfies PageLoad;
