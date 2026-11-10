import {
    describe,
    expect,
    it,
} from "vitest";
import type { Sent } from "./api/upload";
import { Uploads } from "./upload.svelte";

const file = (name: string, size = 10) => new File([new Uint8Array(size)], name);

function uploads(answers: Record<string, Sent>, maxBytes = 100) {
    const order: string[] = [];
    let reloads = 0;
    const u = new Uploads({
        maxBytes,
        send: async (f, progress) => {
            order.push(f.name);
            progress(0.5);
            return answers[f.name];
        },
        done: async () => reloads++,
    });
    return { u, order, reloads: () => reloads };
}

describe("Uploads", () => {
    it("sends files in the order given, listing the newest first", async () => {
        const { u, order, reloads } = uploads({ "a.eml": { documentId: 1, same: false }, "b.eml": { documentId: 2, same: true } });
        await u.add([file("a.eml"), file("b.eml")]);
        expect(order).toEqual(["a.eml", "b.eml"]);
        expect(u.entries.map(e => [e.name, e.documentId, e.same, e.sent])).toEqual([["b.eml", 2, true, 1], ["a.eml", 1, false, 1]]);
        expect(reloads()).toBe(2);
        expect(u.of(1)?.name).toBe("a.eml");
    });

    it("keeps why a file was refused", async () => {
        const { u } = uploads({ "a.eml": { error: "not an e-mail" } });
        await u.add([file("a.eml")]);
        expect(u.entries[0].error).toBe("not an e-mail");
    });

    it("refuses a file over the backend's limit without sending it", async () => {
        const { u, order } = uploads({}, 1024);
        await u.add([file("big.eml", 2048)]);
        expect(order).toEqual([]);
        expect(u.entries[0].error).toBe("超過單檔上限 1 KB");
    });
});
