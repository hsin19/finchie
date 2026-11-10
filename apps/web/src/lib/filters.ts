// The transactions page keeps every filter in the URL
// (?m=2026-10&kind=purchase&card=hsbc:4417&q=uber&overseas=1&hide=transfers&sort=largest&page=2), so a
// filtered view can be bookmarked and the back button undoes a filter. ?card= picks a bank or one of its
// cards, ?account= a deposit account. ?closing=2026-10 keeps the rows of the
// statements that closed that month; it replaces the default period instead of adding to it.

import { lineAccount } from "#lib/accounts.js";
import {
    type TransactionKind,
    TRANSACTIONS_PAGE_SIZE,
    type TransactionSearch,
    type TransactionSort,
} from "#lib/api/client.js";
import {
    isMonth,
    type Month,
} from "#lib/dates.js";
import {
    cardLabel,
    institution,
} from "#lib/institutions.js";
import { isKind } from "#lib/kinds.js";
import { resolve } from "$app/paths";
import type { ReadonlyURLSearchParams } from "$app/state";
import type { ResolvedPathname } from "$app/types";

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
    const { institution, card } = readCard(params.get("card"));
    const sort = params.get("sort");
    return {
        period: m === "all" ? "all" : isMonth(m) ? m : undefined,
        closing: isMonth(closing) ? closing : undefined,
        kinds: params.getAll("kind").filter(isKind),
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

/** ?card=: a bank ("hsbc") or one of its cards ("hsbc:4417"). */
function readCard(value: string | null): { institution: string; card: string; } {
    const [institution = "", card = ""] = (value ?? "").split(":");
    return { institution, card };
}

/** The ?card= value for a bank, or one of its cards. */
export function cardParam(institution: string, last4?: string | null): string {
    return last4 ? `${institution}:${last4}` : institution;
}

/** The query that reads back as `filters`; what is the default is left out. */
export function filterParams(filters: Partial<Filters>): URLSearchParams {
    const params = new URLSearchParams();
    if (filters.period) params.set("m", filters.period);
    if (filters.closing) params.set("closing", filters.closing);
    for (const kind of filters.kinds ?? []) params.append("kind", kind);
    if (filters.institution) params.set("card", cardParam(filters.institution, filters.card));
    if (filters.account) params.set("account", filters.account);
    if (filters.q) params.set("q", filters.q);
    if (filters.overseas) params.set("overseas", "1");
    if (filters.hideTransfers) params.set("hide", "transfers");
    if (filters.sort && filters.sort !== "newest") params.set("sort", filters.sort);
    if (filters.page && filters.page > 1) params.set("page", String(filters.page));
    return params;
}

/** The transactions page with these filters. */
export function transactionsHref(filters: Partial<Filters> = {}): ResolvedPathname {
    return transactionsQueryHref(filterParams(filters));
}

/** The transactions page with this query, as built by withChanges. */
export function transactionsQueryHref(params: URLSearchParams | string): ResolvedPathname {
    const query = params.toString();
    return query ? resolve(`/transactions?${query}`) : resolve("/transactions");
}

// The account picker is one select for a bank, one of its cards, or a deposit account; its values
// are the ?card= value, or "account:" and the account's ID.

const ACCOUNT = "account:";

/** The picker's value for the filters. */
export function pickerValue(filters: Pick<Filters, "account" | "institution" | "card">): string {
    if (filters.account) return `${ACCOUNT}${filters.account}`;
    return filters.institution ? cardParam(filters.institution, filters.card) : "";
}

/** The URL changes for picking `value` ("" for every account). */
export function pickerChanges(value: string): FilterChanges {
    const account = value.startsWith(ACCOUNT) ? value.slice(ACCOUNT.length) : null;
    return { account, card: account ? null : value || null };
}

/**
 * Each bank, then its cards and its deposit accounts. Counts come from the byCard and byAccount facets,
 * which ignore the picker, so every option shows what picking it would give.
 */
export function pickerOptions({ byCard, byAccount }: Pick<TransactionSearch, "byCard" | "byAccount">): { value: string; label: string; }[] {
    const options: { value: string; label: string; }[] = [];
    const banks = [...new Set([...byCard, ...byAccount].map(c => c.institution))].sort();
    for (const bank of banks) {
        const cards = byCard.filter(c => c.institution === bank);
        const accounts = byAccount.filter(a => a.institution === bank);
        options.push({ value: bank, label: `${institution(bank).code} (all) · ${[...cards, ...accounts].reduce((n, c) => n + c.count, 0)}` });
        for (const c of cards) if (c.cardLast4) options.push({ value: cardParam(bank, c.cardLast4), label: `${cardLabel(bank, c.cardLast4)} · ${c.count}` });
        for (const a of accounts) {
            const name = lineAccount({ institution: bank, accountKind: "deposit", accountName: a.accountName });
            options.push({ value: `${ACCOUNT}${a.accountId}`, label: `${name} ${a.accountNumber ?? ""} · ${a.count}` });
        }
    }
    return options;
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
