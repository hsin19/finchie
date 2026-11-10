import {
    describe,
    expect,
    it,
} from "vitest";
import {
    filterParams,
    type Filters,
    pageCount,
    pickerChanges,
    pickerOptions,
    pickerValue,
    readFilters,
    searchPeriod,
    transactionsHref,
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

describe("links to the transactions page", () => {
    it("reads back as the filters it was built from", () => {
        const all: Filters = {
            period: "2026-10",
            closing: "2026-09",
            kinds: ["purchase", "refund"],
            institution: "hsbc",
            card: "4417",
            account: "",
            q: "uber eats",
            overseas: true,
            hideTransfers: true,
            sort: "largest",
            page: 3,
        };
        expect(readFilters(filterParams(all))).toEqual(all);
        const defaults = readFilters(new URLSearchParams());
        expect(filterParams(defaults).toString()).toBe("");
        expect(readFilters(filterParams({ institution: "esun" }))).toMatchObject({ institution: "esun", card: "" });
    });

    it("links a closing month's lines of one card", () => {
        expect(transactionsHref({ closing: "2026-10", kinds: ["purchase"], institution: "hsbc", card: "4417" })).toBe("/transactions?closing=2026-10&kind=purchase&card=hsbc%3A4417");
        expect(transactionsHref()).toBe("/transactions");
    });
});

describe("account picker", () => {
    it("picks a bank, a card, or a deposit account, and shows the pick", () => {
        expect(pickerChanges("hsbc:4417")).toEqual({ account: null, card: "hsbc:4417" });
        expect(pickerChanges("account:7")).toEqual({ account: "7", card: null });
        expect(pickerChanges("")).toEqual({ account: null, card: null });
        expect(pickerValue(filters("card=hsbc:4417"))).toBe("hsbc:4417");
        expect(pickerValue(filters("account=7"))).toBe("account:7");
        expect(pickerValue(filters(""))).toBe("");
    });

    it("lists each bank with its cards and accounts, every option reading back as its pick", () => {
        const options = pickerOptions({
            byCard: [{ institution: "hsbc", cardLast4: "4417", count: 3, amount: "0" }, { institution: "hsbc", count: 1, amount: "0" }],
            byAccount: [{ institution: "esun", accountId: 7, accountName: "臺幣活存", accountNumber: "··1234", count: 2 }],
        });
        expect(options.map(o => o.value)).toEqual(["esun", "account:7", "hsbc", "hsbc:4417"]);
        expect(options[0].label).toBe("ESUN (all) · 2");
        for (const option of options) {
            const params = new URLSearchParams();
            for (const [key, value] of Object.entries(pickerChanges(option.value))) if (value) params.set(key, String(value));
            expect(pickerValue(readFilters(params))).toBe(option.value);
        }
    });
});
