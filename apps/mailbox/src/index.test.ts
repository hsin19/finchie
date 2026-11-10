import {
    afterEach,
    describe,
    expect,
    it,
    vi,
} from "vitest";
import worker, { type Env } from "./index";

const env: Env = {
    FINCHIE_URL: "https://finchie.example.com",
    OWNER_EMAIL: "owner@gmail.com",
    ACCESS_CLIENT_ID: "client-id.access",
    ACCESS_CLIENT_SECRET: "client-secret",
};

const RAW = "From: Example Bank <statements@bank.example>\r\nSubject: Statement\r\n\r\nbody";

function message(from: string, headers: HeadersInit) {
    const sent = { rejected: undefined as string | undefined, forwardedTo: undefined as string | undefined };
    const msg = {
        from,
        to: "finchie@example.com",
        headers: new Headers(headers),
        raw: new Response(RAW).body!,
        rawSize: RAW.length,
        setReject(reason: string) {
            sent.rejected = reason;
        },
        async forward(to: string) {
            sent.forwardedTo = to;
        },
        async reply() {},
    };
    return { msg: msg as unknown as ForwardableEmailMessage, sent };
}

async function receive(msg: ForwardableEmailMessage, withEnv: Env = env) {
    await worker.email(msg, withEnv);
}

afterEach(() => {
    vi.unstubAllGlobals();
});

describe("mailbox", () => {
    it("uploads mail forwarded by the owner, as an .eml with the service token", async () => {
        const fetch = vi.fn(async (_url: URL, _init: RequestInit) => new Response("{}", { status: 201 }));
        vi.stubGlobal("fetch", fetch);
        const { msg, sent } = message("owner+caf_=finchie=example.com@gmail.com", { subject: "Example Bank statement" });

        await receive(msg);

        expect(sent.rejected).toBeUndefined();
        const [url, init] = fetch.mock.calls[0]!;
        expect(String(url)).toBe("https://finchie.example.com/api/documents");
        expect(init.method).toBe("POST");
        expect(init.headers).toMatchObject({ "CF-Access-Client-Id": "client-id.access", "CF-Access-Client-Secret": "client-secret" });
        // Access answers a rejected service token with a redirect to its sign-in page, which a followed
        // redirect would turn into a 200 and the mail into one taken.
        expect(init.redirect).toBe("manual");
        const file = (init.body as FormData).get("file") as File;
        expect(file.name).toBe("Example Bank statement.eml");
        expect(await file.text()).toBe(RAW);
    });

    it("bounces the mail when Finchie does not take it, so the owner knows", async () => {
        vi.stubGlobal("fetch", vi.fn(async () => new Response("", { status: 302 })));
        const { msg, sent } = message("owner@gmail.com", { subject: "x" });

        await receive(msg);

        expect(sent.rejected).toMatch(/HTTP 302.*upload it by hand/);
    });

    it("bounces the mail when Finchie cannot be reached", async () => {
        vi.stubGlobal(
            "fetch",
            vi.fn(async () => {
                throw new Error("connection refused");
            }),
        );
        const { msg, sent } = message("owner@gmail.com", { subject: "x" });

        await receive(msg);

        expect(sent.rejected).toMatch(/could not reach Finchie/);
    });

    it("rejects mail nobody forwarded, without uploading it", async () => {
        const fetch = vi.fn();
        vi.stubGlobal("fetch", fetch);
        const { msg, sent } = message("someone@spam.example", { subject: "hello" });

        await receive(msg);

        expect(sent.rejected).toMatch(/only takes mail forwarded by its owner/);
        expect(fetch).not.toHaveBeenCalled();
    });

    it("passes Gmail's forwarding confirmation to the owner", async () => {
        const fetch = vi.fn();
        vi.stubGlobal("fetch", fetch);
        const { msg, sent } = message("forwarding-noreply@google.com", { from: "Gmail Team <forwarding-noreply@google.com>" });

        await receive(msg);

        expect(sent.forwardedTo).toBe("owner@gmail.com");
        expect(sent.rejected).toBeUndefined();
        expect(fetch).not.toHaveBeenCalled();
    });

    it("rejects other mail from Google, such as a stranger's Drive share", async () => {
        const fetch = vi.fn();
        vi.stubGlobal("fetch", fetch);
        const { msg, sent } = message("drive-shares-dm-noreply@google.com", { from: '"Stranger (via Google Drive)" <drive-shares-dm-noreply@google.com>' });

        await receive(msg);

        expect(sent.forwardedTo).toBeUndefined();
        expect(sent.rejected).toMatch(/only takes mail forwarded by its owner/);
        expect(fetch).not.toHaveBeenCalled();
    });

    it("rejects mail with a second From header naming Gmail's forwarding sender", async () => {
        const { msg, sent } = message("someone@spam.example", [["from", "someone@spam.example"], ["from", "forwarding-noreply@google.com"]]);

        await receive(msg);

        expect(sent.forwardedTo).toBeUndefined();
        expect(sent.rejected).toMatch(/only takes mail forwarded by its owner/);
    });

    it("uploads mail whose subject does not decode", async () => {
        const fetch = vi.fn(async (_url: URL, _init: RequestInit) => new Response("{}", { status: 201 }));
        vi.stubGlobal("fetch", fetch);
        const { msg, sent } = message("owner@gmail.com", { subject: "=?UTF-8?B?a?=" });

        await receive(msg);

        expect(sent.rejected).toBeUndefined();
        expect(fetch).toHaveBeenCalledOnce();
    });

    it("bounces the mail when something unexpected fails", async () => {
        vi.stubGlobal("fetch", vi.fn());
        const { msg, sent } = message("forwarding-noreply@google.com", { from: "forwarding-noreply@google.com" });
        msg.forward = async () => {
            throw new Error("destination not verified");
        };

        await receive(msg);

        expect(sent.rejected).toMatch(/failed to handle this mail/);
    });

    it("rejects everything until its settings are in place", async () => {
        const { msg, sent } = message("owner@gmail.com", { subject: "x" });

        await receive(msg, { ...env, ACCESS_CLIENT_SECRET: undefined });

        expect(sent.rejected).toMatch(/not set up/);
    });
});
