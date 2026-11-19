import {
    describe,
    expect,
    it,
} from "vitest";
import {
    api,
    unwrap,
} from "./client.js";

describe("unwrap", () => {
    it("takes a response with no content as done", () => {
        expect(() => unwrap({ data: undefined, response: new Response(null, { status: 204, statusText: "No Content" }) })).not.toThrow();
    });

    it("gives the body of a response with one", () => {
        expect(unwrap({ data: { id: 7 }, response: new Response(null, { status: 201 }) })).toEqual({ id: 7 });
    });

    it("throws what the server said went wrong", () => {
        const response = new Response(null, { status: 400, statusText: "Bad Request" });
        expect(() => unwrap({ error: { message: "only points accounts are kept by hand" }, response })).toThrow("only points accounts are kept by hand");
        expect(() => unwrap({ error: undefined, response })).toThrow("Bad Request");
    });
});

// Never called: each line only has to type-check, or fail to where it says so. If one of the errors stops
// happening, its @ts-expect-error is unused and fails the check instead.
function requests() {
    const client = api();
    return [
        // @ts-expect-error a query parameter the API does not have (it is `query`), among ones it has
        client.GET("/api/transactions", { params: { query: { q: "rent", limit: 0 } } }),
        // @ts-expect-error a body field the API does not have
        client.POST("/api/payees", { body: { counterparty: "00442168037069", name: "房東", note: "rent" } }),
        // @ts-expect-error the path's own parameter is required
        client.GET("/api/accounts/{id}"),
        client.GET("/api/transactions", { params: { query: { query: "rent", limit: 0 } } }),
        client.GET("/api/settings", { signal: new AbortController().signal }),
    ];
}

async function responses() {
    const { data } = await api().GET("/api/settings");
    // @ts-expect-error the answer is typed as the API's Settings, not any
    const wrong: number | undefined = data?.baseCurrency;
    const right: string | undefined = data?.baseCurrency;
    return [wrong, right];
}

describe("api", () => {
    it("types each request and answer by its path, so a key the API does not know fails the type check", () => {
        expect(requests).toBeTypeOf("function");
        expect(responses).toBeTypeOf("function");
    });
});
