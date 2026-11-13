import type { Account } from "#lib/api/client.js";
import {
    describe,
    expect,
    it,
} from "vitest";
import { needs } from "./attention";

const account = (id: number, fields: Partial<Account> = {}): Account => ({
    id,
    institution: "esun",
    kind: "deposit",
    side: "asset",
    label: "臺幣活存",
    number: String(id),
    continuity: "checked",
    gaps: [],
    expiries: [],
    balances: [{ unit: "TWD", asOf: "2026-10-31", amount: "1" }],
    ...fields,
});
const today = "2026-11-13";

describe("needs", () => {
    it("is nothing at all while all is well", () => {
        expect(needs({ byState: [{ state: "parsed", count: 200 }, { state: "duplicate", count: 3 }], accounts: [account(1)], today })).toEqual([]);
    });

    it("puts documents first, then gaps, quiet sources, and lapsing points, each naming its accounts", () => {
        const found = needs({
            byState: [{ state: "needs_review", count: 9 }, { state: "failed", count: 41 }],
            accounts: [
                account(1, { gaps: [{ unit: "TWD", from: "2026-08-31", to: "2026-09-30", unseen: "-200" }] }),
                account(2, { balances: [{ unit: "TWD", asOf: "2026-08-04", amount: "1" }] }),
                account(3, { kind: "points", label: "點數", expiries: [{ unit: "小樹點", amount: "1200", by: "2026-12-31" }] }),
                // Lapses, but not soon.
                account(4, { kind: "points", label: "點數", expiries: [{ unit: "e point", amount: "50", by: "2027-06-30" }] }),
            ],
            today,
        });
        expect(found.map(n => [n.key, n.text, n.href, n.names.map(x => `${x.text} ${x.href}`)])).toEqual([
            ["documents", "50 份文件要處理：9 份對不起來、41 份解析失敗", "/documents?show=todo", []],
            ["gaps", "1 個帳戶的餘額接不起來：多半是少了一期對帳單", "/accounts", ["玉山 臺幣活存 /accounts/1"]],
            ["stale", "1 個帳戶太久沒更新", "/accounts", ["玉山 臺幣活存（101 天前） /accounts/2"]],
            ["lapsing", "點數快到期", "/accounts", ["玉山 點數 1,200 小樹點 12/31 前 /accounts/3"]],
        ]);
    });

    it("lets the owner confirm an overdue account is current, and then says nothing of it", () => {
        const quiet = account(2, { balances: [{ unit: "TWD", asOf: "2026-08-04", amount: "1" }] });
        expect(needs({ byState: [], accounts: [quiet], today })[0].names[0].confirm).toEqual({ id: 2, checkAgainOn: "2027-01-02" });
        expect(needs({ byState: [], accounts: [{ ...quiet, checkAgainOn: "2027-01-02" }], today })).toEqual([]);
    });

    it("reminds of points the rule says lapse soon, as worked out", () => {
        const points = account(5, { kind: "points", label: "點數", lots: [{ unit: "e point", by: "2026-12-31", amount: "28" }] });
        expect(needs({ byState: [], accounts: [points], today })[0].names[0].text).toBe("玉山 點數 28 e point 12/31 前（推算）");
    });

    it("says only the kind of document trouble there is", () => {
        expect(needs({ byState: [{ state: "needs_review", count: 2 }], accounts: [], today })[0].text).toBe("2 份文件要處理：2 份對不起來");
    });
});
