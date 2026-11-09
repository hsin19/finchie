import {
    api,
    LEDGER,
    need,
} from "#lib/api/client.js";
import {
    type Month,
    monthOf,
    monthsBetween,
    shiftMonth,
    thisMonth,
} from "#lib/dates.js";
import type { LayoutLoad } from "./$types";

// A single-page app: nothing renders on a server, the backend serves the shell for every route
// (ADR 0007).
export const ssr = false;

export const load = (async ({ fetch, depends }) => {
    depends(LEDGER);
    const client = api(fetch);
    const [statements, documents, newest] = await Promise.all([
        client.GET("/api/statements", { params: { query: { limit: 500 } } }),
        client.GET("/api/documents", { params: { query: { limit: 500 } } }),
        client.GET("/api/transactions", { params: { query: { limit: 1 } } }),
    ]);

    const newestDate = need(newest).items[0]?.transactionDate;
    // Pages default to the month of the newest transaction: the latest month with data.
    const latestMonth: Month = newestDate ? monthOf(newestDate) : thisMonth();
    const statementList = need(statements);
    // A statement covers about a month before its closing date, sometimes two.
    const oldestClosing = statementList.at(-1)?.closingDate;
    const oldest = oldestClosing ? shiftMonth(monthOf(oldestClosing), -2) : latestMonth;
    const newestMonth = latestMonth > thisMonth() ? latestMonth : thisMonth();

    return {
        statements: statementList,
        // The newest, for what every page shows of them and to see what is still parsing; /documents
        // lists them all.
        documents: need(documents).items,
        latestMonth,
        months: monthsBetween(oldest, newestMonth),
    };
}) satisfies LayoutLoad;
