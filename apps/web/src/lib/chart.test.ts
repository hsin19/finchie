import {
    describe,
    expect,
    it,
} from "vitest";
import {
    compact,
    niceStep,
} from "./chart";

describe("niceStep", () => {
    it("rounds up to 1, 2, or 5 times a power of ten", () => {
        expect(niceStep(40000, 4)).toBe(10000);
        expect(niceStep(45000, 4)).toBe(20000);
        expect(niceStep(150000, 3)).toBe(50000);
        expect(niceStep(9000, 4)).toBe(5000);
    });

    it("takes the next power of ten past 5", () => {
        expect(niceStep(240, 4)).toBe(100);
    });
});

describe("compact", () => {
    it("writes thousands as K, below zero too", () => {
        expect(compact(20000)).toBe("20K");
        expect(compact(-1500)).toBe("-1.5K");
        expect(compact(500)).toBe("500");
    });
});
