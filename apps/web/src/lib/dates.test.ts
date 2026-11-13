import {
    describe,
    expect,
    it,
} from "vitest";
import {
    addDays,
    formatBytes,
    isMonth,
    monthRange,
    monthsBetween,
    shiftMonth,
    today,
    weekday,
    yearlyDate,
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

describe("addDays and yearlyDate", () => {
    it("counts days across a month and a year", () => {
        expect([addDays("2026-11-13", 50), addDays("2026-12-31", 1), addDays("2026-03-01", -1)]).toEqual(["2027-01-02", "2027-01-01", "2026-02-28"]);
    });

    it("leaves the year out only within this one", () => {
        expect([yearlyDate("2026-12-31", "2026-11-13"), yearlyDate("2030-01-02", "2026-11-13")]).toEqual(["12/31", "2030/01/02"]);
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

describe("formatBytes", () => {
    it("says a size the short way", () => {
        expect(formatBytes(512)).toBe("512 B");
        expect(formatBytes(12 * 1024)).toBe("12 KB");
        expect(formatBytes(1.5 * 1024 * 1024)).toBe("1.5 MB");
        expect(formatBytes(32 * 1024 * 1024)).toBe("32 MB");
    });
});
