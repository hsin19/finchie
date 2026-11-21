import type { TransportItem } from "@grafana/faro-web-sdk";
import {
    describe,
    expect,
    it,
} from "vitest";
import {
    scrub,
    startTelemetry,
    withoutQuery,
} from "./telemetry";

describe("withoutQuery", () => {
    it("drops the query and fragment of every URL in the text", () => {
        expect(withoutQuery("https://finchie.test/transactions?q=rent&m=2026-10#top")).toBe("https://finchie.test/transactions");
        expect(withoutQuery("https://finchie.test?q=rent")).toBe("https://finchie.test");
        expect(withoutQuery("/api/transactions?query=rent&limit=1")).toBe("/api/transactions");
        expect(withoutQuery("/statements/5#line-3")).toBe("/statements/5");
        expect(withoutQuery("GET /api/documents?query=bill failed: 500, then /api/settings?x ok"))
            .toBe("GET /api/documents failed: 500, then /api/settings ok");
    });

    it("leaves what holds no query as it is", () => {
        expect(withoutQuery("https://finchie.test/_app/immutable/chunks/a.js:1:234")).toBe(
            "https://finchie.test/_app/immutable/chunks/a.js:1:234",
        );
        expect(withoutQuery("/statements/[id]")).toBe("/statements/[id]");
        expect(withoutQuery("Is the total right?")).toBe("Is the total right?");
    });
});

describe("scrub", () => {
    const item = (payload: unknown, browser: Record<string, unknown> = {}) =>
        ({
            type: "trace",
            payload,
            meta: {
                page: { url: "https://finchie.test/transactions?q=rent#x", id: "/transactions" },
                browser: {
                    name: "Safari",
                    version: "26.1",
                    os: "iOS 26.1",
                    mobile: true,
                    userAgent: "Mozilla/5.0 (iPhone…)",
                    brands: [{ brand: "Safari", version: "26" }],
                    ...browser,
                },
            },
        }) as unknown as TransportItem;

    const span = (attributes: { key: string; value: { stringValue: string; }; }[]) => ({
        resourceSpans: [{
            resource: { attributes: [{ key: "user_agent.original", value: { stringValue: "Mozilla/5.0" } }] },
            scopeSpans: [{ spans: [{ name: "HTTP GET", attributes }] }],
        }],
    });

    it("sends the page without what was searched for", () => {
        expect(scrub(item({})).meta.page).toEqual({ url: "https://finchie.test/transactions", id: "/transactions" });
    });

    it("keeps the browser's name, version and system but not its user agent string", () => {
        expect(scrub(item({})).meta.browser).toEqual({ name: "Safari", version: "26.1", os: "iOS 26.1", mobile: true });
    });

    it("strips a span's URL and drops its query, fragment and user agent attributes", () => {
        const payload = scrub(item(span([
            { key: "http.url", value: { stringValue: "https://finchie.test/api/transactions?query=rent" } },
            { key: "url.full", value: { stringValue: "https://finchie.test/api/transactions?query=rent" } },
            { key: "url.query", value: { stringValue: "query=rent" } },
            { key: "url.fragment", value: { stringValue: "line-3" } },
            { key: "http.user_agent", value: { stringValue: "Mozilla/5.0" } },
            { key: "http.method", value: { stringValue: "GET" } },
        ]))).payload;
        expect(payload).toEqual({
            resourceSpans: [{
                resource: { attributes: [] },
                scopeSpans: [{
                    spans: [{
                        name: "HTTP GET",
                        attributes: [
                            { key: "http.url", value: { stringValue: "https://finchie.test/api/transactions" } },
                            { key: "url.full", value: { stringValue: "https://finchie.test/api/transactions" } },
                            { key: "http.method", value: { stringValue: "GET" } },
                        ],
                    }],
                }],
            }],
        });
    });

    it("strips URLs inside logged and thrown messages", () => {
        const payload = scrub(item({
            type: "Error",
            value: "fetch /api/documents?query=bill failed",
            stacktrace: { frames: [{ filename: "https://finchie.test/_app/immutable/a.js", lineno: 1 }] },
        })).payload;
        expect(payload).toEqual({
            type: "Error",
            value: "fetch /api/documents failed",
            stacktrace: { frames: [{ filename: "https://finchie.test/_app/immutable/a.js", lineno: 1 }] },
        });
    });
});

describe("startTelemetry", () => {
    it("loads nothing when the deployment names no collector", async () => {
        await expect(startTelemetry(undefined, { version: "1.0.0", dev: false })).resolves.toBeUndefined();
        await expect(startTelemetry("", { version: "1.0.0", dev: false })).resolves.toBeUndefined();
    });
});
