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
    const [transactions, document] = await Promise.all([
        client.GET("/api/statements/{id}/transactions", { params: { path: { id } } }),
        client.GET("/api/documents/{id}", { params: { path: { id: statement.documentId } } }),
    ]);
    const lines = need(transactions);
    // The points a card bill reports have an account of their own (ADR 0019): its balance after this
    // bill, and what will lapse.
    const pointsAccounts = [...new Set(lines.filter(t => t.accountKind === "points" && t.accountId).map(t => t.accountId as number))];
    const points = await Promise.all(pointsAccounts.map(async id => need(await client.GET("/api/accounts/{id}", { params: { path: { id } } }))));
    return {
        statement,
        transactions: lines,
        points,
        document: need(document),
        command: `statements show ${statement.institution}/${monthOf(statement.closingDate)}`,
    };
}) satisfies PageLoad;
