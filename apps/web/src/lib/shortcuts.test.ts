import {
    describe,
    expect,
    it,
} from "vitest";
import {
    helpGroups,
    keymap,
    keyName,
} from "./shortcuts";

describe("keymap", () => {
    it("gives a key one meaning per scope", () => {
        for (const [name, scope] of Object.entries(keymap)) {
            const keys = scope.shortcuts.flatMap(s => s.keys);
            expect(new Set(keys).size, name).toBe(keys.length);
        }
    });

    it("lists every scope in the help, keys as they are written in brackets", () => {
        const groups = helpGroups();
        expect(groups.map(g => g.title)).toEqual(Object.values(keymap).map(s => s.title));
        const transactions = groups.find(g => g.title.startsWith("TRANSACTIONS"))!;
        expect(transactions.lines).toContainEqual({ shown: "j / k", label: "上下移動" });
        expect(transactions.lines).toContainEqual({ shown: "esc", label: "取消選取" });
        expect(groups.find(g => g.title.startsWith("DOCUMENT ·"))!.lines).toContainEqual({ shown: "1–9", label: "開啟第幾個附件" });
        expect(groups[0].lines).toContainEqual({ shown: "⌘K", label: "指令" });
    });

    it("writes keys the way the hints on the pages do", () => {
        expect(keyName("Enter")).toBe("enter");
        expect(keyName("Escape")).toBe("esc");
        expect(keyName("j")).toBe("j");
    });
});
