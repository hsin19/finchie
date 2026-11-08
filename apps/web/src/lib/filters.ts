// The transactions page keeps every filter in the URL
// (?m=2026-10&kind=purchase&card=hsbc:4417&q=uber&overseas=1&hide=transfers&sort=largest&page=2), so a
// filtered view can be bookmarked and the back button undoes a filter. ?card= picks a bank or one of its
// cards, ?account= a deposit account. ?closing=2026-10 keeps the rows of the
// statements that closed that month; it replaces the default period instead of adding to it.

import {
    type TransactionKind,
    TRANSACTIONS_PAGE_SIZE,
    type TransactionSort,
} from "#lib/api/client.js";
import {
    isMonth,
    type Month,
} from "#lib/dates.js";
import { kindOrder } from "#lib/labels.js";
import type { ReadonlyURLSearchParams } from "$app/state";

const SORTS: TransactionSort[] = ["newest", "oldest", "largest", "smallest"];

export interface Filters {
    /** The period asked for, if any: a month or every month. */
    period?: Month | "all";
    closing?: Month;
    kinds: TransactionKind[];
    institution: string;
    card: string;
    /** A deposit account's ID, or "". */
    account: string;
    q: string;
    overseas: boolean;
    /** Leave out transfers between the owner's lines and card payments (ADR 0016). */
    hideTransfers: boolean;
    sort: TransactionSort;
    page: number;
}

export function readFilters(params: ReadonlyURLSearchParams): Filters {
    const m = params.get("m");
    const closing = params.get("closing");
    const [institution = "", card = ""] = (params.get("card") ?? "").split(":");
    const sort = params.get("sort");
    return {
        period: m === "all" ? "all" : isMonth(m) ? m : undefined,
        closing: isMonth(closing) ? closing : undefined,
        kinds: params.getAll("kind").filter((k): k is TransactionKind => (kindOrder as string[]).includes(k)),
        institution,
        card,
        account: /^\d+$/.test(params.get("account") ?? "") ? params.get("account")! : "",
        q: params.get("q") ?? "",
        overseas: params.get("overseas") === "1",
        hideTransfers: params.getAll("hide").includes("transfers"),
        sort: SORTS.find(s => s === sort) ?? "newest",
        page: Math.max(1, Math.floor(Number(params.get("page"))) || 1),
    };
}

/** The period searched when none is asked for: every month for a closing month, else `latest`. */
export function searchPeriod(filters: Filters, latest: Month): Month | "all" {
    return filters.period ?? (filters.closing ? "all" : latest);
}

export function pageCount(total: number): number {
    return Math.max(1, Math.ceil(total / TRANSACTIONS_PAGE_SIZE));
}

/** New values by parameter name: null or "" removes the parameter, an array repeats it. */
export type FilterChanges = Record<string, string | string[] | null>;

/** `params` with `changes` applied. Any change but a page change starts again at page 1. */
export function withChanges(params: ReadonlyURLSearchParams, changes: FilterChanges): URLSearchParams {
    const dropped = new Set([...Object.keys(changes), "page"]);
    const kept = [...params].filter(([key]) => !dropped.has(key));
    const added = Object.entries(changes).flatMap(([key, value]) => (value === null ? [] : [value].flat()).filter(v => v !== "").map(v => [key, v]));
    return new URLSearchParams([...kept, ...added]);
}
