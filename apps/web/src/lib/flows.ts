// Amounts as the owner sees them (ADR 0013): money in positive, money out negative, whichever account
// a line is on. A card bill prints a purchase positive; to the owner it is money out.

import type { Transaction } from "#lib/api/client.js";
import { net } from "#lib/family.js";
import {
    negate,
    sum,
} from "#lib/money.js";

type Line = Pick<Transaction, "accountKind" | "amount" | "children" | "counterpart" | "flow" | "kind" | "unit">;

/** A transfer between the owner's lines, or a card bill's payment line: neither spent nor received (ADR 0016). */
export function isTransfer(t: Pick<Transaction, "counterpart" | "kind">): boolean {
    return t.counterpart !== undefined || t.kind === "payment";
}

/** The line's flow, net of the children folded into it. */
export function flowOf(t: Line): string {
    return t.flow ?? (t.accountKind === "deposit" ? t.amount : negate(net(t)));
}

/** What the lines came to per unit, transfers left out: TWD first. */
export function netByUnit(lines: Line[]): { unit: string; amount: string; }[] {
    const counted = lines.filter(t => !isTransfer(t));
    const units = [...new Set(counted.map(t => t.unit ?? "TWD"))].sort((a, b) => Number(b === "TWD") - Number(a === "TWD") || a.localeCompare(b));
    return units.map(unit => ({ unit, amount: sum(counted.filter(t => (t.unit ?? "TWD") === unit).map(flowOf)) }));
}
