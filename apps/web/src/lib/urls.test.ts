import {
    describe,
    expect,
    it,
} from "vitest";
import {
    readLine,
    readStatementsView,
    statementHref,
    statementsHref,
} from "./urls";

const read = (href: string) => new URL(href, "https://finchie.test").searchParams;

describe("statements URLs", () => {
    it("reads back as the view it links to", () => {
        expect(readStatementsView(read(statementsHref("2026-10")))).toEqual({ all: false, month: "2026-10" });
        expect(readStatementsView(read(statementsHref("all")))).toEqual({ all: true });
        expect(readStatementsView(read(statementsHref()))).toEqual({ all: false, month: undefined });
        expect(readStatementsView(read("/statements?m=2026-13"))).toEqual({ all: false, month: undefined });
    });

    it("links a statement's line, and reads only a line id back", () => {
        expect(statementHref(5, 812)).toBe("/statements/5?line=812");
        expect(statementHref(5)).toBe("/statements/5");
        expect(readLine(read(statementHref(5, 812)))).toBe(812);
        expect(readLine(read("/statements/5?line=x"))).toBeUndefined();
        expect(readLine(read("/statements/5"))).toBeUndefined();
    });
});
