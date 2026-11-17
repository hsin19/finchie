// @vitest-environment happy-dom
import {
    afterEach,
    describe,
    expect,
    it,
} from "vitest";
import {
    claims,
    listen,
} from "./keys";

/** Dispatches a keydown on `target` and reports whether a shortcut listener on window would take it. */
function press(target: EventTarget, key: string, init: KeyboardEventInit = {}): boolean | undefined {
    let claimed: boolean | undefined;
    const listener = (event: KeyboardEvent) => (claimed = claims(event));
    window.addEventListener("keydown", listener);
    target.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true, ...init }));
    window.removeEventListener("keydown", listener);
    return claimed;
}

function add<K extends keyof HTMLElementTagNameMap>(tag: K, setup: (el: HTMLElementTagNameMap[K]) => void = () => {}) {
    const el = document.createElement(tag);
    setup(el);
    document.body.append(el);
    return el;
}

afterEach(() => document.body.replaceChildren());

describe("claims", () => {
    it("takes plain keys on the page", () => {
        expect(press(document.body, "j")).toBe(true);
        expect(press(document.body, "Enter")).toBe(true);
    });

    it("leaves Enter and Space on a focused control to the browser, but not letters", () => {
        const button = add("button");
        const link = add("a", a => (a.href = "/statements"));
        expect(press(button, "Enter")).toBe(false);
        expect(press(button, " ")).toBe(false);
        expect(press(link, "Enter")).toBe(false);
        expect(press(button, "j")).toBe(true);
    });

    it("never takes keys while typing", () => {
        expect(press(add("input"), "j")).toBe(false);
        expect(press(add("input", i => (i.type = "search")), "j")).toBe(false);
        expect(press(add("input", i => (i.type = "number")), "n")).toBe(false);
        expect(press(add("textarea"), "Escape")).toBe(false);
        expect(press(add("select"), "t")).toBe(false);
    });

    it("keeps working on a focused checkbox, but leaves it Space and Enter", () => {
        const checkbox = add("input", i => (i.type = "checkbox"));
        expect(press(checkbox, "j")).toBe(true);
        expect(press(checkbox, "/")).toBe(true);
        expect(press(checkbox, " ")).toBe(false);
        expect(press(checkbox, "Enter")).toBe(false);
    });

    it("leaves keys that belong to an IME composition alone", () => {
        expect(press(document.body, "Enter", { isComposing: true })).toBe(false);
        // Safari: the Enter that confirms a candidate comes after compositionend, as keyCode 229.
        expect(press(document.body, "Enter", { keyCode: 229 })).toBe(false);
        expect(press(document.body, "j", { keyCode: 74 })).toBe(true);
    });

    it("leaves every key to an open dialog, Esc included", () => {
        const dialog = add("dialog", d => d.setAttribute("open", ""));
        const close = document.createElement("button");
        dialog.append(close);
        expect(press(close, "s")).toBe(false);
        expect(press(document.body, "Escape")).toBe(false);
        dialog.removeAttribute("open");
        expect(press(document.body, "s")).toBe(true);
    });

    it("ignores combinations with modifiers", () => {
        expect(press(document.body, "k", { metaKey: true })).toBe(false);
        expect(press(document.body, "t", { ctrlKey: true })).toBe(false);
    });
});

describe("listen", () => {
    function slash() {
        document.body.dispatchEvent(new KeyboardEvent("keydown", { key: "/", bubbles: true, cancelable: true }));
    }

    it("gives a page the key before the layout, even when the layout listened first", () => {
        const calls: string[] = [];
        const stopLayout = listen(window, { "/": () => calls.push("layout") }, { global: true });
        const stopPage = listen(window, { "/": () => calls.push("page") });
        slash();
        stopPage();
        stopLayout();
        expect(calls).toEqual(["page"]);
    });

    it("lets the layout have the page's keys while the page rests", () => {
        const calls: string[] = [];
        const stopLayout = listen(window, { "/": () => calls.push("layout") }, { global: true });
        const stopPage = listen(window, { "/": () => calls.push("page") }, { active: () => false });
        slash();
        stopPage();
        stopLayout();
        expect(calls).toEqual(["layout"]);
    });

    it("lets the layout have keys the page does not use", () => {
        const calls: string[] = [];
        const stopLayout = listen(window, { "/": () => calls.push("layout") }, { global: true });
        const stopPage = listen(window, { j: () => calls.push("page") });
        slash();
        stopPage();
        stopLayout();
        expect(calls).toEqual(["layout"]);
    });
});
