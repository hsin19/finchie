import {
    api,
    LEDGER,
    need,
} from "#lib/api/client.js";
import { monthOf } from "#lib/dates.js";
import { error } from "@sveltejs/kit";
import type { PageLoad } from "./$types";

export const load = (async ({ fetch, params, depends }) => {
    // A re-parse keeps the statement's id: once it is done, the ledger reloads and so does this page.
    depends(LEDGER);
    const id = Number(params.id);
    if (!Number.isSafeInteger(id) || id <= 0) error(404, `statement ${params.id} not found`);

    const client = api(fetch);
    const found = await client.GET("/api/statements/{id}", { params: { path: { id } } });
    // Gone: a re-parse found its document a duplicate, or an old link.
    if (found.response.status === 404) error(404, `statement ${id} not found: a re-parse may have replaced it`);
    const statement = need(found);
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
