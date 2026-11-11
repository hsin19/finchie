import {
    afterEach,
    beforeEach,
    describe,
    expect,
    it,
    vi,
} from "vitest";
import { poll } from "./poll";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

/** A poll whose looks answer from `answers`, one each, then the last again; an Error rejects. */
function polling(waitingFor: string, answers: (string | Error)[], options: Parameters<typeof poll>[1] = {}) {
    const start = Date.now();
    /** Milliseconds from the start to each look. */
    const looks: number[] = [];
    const p = poll(async () => {
        looks.push(Date.now() - start);
        const answer = answers.length > 1 ? answers.shift()! : answers[0];
        if (answer instanceof Error) throw answer;
        return answer;
    }, options);
    p.update(waitingFor);
    return { p, looks };
}

const waits = (looks: number[]) => looks.map((at, i) => at - (looks[i - 1] ?? 0));

describe("poll", () => {
    it("looks while something is pending, less and less often, without being told the same again", async () => {
        const { looks } = polling("12", ["12"]);
        await vi.advanceTimersByTimeAsync(60_000);
        expect(waits(looks).slice(0, 6)).toEqual([1500, 3000, 6000, 12_000, 15_000, 15_000]);
    });

    it("keeps looking after a look fails", async () => {
        const { looks } = polling("12", [new Error("502"), new Error("502"), "12"]);
        await vi.advanceTimersByTimeAsync(60_000);
        expect(looks.length).toBeGreaterThan(4);
    });

    it("stops once nothing is pending", async () => {
        const { looks } = polling("12", ["12", ""]);
        await vi.advanceTimersByTimeAsync(60_000);
        expect(looks).toEqual([1500, 4500]);
    });

    it("starts over at the first wait when what is pending changes", async () => {
        const { looks } = polling("12", ["12", "12", "12,13", "12,13"]);
        await vi.advanceTimersByTimeAsync(30_000);
        expect(waits(looks).slice(0, 5)).toEqual([1500, 3000, 6000, 1500, 3000]);
    });

    it("does nothing while nothing is pending", async () => {
        const { looks } = polling("", ["12"]);
        await vi.advanceTimersByTimeAsync(60_000);
        expect(looks).toEqual([]);
    });

    it("stops in a hidden tab and looks again soon once it is shown", async () => {
        const { p, looks } = polling("12", ["12"], { hidden: true });
        await vi.advanceTimersByTimeAsync(60_000);
        expect(looks).toEqual([]);
        p.visible(true);
        await vi.advanceTimersByTimeAsync(4500);
        expect(looks).toHaveLength(2);
        p.visible(false);
        await vi.advanceTimersByTimeAsync(60_000);
        expect(looks).toHaveLength(2);
        p.visible(true);
        await vi.advanceTimersByTimeAsync(1500);
        expect(looks).toHaveLength(3);
    });

    it("stops for good", async () => {
        const { p, looks } = polling("12", ["12"]);
        p.stop();
        p.update("13");
        await vi.advanceTimersByTimeAsync(60_000);
        expect(looks).toEqual([]);
    });
});
