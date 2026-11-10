// @vitest-environment happy-dom
import {
    afterEach,
    beforeEach,
    describe,
    expect,
    it,
    vi,
} from "vitest";
import {
    focusSearch,
    Search,
    searchTarget,
} from "./search.svelte";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

/** A box on a page whose URL has ?q=`url`; sending moves the query asked for, as QueryRequests does. */
function box(url = "") {
    const sent: string[] = [];
    let asked = url;
    const search = new Search(url, {
        asked: () => asked,
        send: q => {
            sent.push(q);
            asked = q;
        },
    });
    return { search, sent, settle: (q: string) => ((asked = q), search.landed(q)) };
}

function type(search: Search, text: string) {
    search.value = text;
    search.typed();
}

describe("Search", () => {
    it("sends a moment after the last key, once", () => {
        const { search, sent } = box();
        type(search, "u");
        vi.advanceTimersByTime(100);
        type(search, "ub");
        vi.advanceTimersByTime(249);
        expect(sent).toEqual([]);
        vi.advanceTimersByTime(1);
        expect(sent).toEqual(["ub"]);
    });

    it("does not send what was asked for already, nor spaces around it", () => {
        const { search, sent } = box("uber");
        type(search, " uber ");
        vi.advanceTimersByTime(250);
        expect(sent).toEqual([]);
    });

    it("keeps what was typed after a search it sent lands", () => {
        const { search, sent, settle } = box();
        type(search, "ub");
        vi.advanceTimersByTime(250);
        type(search, "uber");
        settle(sent[0]);
        expect(search.value).toBe("uber");
        vi.advanceTimersByTime(250);
        expect(sent).toEqual(["ub", "uber"]);
    });

    it("follows the URL when another navigation lands, dropping what was not sent", () => {
        const { search, sent, settle } = box("uber");
        type(search, "ube");
        settle("rent");
        expect(search.value).toBe("rent");
        vi.advanceTimersByTime(250);
        expect(sent).toEqual([]);
    });

    it("follows the URL after its own search landed, even when the URL did not change", () => {
        // ?q=ab; abc is sent, then ab again before abc loads: the URL never leaves ab.
        const { search, sent, settle } = box("ab");
        type(search, "abc");
        vi.advanceTimersByTime(250);
        type(search, "ab");
        vi.advanceTimersByTime(250);
        expect(sent).toEqual(["abc", "ab"]);
        settle("ab");
        // Later Back brings ab again from elsewhere: the box follows.
        settle("x");
        expect(search.value).toBe("x");
        settle("ab");
        expect(search.value).toBe("ab");
    });

    it("clears at once", () => {
        const { search, sent } = box("uber");
        search.clear();
        expect(search.value).toBe("");
        expect(sent).toEqual([""]);
    });

    it("drops a search not sent when the page goes", () => {
        const { search, sent } = box();
        type(search, "ub");
        search.dispose();
        vi.advanceTimersByTime(1000);
        expect(sent).toEqual([]);
    });
});

describe("focusSearch", () => {
    it("focuses the page's search box while it is mounted", () => {
        const input = document.createElement("input");
        document.body.append(input);
        const detach = searchTarget(input);
        expect(focusSearch()).toBe(true);
        expect(document.activeElement).toBe(input);
        detach();
        input.remove();
        expect(focusSearch()).toBe(false);
    });
});
