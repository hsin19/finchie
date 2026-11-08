import {
    api,
    LEDGER,
    need,
} from "#lib/api/client.js";
import { institution } from "#lib/labels.js";
import { error } from "@sveltejs/kit";
import type { PageLoad } from "./$types";

export const load = (async ({ fetch, params, depends }) => {
    depends(LEDGER);
    const id = Number(params.id);
    if (!Number.isSafeInteger(id) || id <= 0) error(404, `account ${params.id} not found`);

    const client = api(fetch);
    const [account, lines] = await Promise.all([
        client.GET("/api/accounts/{id}", { params: { path: { id } } }),
        client.GET("/api/accounts/{id}/lines", { params: { path: { id }, query: { limit: 500 } } }),
    ]);
    const found = need(account);
    return {
        account: found,
        lines: need(lines),
        command: `accounts show ${institution(found.institution).code.toLowerCase()}/${found.number || found.kind}`,
    };
}) satisfies PageLoad;
