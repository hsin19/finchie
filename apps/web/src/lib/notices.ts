// What a notification says, in the app's words: the overview, /notifications, and the service worker
// that shows a push all say it alike. Nothing here may need the app's router, which a service worker has
// none of.

import type {
    Notification,
    NotificationKind,
    Shortfall,
} from "#lib/api/types.js";
import { yearlyDate } from "#lib/dates.js";
import { institution } from "#lib/institutions.js";
import { formatAmount } from "#lib/money.js";
import {
    accountName,
    daysAgo,
} from "#lib/names.js";

/** The account a notification is about, as it is named now. */
export type NotifiedAccount = NonNullable<Notification["account"]>;

/** What each kind of notification is about, in a few words. */
export const noticeLabels: Record<NotificationKind, string> = {
    shortfall: "扣款可能不夠",
    document: "文件",
    gap: "餘額接不起來",
    overdue: "太久沒更新",
    lapsing: "點數快到期",
};

export const named = (a: Pick<NotifiedAccount, "institution" | "kind" | "label" | "name">) => `${institution(a.institution).name} ${accountName(a)}`.trim();

export const daysSince = (day: string, today: string) => Math.round((Date.parse(today) - Date.parse(day)) / 86_400_000);

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

/** What a push carries (the backend's notify.Message): a notification just raised, or, without one, a test. */
export interface PushMessage {
    base: string;
    notification?: Notification;
}

/** A push as the phone shows it: what it is about as its title, what it says as its body. */
export function pushText({ base, notification: n }: PushMessage, today: string): { title: string; body: string; tag: string; } {
    if (!n) return { title: "Finchie", body: "推播可以用了：之後有要處理的事，會直接通知這台裝置", tag: "test" };
    const about = n.account ? `${named(n.account)} ` : "";
    return { title: noticeLabels[n.kind], body: `${about}${describe(n, today, base)}`, tag: `notification-${n.id}` };
}
