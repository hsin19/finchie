import {
    api,
    LEDGER,
    need,
} from "#lib/api/client.js";
import type { PageLoad } from "./$types";

/** The newest of them, resolved ones included: enough to see what was told and what became of it. */
const RECENT = 200;

export const load = (async ({ fetch, depends }) => {
    depends(LEDGER);
    const list = need(await api(fetch).GET("/api/notifications", { params: { query: { limit: RECENT } } }));
    return { all: list.items };
}) satisfies PageLoad;
