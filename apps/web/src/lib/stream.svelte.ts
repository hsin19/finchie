// The app's first page draws before its data is in: its loads hand the data over as promises, so the layout
// draws at once and the page shows its skeleton until the data comes (Loaded). After that, loads wait for
// their data again, as SvelteKit expects: a navigation keeps its scroll restoration and the layout's skeleton
// covers the wait, and a reload keeps the page as it is until the new data is in.

import { reportError } from "#lib/telemetry.js";
import { untrack } from "svelte";

/** How long a wait goes unshown: past it, a skeleton, so a quick load does not flash one. */
export const SKELETON_AFTER_MS = 150;

/** A load's data, either in hand or, on the app's first page, still coming. */
export type Deferred<T> = { value: T; } | { promise: Promise<T>; };

/** What a Deferred holds, once it is in. */
export type Settle<D> = D extends Deferred<infer V> ? V : never;

/** A page's data as its content takes it: the layout's (`ledger`) with the page's own (`content`). */
export type Shown<D extends { ledger: Deferred<object>; content: Deferred<object>; }> = Settle<D["ledger"]> & Settle<D["content"]>;

let drawn = false;

// What the first page's data turned out to be, once in. The layout's load does not run again on a navigation,
// so later pages get the same Deferred: in hand by then, they draw within their navigation, before SvelteKit
// restores the scroll and runs afterNavigate, as they would have had the load waited.
const arrived = new WeakMap<Promise<unknown>, { value: unknown; }>();

/** Called once the app has drawn its first page: from then on, loads wait for their data. */
export function markDrawn(): void {
    drawn = true;
}

/** A load's data: on the app's first page still coming, so the load returns at once; after it, in hand. */
export async function defer<T>(data: Promise<T>): Promise<Deferred<T>> {
    if (drawn) return { value: await data };
    // Registered before anyone else can wait for it, so it is recorded before they hear. Loaded takes the
    // failure; this also keeps the promise from counting as unhandled until it does.
    data.then(value => arrived.set(data, { value }), () => undefined);
    return { promise: data };
}

/** A Deferred as it stands: in hand once its data has come. */
export function current<T>(data: Deferred<T>): Deferred<T> {
    if ("value" in data) return data;
    return (arrived.get(data.promise) as { value: T; } | undefined) ?? data;
}

/** The data a Deferred holds, when it is in. */
export function valueOf<T>(data: Deferred<T>): Promise<T> {
    const now = current(data);
    return "value" in now ? Promise.resolve(now.value) : now.promise;
}

/** Two Deferreds' data together, as one: in hand when both are. */
export function join<A extends object, B extends object>(first: Deferred<A>, second: Deferred<B>): Deferred<A & B> {
    const a = current(first);
    const b = current(second);
    if ("value" in a && "value" in b) return { value: { ...a.value, ...b.value } };
    return { promise: Promise.all([valueOf(a), valueOf(b)]).then(([x, y]) => ({ ...x, ...y })) };
}

/**
 * The newest data a Deferred source has held, kept while newer data is still coming, and why the newest
 * failed, if it did. Made while a component initializes.
 */
export class Settled<T> {
    value = $state.raw<T>();
    error = $state.raw<unknown>();

    constructor(source: () => Deferred<T>) {
        const first = current(untrack(source));
        if ("value" in first) this.value = first.value;
        $effect(() => {
            const data = current(source());
            if ("value" in data) {
                this.value = data.value;
                this.error = undefined;
                return;
            }
            let fresh = true;
            data.promise.then(
                value => {
                    if (!fresh) return;
                    this.value = value;
                    this.error = undefined;
                },
                (error: unknown) => {
                    if (!fresh) return;
                    this.error = error;
                    // Shown in place, it is caught here and never reaches window.onerror.
                    reportError(error);
                },
            );
            return () => {
                fresh = false;
            };
        });
    }
}
