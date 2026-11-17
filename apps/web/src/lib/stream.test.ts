import {
    beforeEach,
    describe,
    expect,
    it,
    vi,
} from "vitest";

// Whether the app has drawn its first page is the module's own state: each test starts from a fresh one.
let stream: typeof import("./stream.svelte.js");
beforeEach(async () => {
    vi.resetModules();
    stream = await import("./stream.svelte.js");
});

describe("defer", () => {
    it("hands the first page's data over still coming, so its load returns at once", async () => {
        const { promise: data, resolve: arrive } = Promise.withResolvers<string>();
        const deferred = await stream.defer(data);
        expect(deferred).toHaveProperty("promise");
        arrive("in");
        await expect(stream.valueOf(deferred)).resolves.toBe("in");
    });

    it("waits for the data once the first page has drawn", async () => {
        stream.markDrawn();
        await expect(stream.defer(Promise.resolve("in"))).resolves.toEqual({ value: "in" });
    });

    it("leaves a failure for the page to show, not for the browser to report", async () => {
        const unhandled = vi.fn();
        process.on("unhandledRejection", unhandled);
        try {
            const deferred = await stream.defer(Promise.reject(new Error("down")));
            await new Promise(resolve => setTimeout(resolve, 0));
            expect(unhandled).not.toHaveBeenCalled();
            await expect(stream.valueOf(deferred)).rejects.toThrow("down");
        } finally {
            process.off("unhandledRejection", unhandled);
        }
    });
});

describe("current", () => {
    it("has the first page's data in hand for a page drawn after it came", async () => {
        const ledger = await stream.defer(Promise.resolve({ a: 1 }));
        await stream.valueOf(ledger);
        expect(stream.current(ledger)).toEqual({ value: { a: 1 } });
        expect(stream.join(ledger, { value: { b: 2 } })).toEqual({ value: { a: 1, b: 2 } });
    });

    it("leaves data still coming, or that failed, as it is", async () => {
        const { promise, reject } = Promise.withResolvers<object>();
        const deferred = await stream.defer(promise);
        expect(stream.current(deferred)).toBe(deferred);
        reject(new Error("down"));
        await expect(stream.valueOf(deferred)).rejects.toThrow("down");
        expect(stream.current(deferred)).toBe(deferred);
    });
});

describe("join", () => {
    it("is in hand when both halves are", () => {
        expect(stream.join({ value: { a: 1 } }, { value: { b: 2 } })).toEqual({ value: { a: 1, b: 2 } });
    });

    it("is still coming while either half is", async () => {
        const joined = stream.join({ value: { a: 1 } }, { promise: Promise.resolve({ b: 2 }) });
        expect(joined).toHaveProperty("promise");
        await expect(stream.valueOf(joined)).resolves.toEqual({ a: 1, b: 2 });
    });
});
