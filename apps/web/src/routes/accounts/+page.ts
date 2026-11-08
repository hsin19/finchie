import {
    api,
    LEDGER,
    need,
} from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load = (async ({ fetch, depends }) => {
    depends(LEDGER);
    return { accounts: need(await api(fetch).GET("/api/accounts")) };
}) satisfies PageLoad;
