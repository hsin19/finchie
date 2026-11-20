// What the web app tells about itself through Grafana Faro (ADR 0024): its errors, Web Vitals and sessions,
// and a span for each request, which the backend's trace continues. Nothing loads unless the deployment
// names a collector, and nothing leaves the browser carrying what the owner searched for.

import type {
    Faro,
    TransportItem,
} from "@grafana/faro-web-sdk";
import { isHttpError } from "@sveltejs/kit";

let faro: Faro | undefined;
let starting: Promise<void> | undefined;
let route: string | null = null;

/** Starts telemetry once, if the deployment names a collector; only then is the SDK downloaded. */
export function startTelemetry(url: string | undefined, app: { version: string; dev: boolean; }): Promise<void> {
    if (!url) return Promise.resolve();
    starting ??= start(url, app);
    return starting;
}

async function start(url: string, app: { version: string; dev: boolean; }) {
    const [{ getWebInstrumentations, initializeFaro }, { TracingInstrumentation }] = await Promise.all([
        import("@grafana/faro-web-sdk"),
        import("@grafana/faro-web-tracing"),
    ]);
    faro = initializeFaro({
        url,
        app: {
            name: "finchie-web",
            namespace: "finchie",
            version: app.version,
            environment: app.dev ? "development" : "production",
        },
        // Requests to the app's own origin carry a traceparent, so the backend's span joins the browser's.
        instrumentations: [...getWebInstrumentations(), new TracingInstrumentation()],
        beforeSend: scrub,
    });
    showRoute();
}

/** Errors the app catches itself and so never reach window.onerror: SvelteKit's, and deferred loads'. */
export function reportError(error: unknown): void {
    faro?.api.pushError(asError(error));
}

// A failed load's error(), from need(), is SvelteKit's HttpError: no Error, and no message of its own.
function asError(error: unknown): Error {
    if (error instanceof Error) return error;
    if (isHttpError(error)) return new Error(`HTTP ${error.status}: ${error.body.message}`);
    return new Error(String(error));
}

/**
 * Pages are told apart by their route (`/statements/[id]`), as the backend names its spans, rather than by
 * every statement's own address.
 */
export function setRoute(id: string | null): void {
    route = id;
    showRoute();
}

function showRoute() {
    const url = withoutQuery(location.href);
    // Before the first navigation ends the route is not known yet: the page goes by its address alone.
    faro?.api.setPage(route ? { id: route, url } : { url });
}

// A URL's query and fragment hold what the owner searched for and filtered by (every filter is in the URL).
const QUERY = /((?:[a-z][a-z\d+.-]*:\/\/[^\s/?#"'<>]+)(?:\/[^\s?#"'<>]*)?|\/[^\s?#"'<>]*)[?#][^\s"'<>]*/gi;

/** Every URL in the text, absolute or a path, without its query and fragment. */
export function withoutQuery(text: string): string {
    return text.replace(QUERY, "$1");
}

// The browser is known by name, version and system, which tell a phone from a desktop; the full user agent
// string adds nothing to that but a fingerprint. A span's own query attribute goes with the URLs' queries.
const DROPPED_META = ["userAgent", "brands"];
const DROPPED_ATTRIBUTES = new Set(["url.query", "user_agent.original", "http.user_agent", "browser.brands"]);

/** Faro's beforeSend: what an item may carry out of the browser. */
export function scrub(item: TransportItem): TransportItem {
    const clean = scrubbed(item) as TransportItem;
    const browser = clean.meta.browser as Record<string, unknown> | undefined;
    for (const key of DROPPED_META) delete browser?.[key];
    return clean;
}

function scrubbed(value: unknown): unknown {
    if (typeof value === "string") return withoutQuery(value);
    if (Array.isArray(value)) return value.filter(entry => !isDroppedAttribute(entry)).map(scrubbed);
    if (value === null || typeof value !== "object") return value;
    return Object.fromEntries(Object.entries(value).map(([key, entry]) => [key, scrubbed(entry)]));
}

// Spans and their resources list attributes as OTLP's { key, value } pairs.
function isDroppedAttribute(entry: unknown): boolean {
    return entry !== null && typeof entry === "object" && "key" in entry && "value" in entry
        && typeof entry.key === "string" && DROPPED_ATTRIBUTES.has(entry.key);
}
