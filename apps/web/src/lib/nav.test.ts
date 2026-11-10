import {
    describe,
    expect,
    it,
} from "vitest";
import {
    carriedLinks,
    entityKey,
    monthHref,
} from "./nav";

const carried = (routeId: string, query: string) => carriedLinks(routeId, new URLSearchParams(query));

describe("carriedLinks", () => {
    it("takes a month's bill to the transactions on it", () => {
        expect(carried("/statements", "m=2026-10")).toEqual({ statements: "/statements?m=2026-10", transactions: "/transactions?closing=2026-10" });
    });

    it("takes the transactions of a closing month back to that month's bill", () => {
        expect(carried("/transactions", "closing=2026-10&kind=purchase")).toEqual({ statements: "/statements?m=2026-10", transactions: "/transactions?closing=2026-10" });
        expect(carried("/transactions", "m=all&closing=2026-10")).toEqual({ statements: "/statements?m=2026-10", transactions: "/transactions?m=all&closing=2026-10" });
    });

    it("keeps a month of transactions only where it means the same", () => {
        expect(carried("/transactions", "m=2026-10&q=uber")).toEqual({ statements: "/statements", transactions: "/transactions?m=2026-10" });
    });

    it("carries nothing from other pages, or without a month", () => {
        const none = { statements: "/statements", transactions: "/transactions" };
        expect(carried("/statements", "all")).toEqual(none);
        expect(carried("/statements/[id]", "line=4")).toEqual(none);
        expect(carried("/", "")).toEqual(none);
    });
});

describe("monthHref", () => {
    it("keeps the transactions page's other filters, from its first page", () => {
        expect(monthHref("/transactions", new URLSearchParams("m=2026-09&kind=purchase&page=3"), "2026-10")).toBe("/transactions?kind=purchase&m=2026-10");
    });

    it("opens the month's bill from anywhere else", () => {
        expect(monthHref("/accounts", new URLSearchParams("sort=bank"), "2026-10")).toBe("/statements?m=2026-10");
    });
});

describe("entityKey", () => {
    it("changes with the entity a page shows, not with its query", () => {
        expect(entityKey({ id: "7" })).not.toBe(entityKey({ id: "9" }));
        expect(entityKey({ id: "7" })).toBe(entityKey({ id: "7" }));
        expect(entityKey({})).toBe("");
        expect(entityKey({ b: "2", a: "1" })).toBe(entityKey({ a: "1", b: "2" }));
    });
});
