import {
    api,
    need,
} from "#lib/api/client.js";
import { monthOf } from "#lib/dates.js";
import { error } from "@sveltejs/kit";
import type { PageLoad } from "./$types";

// No parent(): the layout reloads while a re-parse runs, and this page waits for it to finish before
// reloading the statement, which may be gone by then (a duplicate, or a failure).
export const load = (async ({ fetch, params }) => {
    const id = Number(params.id);
    if (!Number.isSafeInteger(id) || id <= 0) error(404, `statement ${params.id} not found`);

    const client = api(fetch);
    const statement = need(await client.GET("/api/statements/{id}", { params: { path: { id } } }));
    const [transactions, document, accounts] = await Promise.all([
        client.GET("/api/statements/{id}/transactions", { params: { path: { id } } }),
        client.GET("/api/documents/{id}", { params: { path: { id: statement.documentId } } }),
        client.GET("/api/accounts"),
    ]);
    // The points a card bill reports have an account of their own (ADR 0019): its balance after this
    // bill, even when no points moved in the period, and what will lapse.
    const pointsAccounts = need(accounts).filter(a => a.kind === "points" && a.institution === statement.institution);
    const points = await Promise.all(pointsAccounts.map(async a => need(await client.GET("/api/accounts/{id}", { params: { path: { id: a.id } } }))));
    return {
        statement,
        transactions: need(transactions),
        points,
        document: need(document),
        command: `statements show ${statement.institution}/${monthOf(statement.closingDate)}`,
    };
}) satisfies PageLoad;
