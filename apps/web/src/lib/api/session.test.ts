import {
    describe,
    expect,
    it,
} from "vitest";
import {
    reloader,
    signedOut,
    withSession,
} from "./session.js";

/** What fetch gives for a redirect it was told not to follow: no status, no body. */
const redirected = { type: "opaqueredirect", status: 0, ok: false } as Response;

function fetching(response: Response): typeof fetch {
    return () => Promise.resolve(response);
}

/** Whether a promise is still pending once everything already queued has run. */
async function pending(promise: Promise<unknown>): Promise<boolean> {
    const nothing = Symbol();
    return await Promise.race([promise, new Promise(done => setTimeout(() => done(nothing)))]) === nothing;
}

describe("signedOut", () => {
    it("reads a redirect or a 401 as no session", () => {
        expect(signedOut(redirected)).toBe(true);
        expect(signedOut(new Response(null, { status: 401 }))).toBe(true);
    });

    it("takes any other answer as it is", () => {
        expect(signedOut(new Response(null, { status: 200 }))).toBe(false);
        expect(signedOut(new Response(null, { status: 500 }))).toBe(false);
    });
});

describe("withSession", () => {
    it("passes an answer through while signed in", async () => {
        const response = new Response("{}", { status: 200 });
        let signIns = 0;
        const fetch = withSession(fetching(response), () => (signIns++, true));
        expect(await fetch("/api/statements")).toBe(response);
        expect(signIns).toBe(0);
    });

    it("signs in again when a read finds the session gone, and never answers the page that is leaving", async () => {
        let signIns = 0;
        const fetch = withSession(fetching(redirected), () => (signIns++, true));
        expect(await pending(fetch(new Request("http://finchie.test/api/statements")))).toBe(true);
        expect(signIns).toBe(1);
    });

    it("answers a read 401 when signing in again did not help", async () => {
        const fetch = withSession(fetching(redirected), () => false);
        const response = await fetch("/api/statements");
        expect(response.status).toBe(401);
        expect(await response.json()).toEqual({ message: "登入過期了：重新整理會重新登入" });
    });

    it("leaves the page alone when a change finds the session gone, so a draft survives", async () => {
        let signIns = 0;
        const fetch = withSession(fetching(redirected), () => (signIns++, true));
        const response = await fetch(new Request("http://finchie.test/api/accounts/7", { method: "PATCH" }));
        expect(response.status).toBe(401);
        expect(signIns).toBe(0);
        expect((await fetch("/api/accounts", { method: "POST" })).status).toBe(401);
    });

    it("keeps what the backend said when it is the one refusing", async () => {
        const refused = new Response('{"message":"invalid access token"}', { status: 401 });
        expect(await withSession(fetching(refused), () => false)("/api/statements")).toBe(refused);
    });
});

describe("reloader", () => {
    function setup(start = 1_000_000) {
        const state = { now: start, reloads: 0, stored: new Map<string, string>() };
        const storage = { getItem: (key: string) => state.stored.get(key) ?? null, setItem: (key: string, value: string) => void state.stored.set(key, value) };
        const make = () => reloader({ now: () => state.now, storage: () => storage, reload: () => void state.reloads++ });
        return { state, make };
    }

    it("reloads once, however many requests ask while the page leaves", () => {
        const { state, make } = setup();
        const signIn = make();
        expect([signIn(), signIn(), signIn()]).toEqual([true, true, true]);
        expect(state.reloads).toBe(1);
    });

    it("does not reload again when the page it just reloaded is still signed out", () => {
        const { state, make } = setup();
        make()();
        state.now += 5_000;
        // A new page: the reload came back, and its first request is refused again.
        expect(make()()).toBe(false);
        expect(state.reloads).toBe(1);
    });

    it("reloads again for a session that ends later", () => {
        const { state, make } = setup();
        make()();
        state.now += 60_000;
        expect(make()()).toBe(true);
        expect(state.reloads).toBe(2);
    });

    it("takes a reload stamped in the future as old, not as a reason to wait for the clock", () => {
        const { state, make } = setup();
        make()();
        state.now -= 3_600_000;
        expect(make()()).toBe(true);
    });

    it("does not reload where it cannot remember having done so", () => {
        let reloads = 0;
        const signIn = reloader({
            storage: () => {
                throw new Error("storage is blocked");
            },
            reload: () => void reloads++,
        });
        expect(signIn()).toBe(false);
        expect(reloads).toBe(0);
    });
});
