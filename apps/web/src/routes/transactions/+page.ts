import {
    api,
    LEDGER,
    need,
    type TransactionQuery,
    TRANSACTIONS_PAGE_SIZE as PAGE_SIZE,
} from "#lib/api/client.js";
import { closingRange } from "#lib/bills.js";
import { monthRange } from "#lib/dates.js";
import {
    pageCount,
    readFilters,
    searchPeriod,
    transactionsQueryHref,
    withChanges,
} from "#lib/filters.js";
import { redirect } from "@sveltejs/kit";
import type { PageLoad } from "./$types";

export const load = (async ({ fetch, url, parent, depends }) => {
    depends(LEDGER);
    const { latestMonth } = await parent();
    const filters = readFilters(url.searchParams);
    const period = searchPeriod(filters, latestMonth);
    const { kinds, institution, card, account, q, overseas, hideTransfers, sort, closing } = filters;

    const query: TransactionQuery = {
        ...(period === "all" ? {} : monthRange(period)),
        ...(closing ? closingRange(closing) : {}),
        kind: kinds.length > 0 ? kinds : undefined,
        institution: institution || undefined,
        card: card || undefined,
        account: account ? Number(account) : undefined,
        q: q || undefined,
        overseas: overseas || undefined,
        hideTransfers: hideTransfers || undefined,
        sort: sort === "newest" ? undefined : sort,
        limit: PAGE_SIZE,
        offset: (filters.page - 1) * PAGE_SIZE,
    };
    const result = need(await api(fetch).GET("/api/transactions", { params: { query } }));

    // A page past the end, from an old link or a page change made while a filter was loading.
    const pages = pageCount(result.total);
    if (filters.page > pages) {
        const clamped = withChanges(url.searchParams, { page: pages === 1 ? null : String(pages) }).toString();
        redirect(307, transactionsQueryHref(clamped));
    }
    return { filters, result };
}) satisfies PageLoad;
