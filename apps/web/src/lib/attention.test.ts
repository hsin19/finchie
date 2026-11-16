import type {
    Account,
    Notification,
    NotificationKind,
} from "#lib/api/client.js";
import {
    describe,
    expect,
    it,
} from "vitest";
import {
    describe as describeNotification,
    needs,
} from "./attention";

const today = "2026-11-13";
const base = "TWD";
const savings = { id: 1, institution: "esun", kind: "deposit" as const, label: "臺幣活存" };
const points = { id: 3, institution: "esun", kind: "points" as const, label: "點數" };
const note = (id: number, kind: NotificationKind, rest: Partial<Notification> = {}): Notification => ({ id, kind, raisedAt: "2026-11-13T01:00:00Z", ...rest });
const live = (id: number, freshFor: number): Account => ({
    id,
    institution: "esun",
    kind: "securities",
    side: "asset",
    number: String(id),
    balances: [],
    continuity: "checked",
    gaps: [],
    expiries: [],
    overdue: true,
    excused: false,
    freshFor,
});

const shortfall = note(10, "shortfall", {
    account: savings,
    shortfall: {
        unit: "TWD",
        asOf: "2026-10-31",
        balance: "5000",
        owed: "7000",
        lacks: "2000",
        debits: [
            { statementId: 1, institution: "hsbc", dueDate: "2026-11-09", unit: "TWD", amount: "3000" },
            { statementId: 2, institution: "esun", dueDate: "2026-11-23", unit: "TWD", amount: "4000" },
        ],
    },
});

describe("needs", () => {
    it("is nothing at all while all is well, nor for what the owner handled or what resolved by itself", () => {
        expect(needs({ notifications: [], accounts: [], today, base })).toEqual([]);
        const done = [{ ...shortfall, handledAt: "2026-11-13T02:00:00Z" }, { ...shortfall, id: 11, resolvedAt: "2026-11-13T03:00:00Z" }];
        expect(needs({ notifications: done, accounts: [], today, base })).toEqual([]);
    });

    it("puts what may not be paid first, then documents, gaps, quiet sources, and lapsing points, each naming what it is about", () => {
        const found = needs({
            notifications: [
                note(5, "lapsing", { account: points, lapsing: { unit: "小樹點", amount: "1200", by: "2026-12-31", estimated: false, soon: true } }),
                note(4, "overdue", { account: { ...savings, id: 2 }, newest: "2026-08-04" }),
                note(3, "gap", { account: savings, gap: { unit: "TWD", from: "2026-08-31", to: "2026-09-30", unseen: "-200" } }),
                note(6, "gap", { account: savings, gap: { unit: "USD", from: "2026-08-31", to: "2026-09-30", unseen: "5" } }),
                note(2, "document", { document: { id: 7, filename: "bill.eml", status: "failed", failure: "locked" } }),
                note(8, "document", { document: { id: 8, filename: "card.eml", status: "needs_review" } }),
                shortfall,
            ],
            accounts: [],
            today,
            base,
        });
        expect(found.map(n => [n.kind, n.text, n.href, n.ids, n.names.map(x => `${x.text} ${x.href} ${x.ids.join()}`)])).toEqual([
            ["shortfall", "1 個帳戶可能不夠扣卡費：照最新對帳單的餘額算，之後轉進來的還看不到", "/accounts", [10], [
                "玉山 臺幣活存 差 2,000（10/31 餘額 5,000，要扣 滙豐 11/09 3,000 + 玉山 11/23 4,000） /accounts/1 10",
            ]],
            ["document", "2 份文件要處理：1 份對不起來、1 份解析失敗", "/documents?show=todo", [2, 8], []],
            ["gap", "1 個帳戶的餘額接不起來：多半是少了一期對帳單", "/accounts", [3, 6], ["玉山 臺幣活存 /accounts/1 3,6"]],
            ["overdue", "1 個帳戶太久沒更新", "/accounts", [4], ["玉山 臺幣活存（101 天前） /accounts/2 4"]],
            ["lapsing", "點數快到期", "/accounts", [5], ["玉山 點數 1,200 小樹點 12/31 前 /accounts/3 5"]],
        ]);
    });

    it("lets the owner confirm an overdue account is current, as long from today as its source may take", () => {
        const overdue = note(4, "overdue", { account: { ...savings, id: 2 }, newest: "2026-08-04" });
        expect(needs({ notifications: [overdue], accounts: [live(2, 50)], today, base })[0].names[0].confirm).toEqual({ id: 2, checkAgainOn: "2027-01-02" });
        expect(needs({ notifications: [overdue], accounts: [], today, base })[0].names[0].confirm).toBeUndefined();
    });

    it("says only the kind of document trouble there is", () => {
        const review = note(1, "document", { document: { id: 7, filename: "bill.eml", status: "needs_review" } });
        expect(needs({ notifications: [review], accounts: [], today, base })[0].text).toBe("1 份文件要處理：1 份對不起來");
    });
});

describe("describe", () => {
    it("says what each notification is, without its account", () => {
        expect(describeNotification(note(1, "document", { document: { id: 7, filename: "bill.eml", status: "failed" } }), today, base)).toBe("bill.eml：解析失敗");
        expect(describeNotification(note(2, "gap", { gap: { unit: "TWD", from: "2026-08-31", to: "2026-09-30", unseen: "-200" } }), today, base))
            .toBe("TWD 08/31 到 09/30 之間有 -200 沒有交易說明");
        expect(describeNotification(note(5, "gap", { gap: { unit: "USD", from: "2022-01-31", to: "2025-10-31", unseen: "5" } }), today, base))
            .toBe("USD 2022/01/31 到 2025/10/31 之間有 +5 沒有交易說明");
        expect(describeNotification(note(3, "overdue", { newest: "2026-08-04" }), today, base)).toBe("101 天前的餘額");
        expect(describeNotification(note(4, "lapsing", { lapsing: { unit: "e point", amount: "28", by: "2026-12-31", estimated: true, soon: true } }), today, base))
            .toBe("28 e point 12/31 前（推算）");
    });
});
