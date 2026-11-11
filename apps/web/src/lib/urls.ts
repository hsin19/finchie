// The statements pages' URLs: what their query means, and links to them. (The transactions page's
// are in filters.ts, the documents list's in documents.ts, the accounts list's in accounts.ts.)

import {
    isMonth,
    type Month,
} from "#lib/dates.js";
import { resolve } from "$app/paths";
import type { ResolvedPathname } from "$app/types";

/** /statements: one closing month's bill (?m=, else the newest), or every statement (?all). */
export type StatementsView = { all: true; } | { all: false; month?: Month; };

export function readStatementsView(params: Pick<URLSearchParams, "get" | "has">): StatementsView {
    if (params.has("all")) return { all: true };
    const m = params.get("m");
    return { all: false, month: isMonth(m) ? m : undefined };
}

/** A month's bill, every statement ("all"), or the newest month's bill. */
export function statementsHref(view?: Month | "all"): ResolvedPathname {
    if (view === "all") return resolve("/statements?all");
    return view ? resolve(`/statements?m=${view}`) : resolve("/statements");
}

/** A statement's page, scrolled to one of its lines (?line=). */
export function statementHref(id: number, line?: number): ResolvedPathname {
    return line === undefined ? resolve("/statements/[id]", { id: String(id) }) : resolve(`/statements/[id]?line=${line}`, { id: String(id) });
}

/** The line a statement's page shows, if any. */
export function readLine(params: Pick<URLSearchParams, "get">): number | undefined {
    const line = Number(params.get("line"));
    return Number.isSafeInteger(line) && line > 0 ? line : undefined;
}
