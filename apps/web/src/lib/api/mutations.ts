// Changes the owner makes from the UI. Each one goes through change(): it reports a failure the same
// way everywhere, and reloads what the change touched.

import {
    goto,
    invalidate,
} from "$app/navigation";
import type { ResolvedPathname } from "$app/types";
import {
    api,
    type Data,
    LEDGER,
    type Result,
    unwrap,
} from "./client.js";

/** What a change gave back, or why it failed, in words to show next to the control. */
export type Outcome<T> = { data: T; error?: undefined; } | { data?: undefined; error: string; };

/**
 * Sends a change and, once it is done, reloads every load that reads the ledger (LEDGER). A failure
 * comes back as `error`, never thrown: the server's message, or that the backend could not be reached.
 *
 * A change that removes what the page shows goes to `then` instead, and the ledger reloads there: reloaded
 * in place, the page would find what it shows gone. The page it leaves is replaced in the history, so Back
 * does not return to it either.
 */
export async function change<R extends Result>(request: Promise<R>, then?: ResolvedPathname): Promise<Outcome<Data<R>>> {
    let data: Data<R>;
    try {
        data = unwrap(await request);
    } catch (error) {
        return { error: error instanceof TypeError ? "連不到 backend" : (error as Error).message };
    }
    if (then) await goto(then, { replace: true, invalidate: [LEDGER] });
    else await invalidate(LEDGER);
    return { data };
}

/** Parses a document again from its original (ADR 0005); it turns pending, and the layout follows it. */
export function reparse(id: number) {
    return change(api().POST("/api/documents/{id}/reparse", { params: { path: { id } } }));
}

/** One of an account's gaps, as the account lists it. */
export interface GapRef {
    unit: string;
    from: string;
    to: string;
}

/**
 * Makes up for a gap whose reports are lost with what it lacks: its two balances join up, and it
 * counts from the last day no report covers. A report found later takes over by itself.
 */
export function fillGap(accountId: number, gap: GapRef) {
    const { unit, from, to } = gap;
    return change(api().POST("/api/accounts/{id}/fills", { params: { path: { id: accountId } }, body: { unit, from, to } }));
}

/** What the owner writes about a fill, such as why its reports are lost; empty clears it. */
export function setFillMemo(accountId: number, fillId: number, memo: string) {
    return change(api().PATCH("/api/accounts/{id}/fills/{fillId}", { params: { path: { id: accountId, fillId } }, body: { memo } }));
}

/** Takes a fill back: the gap shows again. */
export function unfillGap(accountId: number, fillId: number) {
    return change(api().DELETE("/api/accounts/{id}/fills/{fillId}", { params: { path: { id: accountId, fillId } } }));
}

/**
 * Deletes a document that holds nothing of the ledger (`deletable`), its original included, and goes to
 * `then`: the document's own page would have nothing to show.
 */
export function deleteDocument(id: number, then: ResolvedPathname) {
    return change(api().DELETE("/api/documents/{id}", { params: { path: { id } } }), then);
}
