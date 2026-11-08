import {
    describe,
    expect,
    it,
} from "vitest";
import {
    pageCount,
    readFilters,
    searchPeriod,
    withChanges,
} from "./filters";

const filters = (query: string) => readFilters(new URLSearchParams(query));

describe("filters", () => {
    it("reads every filter from the URL, ignoring values it does not know", () => {
        expect(filters("m=2026-10&kind=purchase&kind=bogus&card=hsbc:4417&q=uber&overseas=1&hide=transfers&sort=largest&page=2")).toEqual({
            period: "2026-10",
            closing: undefined,
            kinds: ["purchase"],
            institution: "hsbc",
            card: "4417",
            account: "",
            q: "uber",
            overseas: true,
            hideTransfers: true,
            sort: "largest",
            page: 2,
        });
        expect(filters("m=2026-13&sort=random&page=-3")).toMatchObject({ period: undefined, sort: "newest", page: 1 });
        expect(filters("account=7&hide=bogus")).toMatchObject({ account: "7", hideTransfers: false });
        expect(filters("account=7x")).toMatchObject({ account: "" });
    });

    it("searches every month for a closing month unless a period is asked for", () => {
        expect(searchPeriod(filters(""), "2026-10")).toBe("2026-10");
        expect(searchPeriod(filters("closing=2026-09"), "2026-10")).toBe("all");
        expect(searchPeriod(filters("closing=2026-09&m=2026-08"), "2026-10")).toBe("2026-08");
        expect(searchPeriod(filters("m=all"), "2026-10")).toBe("all");
    });

    it("counts at least one page", () => {
        expect(pageCount(0)).toBe(1);
        expect(pageCount(50)).toBe(1);
        expect(pageCount(51)).toBe(2);
    });

    it("applies changes, removing empty ones, and starts again at page 1 unless paging", () => {
        const params = new URLSearchParams("m=2026-10&kind=purchase&q=uber&page=3");
        expect(withChanges(params, { kind: ["purchase", "refund"], q: "" }).toString()).toBe("m=2026-10&kind=purchase&kind=refund");
        expect(withChanges(params, { overseas: null }).toString()).toBe("m=2026-10&kind=purchase&q=uber");
        expect(withChanges(params, { page: "4" }).toString()).toBe("m=2026-10&kind=purchase&q=uber&page=4");
    });
});
