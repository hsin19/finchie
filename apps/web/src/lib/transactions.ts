// A transaction as the transactions page shows it, in a row (desktop or mobile) or in its detail: one
// view of each part, so the two lists cannot drift apart.

import type {
    LineCounterpart,
    Transaction,
} from "#lib/api/client.js";
import { refundedInFull } from "#lib/family.js";
import {
    flowOf,
    isPoints,
    isTrade,
    isTransfer,
    netByUnit,
} from "#lib/flows.js";
import { institution } from "#lib/institutions.js";
import { kinds } from "#lib/kinds.js";
import {
    approx,
    formatAmount,
    sign,
} from "#lib/money.js";
import { tone } from "#lib/tone.js";
import { statementHref } from "#lib/urls.js";
import { resolve } from "$app/paths";
import type { ResolvedPathname } from "$app/types";

/** The amount as the owner sees it: money out negative, whichever account (ADR 0013). */
export function amountText(t: Transaction): string {
    return formatAmount(flowOf(t), { signed: true });
}

/** Its color: money that only moved between the owner's own lines is muted. */
export function amountTone(t: Transaction): string {
    if (refundedInFull(t) || isTransfer(t)) return "text-mute";
    // Shares or points, not money (ADR 0017, ADR 0019).
    if (isTrade(t) || isPoints(t)) return "text-ink";
    if (sign(flowOf(t)) > 0) return "text-pos";
    return t.kind === "purchase" || t.kind === "other" ? "text-ink" : tone[kinds[t.kind].tone];
}

/** The unit after an amount, unless it is the base currency. */
export function unitSuffix(t: Pick<Transaction, "unit">, base: string): string {
    return t.unit !== base ? t.unit : "";
}

/** "USD 12.50": what the line was in its foreign currency, or "". */
export function original(t: Pick<Transaction, "originalAmount" | "originalCurrency">): string {
    return t.originalAmount && t.originalCurrency ? `${t.originalCurrency} ${formatAmount(t.originalAmount)}` : "";
}

/** "32.10 TWD / USD": the rate the bank applied, for showing only (a ratio, not money). */
export function rate(t: Pick<Transaction, "amount" | "originalAmount" | "originalCurrency">, base: string): string | undefined {
    if (!t.originalAmount || !t.originalCurrency || approx(t.originalAmount) === 0) return undefined;
    return `${(approx(t.amount) / approx(t.originalAmount)).toFixed(2)} ${base} / ${t.originalCurrency}`;
}

/** Lines in a row by their date, as listed: each day's lines together. */
export function byDay<T extends Pick<Transaction, "transactionDate">>(items: readonly T[]): { date: string; items: T[]; }[] {
    const groups: { date: string; items: T[]; }[] = [];
    for (const t of items) {
        if (groups.at(-1)?.date !== t.transactionDate) groups.push({ date: t.transactionDate, items: [] });
        groups.at(-1)!.items.push(t);
    }
    return groups;
}

/** "-1,200 · USD +50": what a day came to, per unit, the base currency first and unnamed. */
export function dayTotal(lines: Transaction[], base: string): string {
    return netByUnit(lines, base).map(n => `${n.unit === base ? "" : `${n.unit} `}${formatAmount(n.amount, { signed: true })}`).join(" · ");
}

/**
 * Where a linked line's money went or came from (ADR 0016): the card bill a payment went to, at its
 * payment line, which says more than the card account; or another of the owner's accounts.
 */
export function counterpartLink(other: LineCounterpart): { href: ResolvedPathname; text: string; } {
    if (other.accountKind !== "credit_card" && other.accountId !== undefined) {
        return {
            href: resolve("/accounts/[id]", { id: String(other.accountId) }),
            text: [institution(other.institution).name, other.accountName, other.accountNumber].filter(Boolean).join(" "),
        };
    }
    return { href: statementHref(other.statementId, other.id), text: `${institution(other.institution).name} 信用卡帳單` };
}
