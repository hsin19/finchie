// A card bill as every page shows it: its status tag and whether it adds up. One rule each, so a bill
// reads the same on the overview, the month, the list, its own page, and a transaction's detail.

import type {
    AccountDetail,
    Balance,
    Check,
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
 * statement of the parse before it (and its document says so); else whether every check balances. A
 * re-parse the page asked for and has not seen start yet is `pending` as well.
 */
export function statementStatus(
    statement: Pick<Statement, "balanced">,
    doc?: Pick<Document, "status">,
    { reparsing = false } = {},
): Document["status"] {
    if (reparsing) return "pending";
    if (doc) return doc.status;
    return statement.balanced ? "parsed" : "needs_review";
}

/** What the bill prints minus what its lines come to: positive when lines are missing. */
export function checkDelta(check: Pick<Check, "stated" | "computed">): string {
    return subtract(check.stated, check.computed);
}

/** The checks that do not balance, in the bill's order. */
export function failingChecks(statement: Pick<Statement, "checks">): Check[] {
    return statement.checks.filter(c => !c.balanced);
}

/** "本期新增款項 Δ +120", with the check's unit where it is not the bill's currency ("e point Δ -20"). */
export function checkText(check: Check, currency: string): string {
    const unit = check.unit && check.unit !== currency ? ` ${check.unit}` : "";
    return `${check.name} Δ ${formatAmount(checkDelta(check), { signed: true })}${unit}`;
}

/** "balanced", or each check that does not balance: "本期新增款項 Δ +120；e point Δ -20 e point". */
export function checksText(statement: Pick<Statement, "balanced" | "checks" | "currency">): string {
    if (statement.balanced) return "balanced";
    const failing = failingChecks(statement);
    // Unbalanced without a failing check should not happen; say so rather than nothing.
    return failing.length > 0 ? failing.map(c => checkText(c, statement.currency)).join("；") : "對不上";
}

/** Why a check does not add up, in words. */
export function deltaMeaning(delta: string): string {
    return sign(delta) < 0 ? "明細比帳單多，可能多抓或重複了一列" : "帳單比明細多，可能漏抓了一列";
}

/** A bill in a line: "12 筆 · 本期新增款項 Δ +120 · 需確認 · 11/25 到期 23,104 · 已繳 11/20". */
export function statementSummary(s: Pick<Statement, "transactionCount" | "balanced" | "checks" | "currency" | "dueDate" | "totalDue" | "paidOn">): string {
    const reconciled = s.balanced ? "balanced" : `${checksText(s)} · 需確認`;
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
