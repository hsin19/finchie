import { error } from "@sveltejs/kit";
import createClient from "openapi-fetch";
import type {
    components,
    paths,
} from "./schema";
import {
    reloader,
    withSession,
} from "./session.js";

// Generated from apps/backend/api/openapi.yaml by `task web:generate`.
type Schemas = components["schemas"];
export type Account = Schemas["Account"];
export type AccountDetail = Schemas["AccountDetail"];
export type AccountKind = Schemas["AccountKind"];
export type AccountLine = Schemas["AccountLine"];
export type Balance = Schemas["Balance"];
export type Expiry = Schemas["Expiry"];
export type BalanceGap = Schemas["BalanceGap"];
export type AccountSide = Schemas["AccountSide"];
export type CardTotal = Schemas["CardTotal"];
export type Check = Schemas["Check"];
export type Document = Schemas["Document"];
export type DocumentPage = Schemas["DocumentPage"];
export type DocumentPreview = Schemas["DocumentPreview"];
export type DocumentFailure = Schemas["DocumentFailure"];
export type DocumentStatus = Schemas["DocumentStatus"];
export type KindTotal = Schemas["KindTotal"];
export type LineCounterpart = Schemas["LineCounterpart"];
export type NetWorth = Schemas["NetWorth"];
export type NetWorthPoint = Schemas["NetWorthPoint"];
export type Position = Schemas["Position"];
export type PreviewFile = Schemas["PreviewFile"];
export type Price = Schemas["Price"];
export type PriceSource = Schemas["PriceSource"];
export type StateCount = Schemas["StateCount"];
export type Settings = Schemas["Settings"];
export type Statement = Schemas["Statement"];
export type Trade = Schemas["Trade"];
export type Transaction = Schemas["Transaction"];
export type TransactionFamily = Schemas["TransactionFamily"];
export type TransactionKind = Schemas["TransactionKind"];
export type TransactionLink = Schemas["TransactionLink"];
export type TransactionSearch = Schemas["TransactionSearch"];
export type TransactionSort = Schemas["TransactionSort"];
export type UnitFlow = Schemas["UnitFlow"];
export type TransactionQuery = NonNullable<paths["/api/transactions"]["get"]["parameters"]["query"]>;

/**
 * What every load that reads the ledger depends on (declared with depends(), never only through
 * parent()): a change to it (mutations.ts) or a parse that finished (the layout's poll) reloads them all.
 */
export const LEDGER = "app:ledger";

export const TRANSACTIONS_PAGE_SIZE = 50;
export const DOCUMENTS_PAGE_SIZE = 50;

const signIn = reloader();

/**
 * In a load function pass its `fetch`, so SvelteKit can track and replay the requests. No redirect is
 * followed: the API has none of its own, so one is Cloudflare Access asking to sign in (session.ts).
 */
export function api(fetchFn: typeof fetch = fetch) {
    return createClient<paths>({ fetch: withSession(fetchFn, signIn), redirect: "manual" });
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
