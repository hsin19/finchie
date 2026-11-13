// What needs the owner, the one thing the overview puts first; nothing at all while all is well (AGENTS.md:
// quiet unless action is needed). Each says where to look into it, and names what it is about.

import {
    accountName,
    accountStatus,
    daysAgo,
    lapsesSoon,
} from "#lib/accounts.js";
import type {
    Account,
    StateCount,
} from "#lib/api/client.js";
import { slashDate } from "#lib/dates.js";
import { documentsQueryHref } from "#lib/documents.js";
import { institution } from "#lib/institutions.js";
import { formatAmount } from "#lib/money.js";
import { resolve } from "$app/paths";
import type { ResolvedPathname } from "$app/types";

export interface Need {
    key: "documents" | "gaps" | "stale" | "lapsing";
    text: string;
    /** Where to look into it. */
    href: ResolvedPathname;
    /** What it is about, each with its own page. */
    names: { text: string; href: ResolvedPathname; }[];
}

const accountHref = (a: Account) => resolve("/accounts/[id]", { id: String(a.id) });
const named = (a: Account) => `${institution(a.institution).name} ${accountName(a)}`.trim();

/**
 * In the order to deal with them: documents whose statement does not add up or that failed, accounts
 * whose balances do not join up, accounts whose source has gone quiet, and points about to lapse.
 */
export function needs({ byState, accounts, today }: { byState: readonly StateCount[]; accounts: readonly Account[]; today: string; }): Need[] {
    const found: Need[] = [];
    const count = (state: StateCount["state"]) => byState.find(s => s.state === state)?.count ?? 0;

    const review = count("needs_review");
    const failed = count("failed");
    if (review + failed > 0) {
        const parts = [review ? `${review} 份對不起來` : "", failed ? `${failed} 份解析失敗` : ""].filter(Boolean);
        found.push({ key: "documents", text: `${review + failed} 份文件要處理：${parts.join("、")}`, href: documentsQueryHref("show=todo"), names: [] });
    }

    const gapped = accounts.filter(a => a.gaps.length > 0);
    if (gapped.length > 0) {
        found.push({
            key: "gaps",
            text: `${gapped.length} 個帳戶的餘額接不起來：多半是少了一期對帳單`,
            href: resolve("/accounts"),
            names: gapped.map(a => ({ text: named(a), href: accountHref(a) })),
        });
    }

    const stale = accounts.map(a => ({ a, status: accountStatus(a, today) })).filter(({ status }) => status.stale);
    if (stale.length > 0) {
        found.push({
            key: "stale",
            text: `${stale.length} 個帳戶太久沒更新`,
            href: resolve("/accounts"),
            names: stale.map(({ a, status }) => ({ text: `${named(a)}（${daysAgo(status.days ?? 0)}）`, href: accountHref(a) })),
        });
    }

    const lapsing = accounts.filter(a => a.expiries[0] && lapsesSoon(a.expiries[0].by, today));
    if (lapsing.length > 0) {
        found.push({
            key: "lapsing",
            text: "點數快到期",
            href: resolve("/accounts"),
            names: lapsing.map(a => {
                const next = a.expiries[0];
                return { text: `${named(a)} ${formatAmount(next.amount)} ${next.unit} ${slashDate(next.by)} 前`, href: accountHref(a) };
            }),
        });
    }
    return found;
}
