import {
    api,
    need,
} from "#lib/api/client.js";
import {
    documentQuery,
    readDocumentFilters,
} from "#lib/documents.js";
import type { PageLoad } from "./$types";

// The first page; the page itself loads the rest as the list scrolls.
export const load = (async ({ fetch, url }) => {
    const filters = readDocumentFilters(url.searchParams);
    const first = need(await api(fetch).GET("/api/documents", { params: { query: documentQuery(filters) } }));
    return { filters, first };
}) satisfies PageLoad;
