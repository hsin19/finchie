// @vitest-environment happy-dom
import {
    error,
    redirect,
} from "@sveltejs/kit";
import {
    beforeEach,
    describe,
    expect,
    it,
    vi,
} from "vitest";

const pushError = vi.fn();
vi.mock("@grafana/faro-web-sdk", () => ({
    getWebInstrumentations: () => [],
    initializeFaro: () => ({ api: { pushError, setPage: vi.fn() } }),
}));
vi.mock("@grafana/faro-web-tracing", () => ({ TracingInstrumentation: class {} }));

const app = { version: "1.0.0", dev: false };
const collector = "https://collector.test/collect/key";

// A fresh module each time: what it keeps (the SDK, errors waiting for it) is per page load.
async function telemetry() {
    vi.resetModules();
    return await import("./telemetry");
}

function thrown(fn: () => never): unknown {
    try {
        fn();
    } catch (caught) {
        return caught;
    }
}

beforeEach(() => pushError.mockClear());

describe("reportError", () => {
    it("keeps what fails before the SDK is in and sends it once it is", async () => {
        const { reportError, startTelemetry } = await telemetry();
        const early = new Error("the ledger did not load");
        reportError(early);
        expect(pushError).not.toHaveBeenCalled();
        await startTelemetry(collector, app);
        expect(pushError).toHaveBeenCalledExactlyOnceWith(early);
    });

    it("keeps no more than 20 while waiting", async () => {
        const { reportError, startTelemetry } = await telemetry();
        for (let i = 0; i < 25; i++) reportError(new Error(`failure ${i}`));
        await startTelemetry(collector, app);
        expect(pushError).toHaveBeenCalledTimes(20);
    });

    it("drops what it kept when the deployment names no collector", async () => {
        const { reportError, startTelemetry } = await telemetry();
        reportError(new Error("the ledger did not load"));
        await startTelemetry(undefined, app);
        await startTelemetry(collector, app);
        expect(pushError).not.toHaveBeenCalled();
    });

    it("sends a failure once, however many places catch it", async () => {
        const { reportError, startTelemetry } = await telemetry();
        await startTelemetry(collector, app);
        const failure = new Error("the ledger did not load");
        reportError(failure);
        reportError(failure);
        expect(pushError).toHaveBeenCalledTimes(1);
    });

    it("leaves out a load's redirect and the API's 4xx, and names a 5xx by its status", async () => {
        const { reportError, startTelemetry } = await telemetry();
        await startTelemetry(collector, app);
        reportError(thrown(() => redirect(307, "/transactions?page=3")));
        reportError(thrown(() => error(404, "statement 9 is not there")));
        reportError(thrown(() => error(500, "database unreachable")));
        expect(pushError).toHaveBeenCalledExactlyOnceWith(new Error("HTTP 500: database unreachable"));
    });
});
