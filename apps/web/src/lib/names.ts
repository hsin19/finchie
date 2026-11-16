// How the UI names an account and a day gone by, with nothing of the app's router: the service worker
// words a push with them too.

import type { Account } from "#lib/api/types.js";
import { accountKinds } from "#lib/kinds.js";

/** The owner's name for an account, else the bank's ("臺幣活存"), else its kind's. */
export function accountName(account: Pick<Account, "kind" | "label" | "name">): string {
    return account.name ?? account.label ?? accountKinds[account.kind].label;
}

/** "今天", "昨天", "41 天前". */
export function daysAgo(days: number): string {
    return days <= 0 ? "今天" : days === 1 ? "昨天" : `${days} 天前`;
}
