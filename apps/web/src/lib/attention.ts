// What needs the owner, the one thing the overview puts first; nothing at all while all is well (AGENTS.md:
// quiet unless action is needed). The backend raises each as a notification and resolves it by itself once
// it is no longer so (internal/notify); the overview shows those still open that the owner has not
// handled, a row per kind, each naming what it is about, and /notifications lists them all.

import {
    accountName,
    checkAgainDefault,
    daysAgo,
} from "#lib/accounts.js";
import type {
    Account,
    Notification,
    NotificationKind,
    Shortfall,
} from "#lib/api/client.js";
import { yearlyDate } from "#lib/dates.js";
import { documentsQueryHref } from "#lib/documents.js";
import { institution } from "#lib/institutions.js";
import { formatAmount } from "#lib/money.js";
import { resolve } from "$app/paths";
import type { ResolvedPathname } from "$app/types";

/** The account a notification is about, as it is named now. */
export type NotifiedAccount = NonNullable<Notification["account"]>;

export interface NeedName {
    text: string;
    href: ResolvedPathname;
    /** The notifications it names. */
    ids: number[];
    /** An overdue account the owner can confirm is current, with the day to look again that gives. */
    confirm?: { id: number; checkAgainOn: string; };
}

export interface Need {
    kind: NotificationKind;
    text: string;
    /** Where to look into it. */
    href: ResolvedPathname;
    /** Every notification the row stands for: handled together where it names nothing. */
    ids: number[];
    names: NeedName[];
}

/** The kinds in the order to deal with them, as the backend lists them: money that may not be there in time first. */
export const notificationKinds: readonly NotificationKind[] = ["shortfall", "document", "gap", "overdue", "lapsing"];

export const accountHref = (a: Pick<NotifiedAccount, "id">) => resolve("/accounts/[id]", { id: String(a.id) });
export const named = (a: Pick<NotifiedAccount, "institution" | "kind" | "label" | "name">) => `${institution(a.institution).name} ${accountName(a)}`.trim();

/** Whether a notification still needs the owner: open, and not handled. */
export const pending = (n: Notification) => !n.resolvedAt && !n.handledAt;

const daysSince = (day: string, today: string) => Math.round((Date.parse(today) - Date.parse(day)) / 86_400_000);

/** "差 2,000（10/31 餘額 5,000，要扣 滙豐 11/09 3,000 + 玉山 11/23 4,000）": what one balance is to cover, and lacks. */
export function describeShortfall(s: Shortfall, base: string, today: string): string {
    const slashDate = (day: string) => yearlyDate(day, today);
    const amount = (value: string) => `${formatAmount(value)}${s.unit === base ? "" : ` ${s.unit}`}`;
    const bills = s.debits.map(d => `${institution(d.institution).name} ${slashDate(d.dueDate)} ${amount(d.amount)}`).join(" + ");
    return `差 ${amount(s.lacks)}（${slashDate(s.asOf)} 餘額 ${amount(s.balance)}，要扣 ${bills}）`;
}

/** What a notification says, without the account it is about; a day of another year than today's says its year. */
export function describe(n: Notification, today: string, base: string): string {
    const slashDate = (day: string) => yearlyDate(day, today);
    switch (n.kind) {
        case "shortfall":
            return n.shortfall ? describeShortfall(n.shortfall, base, today) : "";
        case "document":
            return n.document ? `${n.document.filename}：${n.document.status === "needs_review" ? "對不起來" : "解析失敗"}` : "";
        case "gap":
            return n.gap ? `${n.gap.unit} ${slashDate(n.gap.from)} 到 ${slashDate(n.gap.to)} 之間有 ${formatAmount(n.gap.unseen, { signed: true })} 沒有交易說明` : "";
        case "overdue":
            return n.newest ? `${daysAgo(daysSince(n.newest, today))}的餘額` : "";
        case "lapsing":
            return n.lapsing ? `${formatAmount(n.lapsing.amount)} ${n.lapsing.unit} ${slashDate(n.lapsing.by)} 前${n.lapsing.estimated ? "（推算）" : ""}` : "";
    }
}

/** Where a notification is looked into. */
export function notificationHref(n: Notification): ResolvedPathname {
    if (n.account) return accountHref(n.account);
    if (n.document?.id !== undefined) return resolve("/documents/[id]", { id: String(n.document.id) });
    return resolve("/notifications");
}

/**
 * The notifications that still need the owner, a row per kind in the order to deal with them. An overdue
 * account is confirmed current rather than handled; `accounts` give the day that confirming gives.
 */
export function needs({ notifications, accounts, today, base }: {
    notifications: readonly Notification[];
    accounts: readonly Account[];
    today: string;
    base: string;
}): Need[] {
    const open = notifications.filter(pending);
    const of = (kind: NotificationKind) => open.filter(n => n.kind === kind && (kind === "document" || n.account));
    const ids = (ns: readonly Notification[]) => ns.map(n => n.id);
    const each = (ns: readonly Notification[]): NeedName[] => ns.map(n => ({ text: `${named(n.account!)} ${describe(n, today, base)}`, href: accountHref(n.account!), ids: [n.id] }));
    const accountsOf = (ns: readonly Notification[]) => [...new Map(ns.map(n => [n.account!.id, n.account!])).values()];
    const found: Need[] = [];

    const short = of("shortfall");
    if (short.length > 0) {
        found.push({
            kind: "shortfall",
            text: `${accountsOf(short).length} 個帳戶可能不夠扣卡費：照最新對帳單的餘額算，之後轉進來的還看不到`,
            href: resolve("/accounts"),
            ids: ids(short),
            names: each(short),
        });
    }

    const documents = of("document");
    if (documents.length > 0) {
        const review = documents.filter(n => n.document?.status === "needs_review").length;
        const failed = documents.length - review;
        const parts = [review ? `${review} 份對不起來` : "", failed ? `${failed} 份解析失敗` : ""].filter(Boolean);
        found.push({ kind: "document", text: `${documents.length} 份文件要處理：${parts.join("、")}`, href: documentsQueryHref("show=todo"), ids: ids(documents), names: [] });
    }

    const gaps = of("gap");
    if (gaps.length > 0) {
        const gapped = accountsOf(gaps);
        found.push({
            kind: "gap",
            text: `${gapped.length} 個帳戶的餘額接不起來：多半是少了一期對帳單`,
            href: resolve("/accounts"),
            ids: ids(gaps),
            names: gapped.map(a => ({ text: named(a), href: accountHref(a), ids: ids(gaps.filter(n => n.account!.id === a.id)) })),
        });
    }

    const overdue = of("overdue");
    if (overdue.length > 0) {
        found.push({
            kind: "overdue",
            text: `${overdue.length} 個帳戶太久沒更新`,
            href: resolve("/accounts"),
            ids: ids(overdue),
            names: overdue.map(n => {
                const account = accounts.find(a => a.id === n.account!.id);
                const days = n.newest ? `（${daysAgo(daysSince(n.newest, today))}）` : "";
                return {
                    text: `${named(n.account!)}${days}`,
                    href: accountHref(n.account!),
                    ids: [n.id],
                    confirm: account ? { id: account.id, checkAgainOn: checkAgainDefault(account, today) } : undefined,
                };
            }),
        });
    }

    const lapsing = of("lapsing");
    if (lapsing.length > 0) {
        found.push({ kind: "lapsing", text: "點數快到期", href: resolve("/accounts"), ids: ids(lapsing), names: each(lapsing) });
    }
    return found;
}
