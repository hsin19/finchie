// Amounts as the owner sees them (ADR 0013): money in positive, money out negative, whichever account
// a line is on. A card bill prints a purchase positive; to the owner it is money out.

import type { Transaction } from "#lib/api/client.js";
import { sum } from "#lib/money.js";

type Line = Pick<Transaction, "accountKind" | "counterpart" | "flow" | "kind" | "unit">;

/** A transfer between the owner's lines, or a card bill's payment line: neither spent nor received (ADR 0016). */
export function isTransfer(t: Pick<Transaction, "counterpart" | "kind">): boolean {
    return t.counterpart !== undefined || t.kind === "payment";
}

/** A trade: shares, not money, so neither spent nor received (ADR 0017). */
export function isTrade(t: Pick<Transaction, "accountKind">): boolean {
    return t.accountKind === "securities";
}

/** A points line: points, not money, so neither spent nor received (ADR 0019). */
export function isPoints(t: Pick<Transaction, "accountKind">): boolean {
    return t.accountKind === "points";
}

/** The line's flow as the backend gives it: seen from the owner, net of the children folded into it. */
export function flowOf(t: Pick<Transaction, "flow">): string {
    return t.flow;
}

/** What the lines came to per unit, transfers, trades, and points left out: the base currency first. */
export function netByUnit(lines: Line[], base: string): { unit: string; amount: string; }[] {
    const counted = lines.filter(t => !isTransfer(t) && !isTrade(t) && !isPoints(t));
    const units = [...new Set(counted.map(t => t.unit))].sort((a, b) => Number(b === base) - Number(a === base) || a.localeCompare(b));
    return units.map(unit => ({ unit, amount: sum(counted.filter(t => t.unit === unit).map(flowOf)) }));
}
