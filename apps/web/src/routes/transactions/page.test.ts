import {
    afterEach,
    beforeEach,
    describe,
    expect,
    it,
    vi,
} from "vitest";
import { load } from "./+page";

type Event = Parameters<typeof load>[0];

// The API client asks for relative URLs, which only a browser resolves.
beforeEach(() => {
    vi.stubGlobal(
        "Request",
        class extends Request {
            constructor(input: string, init?: RequestInit) {
                super(new URL(input, "https://finchie.test"), init);
            }
        },
    );
});
afterEach(() => vi.unstubAllGlobals());

/** Runs the load for `query` against an API whose search matches `total` rows. */
function run(query: string, total: number) {
    const requested: URL[] = [];
    const fetch = async (request: Request) => {
        requested.push(new URL(request.url));
        const body = { items: [], total, sum: "0", flows: [], byKind: [], byCard: [], byAccount: [] };
        return new Response(JSON.stringify(body), { headers: { "content-type": "application/json" } });
    };
    const event = { url: new URL(`https://finchie.test/transactions${query}`), fetch, depends: () => {}, parent: async () => ({ latestMonth: "2026-10" }) };
    return { result: load(event as unknown as Event), requested };
}

describe("transactions load", () => {
    it("asks for the filters and the page in the URL, in the latest month by default", async () => {
        const { result, requested } = run("?kind=purchase&account=7&hide=transfers&page=2", 79);
        await expect(result).resolves.toMatchObject({ filters: { kinds: ["purchase"], page: 2 } });
        expect(Object.fromEntries(requested[0].searchParams)).toMatchObject({
            from: "2026-10-01",
            to: "2026-10-31",
            kind: "purchase",
            account: "7",
            hideTransfers: "true",
            offset: "50",
        });
    });

    it("sends a page past the end to the last page", async () => {
        await expect(run("?kind=purchase&page=9", 79).result).rejects.toMatchObject({ status: 307, location: "/transactions?kind=purchase&page=2" });
        await expect(run("?page=3", 0).result).rejects.toMatchObject({ status: 307, location: "/transactions" });
    });
});
