import type { InvestmentPoint } from "#lib/api/client.js";
import {
    describe,
    expect,
    it,
} from "vitest";
import {
    investmentLines,
    investmentQuery,
} from "./investments";

const point = (on: string, worth: string, invested: string): InvestmentPoint => ({
    on,
    worth,
    invested,
    gain: "0",
    unvalued: 0,
    unpriced: 0,
    complete: true,
});

describe("investmentQuery", () => {
    it("asks for as many days up to today as the API gives", () => {
        expect(investmentQuery("2026-11-18")).toEqual({ from: "2024-02-23", to: "2026-11-18", step: "day" });
    });
});

describe("investmentLines", () => {
    it("draws worth and what went in on one scale, with room above and below", () => {
        // 100 to 200 across both lines, padded by 10 each way: 90 at the bottom, 210 at the top.
        const lines = investmentLines([point("2026-11-01", "100", "100"), point("2026-11-02", "200", "150")]);
        expect(lines.worth).toBe("0,91.67 100,8.33");
        expect(lines.invested).toBe("0,91.67 100,50");
    });

    it("puts a single day in the middle", () => {
        const lines = investmentLines([point("2026-11-01", "1000", "1000")]);
        expect(lines.worth).toBe("50,50");
        expect(lines.invested).toBe("50,50");
    });
});
