// Changes the owner makes from the UI. Each one goes through change(): it reports a failure the same
// way everywhere, and reloads what the change touched.

import { invalidate } from "$app/navigation";
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
 */
export async function change<R extends Result>(request: Promise<R>): Promise<Outcome<Data<R>>> {
    let data: Data<R>;
    try {
        data = unwrap(await request);
    } catch (error) {
        return { error: error instanceof TypeError ? "連不到 backend" : (error as Error).message };
    }
    await invalidate(LEDGER);
    return { data };
}

/** Parses a document again from its original (ADR 0005); it turns pending, and the layout follows it. */
export function reparse(id: number) {
    return change(api().POST("/api/documents/{id}/reparse", { params: { path: { id } } }));
}
