import {
    describe,
    expect,
    it,
} from "vitest";
import { pushText } from "./notices";

describe("pushText", () => {
    const today = "2026-11-13";

    it("titles a push by what it is about and says it as the app does", () => {
        const text = pushText({
            base: "TWD",
            notification: {
                id: 7,
                kind: "shortfall",
                raisedAt: "2026-11-13T01:00:00Z",
                account: { id: 1, institution: "esun", kind: "deposit", label: "臺幣活存" },
                shortfall: {
                    unit: "TWD",
                    asOf: "2026-10-31",
                    balance: "5000",
                    owed: "7000",
                    lacks: "2000",
                    debits: [{ statementId: 2, institution: "esun", dueDate: "2026-11-23", unit: "TWD", amount: "7000" }],
                },
            },
        }, today);
        expect(text).toEqual({ title: "扣款可能不夠", body: "玉山 臺幣活存 差 2,000（10/31 餘額 5,000，要扣 玉山 11/23 7,000）", tag: "notification-7" });
    });

    it("names the document, not an account, for a document", () => {
        const text = pushText({ base: "TWD", notification: { id: 8, kind: "document", raisedAt: "2026-11-13T01:00:00Z", document: { filename: "bill.eml", status: "failed" } } }, today);
        expect(text.body).toBe("bill.eml：解析失敗");
    });

    it("says a push without a notification is a test", () => {
        expect(pushText({ base: "TWD" }, today)).toMatchObject({ title: "Finchie", tag: "test" });
    });
});
