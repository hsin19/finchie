import {
    describe,
    expect,
    it,
} from "vitest";
import {
    isMonth,
    monthRange,
    monthsBetween,
    shiftMonth,
    today,
    weekday,
} from "./dates";

describe("months", () => {
    it("validates", () => {
        expect(isMonth("2026-10")).toBe(true);
        expect(isMonth("2026-13")).toBe(false);
        expect(isMonth(null)).toBe(false);
    });

    it("shifts across years", () => {
        expect(shiftMonth("2026-01", -1)).toBe("2025-12");
        expect(shiftMonth("2026-12", 1)).toBe("2027-01");
    });

    it("knows the last day, leap years included", () => {
        expect(monthRange("2028-02")).toEqual({ from: "2028-02-01", to: "2028-02-29" });
        expect(monthRange("2026-10").to).toBe("2026-10-31");
    });

    it("lists newest first", () => {
        expect(monthsBetween("2026-08", "2026-10")).toEqual(["2026-10", "2026-09", "2026-08"]);
        expect(monthsBetween("2026-11", "2026-10")).toEqual([]);
    });
});

describe("weekday", () => {
    it("ignores the local time zone", () => {
        expect(weekday("2026-10-16")).toBe("五");
    });
});

describe("today", () => {
    it("is the calendar day in Taipei, wherever the browser is", () => {
        expect(today(new Date("2026-11-09T20:00:00Z"))).toBe("2026-11-10");
    });
});
