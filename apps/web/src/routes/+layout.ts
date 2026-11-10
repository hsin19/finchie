import {
    api,
    LEDGER,
    need,
    type Settings,
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

/** As many as the API gives in one response. */
const WINDOW = 500;

// The deployment's settings do not change while the app is open: loaded once, not with the ledger.
let settings: Promise<Settings> | undefined;

export const load = (async ({ fetch, depends }) => {
    depends(LEDGER);
    const client = api(fetch);
    settings ??= client.GET("/api/settings").then(need);
    const [statements, documents, pending, newest, deployment] = await Promise.all([
        client.GET("/api/statements", { params: { query: { limit: WINDOW } } }),
        client.GET("/api/documents", { params: { query: { limit: WINDOW } } }),
        client.GET("/api/documents", { params: { query: { state: ["pending"], limit: WINDOW } } }),
        client.GET("/api/transactions", { params: { query: { limit: 1 } } }),
        settings.catch(error => {
            settings = undefined;
            throw error;
        }),
    ]);

    const newestDate = need(newest).items[0]?.transactionDate;
    // Pages default to the month of the newest transaction: the latest month with data.
    const latestMonth: Month = newestDate ? monthOf(newestDate) : thisMonth();
    const statementList = need(statements);
    // A statement covers about a month before its closing date, sometimes two.
    const oldestClosing = statementList.at(-1)?.closingDate;
    const oldest = oldestClosing ? shiftMonth(monthOf(oldestClosing), -2) : latestMonth;
    const newestMonth = latestMonth > thisMonth() ? latestMonth : thisMonth();

    const docPage = need(documents);
    return {
        settings: deployment,
        // The newest card statements, newest first: a window, not all of them, though years of them
        // fit. What adds them up (a month's bill, the year so far, what is unpaid) is recent; the
        // statement list (/statements?all) and the month picker say how many are shown.
        statements: statementList,
        // The newest documents: a window, for what pages show beside the ledger. /documents pages
        // through them all; a page that shows one document loads it itself.
        documents: docPage.items,
        // Every pending document, however old: what the poll follows (+layout.svelte).
        pending: need(pending).items,
        // Counts over every document, not the window.
        documentsByState: docPage.byState,
        latestMonth,
        months: monthsBetween(oldest, newestMonth),
    };
}) satisfies LayoutLoad;
