import {
    api,
    need,
} from "#lib/api/client.js";
import {
    documentQuery,
    readDocumentFilters,
} from "#lib/documents.js";
import type { PageLoad } from "./$types";

// The first page; the page itself loads the rest as the list scrolls. Not reloaded with the ledger
// (LEDGER), which would drop the pages loaded since: the page takes each document's state from the
// layout, which has the newest and every pending one.
export const load = (async ({ fetch, url }) => {
    const filters = readDocumentFilters(url.searchParams);
    const first = need(await api(fetch).GET("/api/documents", { params: { query: documentQuery(filters) } }));
    return { filters, first };
}) satisfies PageLoad;
