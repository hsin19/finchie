import { isMonth } from "#lib/dates.js";
import {
    readFilters,
    transactionsHref,
    transactionsQueryHref,
    withChanges,
} from "#lib/filters.js";
import {
    readStatementsView,
    statementsHref,
} from "#lib/urls.js";
import type { ReadonlyURLSearchParams } from "$app/state";
import type { ResolvedPathname } from "$app/types";

/**
 * Where the page links in the header go from the page on screen: they carry its month, in the
 * parameter that means that month on the other page. On /statements ?m= is a closing month; on
 * /transactions it is the month of the transactions, and ?closing= is the closing month.
 */
export function carriedLinks(routeId: string | null, params: ReadonlyURLSearchParams): { statements: ResolvedPathname; transactions: ResolvedPathname; } {
    if (routeId === "/statements") {
        const view = readStatementsView(params);
        // Only a month asked for: without one, /statements shows the newest anyway.
        if (!view.all && view.month) return { statements: statementsHref(view.month), transactions: transactionsHref({ closing: view.month }) };
    }
    if (routeId === "/transactions") {
        const { period, closing } = readFilters(params);
        return { statements: statementsHref(closing), transactions: transactionsHref({ period, closing }) };
    }
    return { statements: statementsHref(), transactions: transactionsHref() };
}

/**
 * "Switch month": on the transactions page it keeps the other filters (from page 1); elsewhere it
 * opens that month's bill.
 */
export function monthHref(routeId: string | null, params: ReadonlyURLSearchParams, month: string): ResolvedPathname {
    if (routeId !== "/transactions") return statementsHref(isMonth(month) ? month : undefined);
    return transactionsQueryHref(withChanges(params, { m: month }));
}

/**
 * Which entity a page shows, by its route parameters: "id=7". SvelteKit keeps the page component when
 * only a parameter changes (/accounts/7 → /accounts/9), so the layout remounts the page when this
 * changes; state such as an edit draft or a re-parse message then belongs to the entity it was made for.
 * A change of the query alone (a filter) keeps the page.
 */
export function entityKey(params: Record<string, string>): string {
    return Object.entries(params).sort(([a], [b]) => a.localeCompare(b)).map(([k, v]) => `${k}=${v}`).join("&");
}
