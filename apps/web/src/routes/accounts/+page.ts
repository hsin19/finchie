import { accountSort } from "#lib/accounts.js";
import {
    api,
    LEDGER,
    need,
} from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load = (async ({ fetch, depends, url }) => {
    depends(LEDGER);
    return { accounts: need(await api(fetch).GET("/api/accounts")), sort: accountSort(url.searchParams.get("sort")) };
}) satisfies PageLoad;
