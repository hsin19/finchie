import { error } from "@sveltejs/kit";
import createClient, {
    type FetchResponse,
    type MaybeOptionalInit,
} from "openapi-fetch";
import type { paths } from "./schema";
import {
    reloader,
    withSession,
} from "./session.js";

export type * from "./types.js";

/**
 * What every load that reads the ledger depends on (declared with depends(), never only through
 * parent()): a change to it (mutations.ts) or a parse that finished (the layout's poll) reloads them all.
 */
export const LEDGER = "app:ledger";

export const TRANSACTIONS_PAGE_SIZE = 50;
export const DOCUMENTS_PAGE_SIZE = 50;

const signIn = reloader();

type Method = "get" | "put" | "post" | "delete" | "patch";

/** The paths that take the method. */
type PathsWith<M extends Method> = { [P in keyof paths]: paths[P] extends Record<M, object> ? P : never; }[keyof paths];

/** What a request to the path takes: its parameters and body, and fetch's own options (`signal`). */
type Init<P extends keyof paths, M extends Method> = MaybeOptionalInit<paths[P], M>;

/**
 * One method of the API, as openapi-fetch's but with the request typed by its path. openapi-fetch infers
 * the request's type from the argument, and accepts any extra key on it, so a parameter the API does not
 * know (misspelled, or renamed in openapi.yaml) passes the type check and the backend ignores it. Here the
 * path alone decides the type, so the argument is checked against it, its query and body included.
 */
type Call<M extends Method> = <P extends PathsWith<M>>(
    url: P,
    ...init: undefined extends Init<P, M> ? [init?: Init<P, M>] : [init: Init<P, M>]
) => Promise<FetchResponse<Extract<paths[P][M], object>, Init<P, M>, `${string}/${string}`>>;

export interface Api {
    GET: Call<"get">;
    PUT: Call<"put">;
    POST: Call<"post">;
    DELETE: Call<"delete">;
    PATCH: Call<"patch">;
}

/**
 * In a load function pass its `fetch`, so SvelteKit can track and replay the requests. No redirect is
 * followed: the API has none of its own, so one is Cloudflare Access asking to sign in (session.ts).
 */
export function api(fetchFn: typeof fetch = fetch): Api {
    // The same client: Api only types its requests more strictly than openapi-fetch's methods.
    return createClient<paths>({ fetch: withSession(fetchFn, signIn), redirect: "manual" }) as unknown as Api;
}

export interface Result {
    data?: unknown;
    error?: unknown;
    response: Response;
}

export type Data<R extends Result> = NonNullable<R["data"]>;

export function message(result: Result): string {
    const body = result.error as { message?: unknown; } | undefined;
    return typeof body?.message === "string" ? body.message : result.response.statusText || `HTTP ${result.response.status}`;
}

/** For load functions: an error response becomes SvelteKit's error page. */
export function need<R extends Result>(result: R): Data<R> {
    if (result.data === undefined) error(result.response.status, message(result));
    return result.data as Data<R>;
}

/** For load functions whose page is still of use without this response: its data, or why it failed. */
export function attempt<R extends Result>(result: R): { data: Data<R>; error?: undefined; } | { data?: undefined; error: string; } {
    return result.data === undefined ? { error: message(result) } : { data: result.data as Data<R> };
}

/**
 * For event handlers: an error response becomes an Error to show next to the control. Success is the
 * status, not a body: a 204 has none.
 */
export function unwrap<R extends Result>(result: R): Data<R> {
    if (!result.response.ok) throw new Error(message(result));
    return result.data as Data<R>;
}

export function documentContentUrl(id: number): string {
    return `/api/documents/${id}/content`;
}

/** The document's statement PDF, decrypted (ADR 0012). */
export function previewPdfUrl(id: number): string {
    return `/api/documents/${id}/preview/pdf`;
}

/** One file of a document opened for reading, a PDF decrypted. */
export function previewFileUrl(id: number, index: number): string {
    return `/api/documents/${id}/preview/files/${index}`;
}

export function previewBodyUrl(id: number): string {
    return `/api/documents/${id}/preview/body`;
}

/** The Gmail filter that forwards statement e-mails to the mailbox, generated from the parsers (ADR 0014). */
export const MAIL_FILTERS_URL = "/api/mail-filters.xml";
