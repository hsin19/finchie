import {
    describe,
    expect,
    it,
} from "vitest";
import {
    formatAmount,
    formatPrice,
    negate,
    roundWhole,
    sign,
    subtract,
    sum,
} from "./money";

describe("sum", () => {
    it("adds exactly across scales", () => {
        expect(sum(["0.1", "0.2"])).toBe("0.3");
        expect(sum(["23104", "346.56", "-2180", "289", "-35.5"])).toBe("21524.06");
    });

    it("is zero for nothing", () => {
        expect(sum([])).toBe("0");
    });
});

describe("subtract and negate", () => {
    it("handles signs and zero", () => {
        expect(subtract("5340", "5220")).toBe("120");
        expect(subtract("5220", "5340.5")).toBe("-120.5");
        expect(negate("0.00")).toBe("0.00");
        expect(sign("-0.01")).toBe(-1);
        expect(sign("0")).toBe(0);
    });
});

describe("formatAmount", () => {
    it.each([
        ["23104", "23,104"],
        ["-41280", "-41,280"],
        ["780.00", "780"],
        ["-35.5", "-35.50"],
        ["346.56", "346.56"],
        ["1234567.891", "1,234,567.891"],
        ["0", "0"],
    ])("%s → %s", (input, expected) => {
        expect(formatAmount(input)).toBe(expected);
    });

    it("marks positive amounts when signed", () => {
        expect(formatAmount("120", { signed: true })).toBe("+120");
        expect(formatAmount("-120", { signed: true })).toBe("-120");
        expect(formatAmount("0", { signed: true })).toBe("0");
    });

    it("rejects anything but a decimal", () => {
        expect(() => formatAmount("1e3")).toThrow();
    });
});

describe("formatPrice", () => {
    it.each([
        ["32.8538370571241164", "32.85"],
        ["3000000", "3,000,000"],
        ["1", "1"],
        ["0.5", "0.50"],
        ["0.000144510061", "0.0001445"],
        ["0.00014351", "0.0001435"],
        ["0", "0"],
    ])("%s → %s", (input, expected) => {
        expect(formatPrice(input)).toBe(expected);
    });
});

describe("roundWhole", () => {
    it.each([
        ["296380.87", "296381"],
        ["296380.5", "296381"],
        ["296380.49", "296380"],
        ["-2.5", "-3"],
        ["-0.4", "0"],
        ["420600.00", "420600"],
        ["15124", "15124"],
    ])("%s → %s", (input, expected) => {
        expect(roundWhole(input)).toBe(expected);
    });
});
