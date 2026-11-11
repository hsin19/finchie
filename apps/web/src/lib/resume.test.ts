import {
    describe,
    expect,
    it,
} from "vitest";
import { absence } from "./resume.js";

describe("absence", () => {
    function setup(hidden = false) {
        const clock = { now: 0 };
        return { clock, away: absence(5 * 60_000, { hidden, now: () => clock.now }) };
    }

    it("says the app is back once it was out of sight long enough", () => {
        const { clock, away } = setup();
        expect(away.change(true)).toBe(false);
        clock.now += 5 * 60_000;
        expect(away.change(false)).toBe(true);
    });

    it("lets a glance at another app pass", () => {
        const { clock, away } = setup();
        away.change(true);
        clock.now += 4 * 60_000;
        expect(away.change(false)).toBe(false);
    });

    it("counts from when the app went out of sight, not from the last time it was told so", () => {
        const { clock, away } = setup();
        away.change(true);
        clock.now += 4 * 60_000;
        away.change(true);
        clock.now += 60_000;
        expect(away.change(false)).toBe(true);
    });

    it("starts over after each return", () => {
        const { clock, away } = setup();
        away.change(true);
        clock.now += 10 * 60_000;
        away.change(false);
        expect(away.change(false)).toBe(false);
        away.change(true);
        clock.now += 60_000;
        expect(away.change(false)).toBe(false);
    });

    it("counts a page opened out of sight as away since it opened", () => {
        const { clock, away } = setup(true);
        clock.now += 5 * 60_000;
        expect(away.change(false)).toBe(true);
    });
});
