// A card bill as every page shows it: its status tag and whether it adds up. One rule each, so a bill
// reads the same on the overview, the month, the list, its own page, and a transaction's detail.

import type {
    AccountDetail,
    Balance,
    Document,
    Expiry,
    Statement,
    Transaction,
} from "#lib/api/client.js";
import { slashDate } from "#lib/dates.js";
import { isPoints } from "#lib/flows.js";
import {
    formatAmount,
    sign,
    subtract,
} from "#lib/money.js";

/**
 * The bill's tag: what became of its document when it is known, since a re-parse that failed leaves the
 * statement of the parse before it (and its document says so); else whether it adds up. A re-parse
 * the page asked for and has not seen start yet is `pending` as well.
 */
export function statementStatus(
    statement: Pick<Statement, "reconciliation">,
    doc?: Pick<Document, "status">,
    { reparsing = false } = {},
): Document["status"] {
    if (reparsing) return "pending";
    if (doc) return doc.status;
    return statement.reconciliation.balanced ? "parsed" : "needs_review";
}

/** What the bill prints minus what its lines add up to: positive when lines are missing. */
export function reconciliationDelta(statement: Pick<Statement, "reconciliation">): string {
    return subtract(statement.reconciliation.stated, statement.reconciliation.computed);
}

/** "balanced", or "Δ +120" when the lines do not add up to the bill. */
export function reconciliationText(statement: Pick<Statement, "reconciliation">): string {
    return statement.reconciliation.balanced ? "balanced" : `Δ ${formatAmount(reconciliationDelta(statement), { signed: true })}`;
}

/** Why a bill does not add up, in words. */
export function deltaMeaning(delta: string): string {
    return sign(delta) < 0 ? "明細比帳單多，可能多抓或重複了一列" : "帳單比明細多，可能漏抓了一列";
}

/** A bill in a line: "12 筆 · Δ +120 · 需確認 · 11/25 到期 23,104 · 已繳 11/20". */
export function statementSummary(s: Pick<Statement, "transactionCount" | "reconciliation" | "dueDate" | "totalDue" | "paidOn">): string {
    const reconciled = s.reconciliation.balanced ? "balanced" : `${reconciliationText(s)} · 需確認`;
    const paid = s.paidOn ? ` · 已繳 ${slashDate(s.paidOn)}` : "";
    return `${s.transactionCount} 筆 · ${reconciled} · ${slashDate(s.dueDate)} 到期 ${formatAmount(s.totalDue)}${paid}`;
}

export interface BillPoints {
    account: AccountDetail;
    unit: string;
    /** The bill's lines of this program: its period totals. */
    lines: Transaction[];
    /** The program's balance after this bill. */
    balance?: Balance;
    /** What will lapse by when: known as of the newest bill only, so empty on an older one. */
    expiries: Expiry[];
}

/**
 * The points a card bill reports (ADR 0019), by program: the bank's points accounts and the bill's
 * lines. A program with no movement in the period still has its balance. Account history is newest
 * first (the API's order).
 */
export function billPoints(statementId: number, accounts: AccountDetail[], lines: Transaction[]): BillPoints[] {
    return accounts.flatMap(account => {
        const own = lines.filter(t => isPoints(t) && t.accountId === account.id);
        const balances = account.history.filter(b => b.statementId === statementId);
        const units = [...new Set([...own.map(t => t.unit), ...balances.map(b => b.unit)])];
        return units.map(unit => ({
            account,
            unit,
            lines: own.filter(t => t.unit === unit),
            balance: balances.find(b => b.unit === unit),
            expiries: account.history.find(b => b.unit === unit)?.statementId === statementId ? account.expiries.filter(e => e.unit === unit) : [],
        }));
    });
}
