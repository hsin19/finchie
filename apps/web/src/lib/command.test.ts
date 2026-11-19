import {
    describe,
    expect,
    it,
} from "vitest";
import {
    commandLine,
    commandText,
} from "./command";

const line = (url: string, custom?: string) => {
    const { pathname, searchParams } = new URL(url, "https://finchie.test");
    return commandText(commandLine(pathname, searchParams, custom));
};

describe("command line", () => {
    it("names the page as a command", () => {
        expect(line("/")).toBe("finchie overview");
        expect(line("/upload")).toBe("finchie upload");
    });

    it("spells every flag out in full", () => {
        expect(line("/statements?m=2026-08")).toBe("finchie statements --period 2026-08");
        expect(line("/transactions?q=7069&m=all")).toBe("finchie transactions --query 7069 --period all");
        expect(line("/statements?all")).toBe("finchie statements --all");
        expect(line("/transactions?closing=2026-08&kind=purchase&kind=refund&overseas=1")).toBe(
            "finchie transactions --closing 2026-08 --kind purchase,refund --overseas",
        );
    });

    it("takes a page's own command", () => {
        expect(line("/statements/12?line=51", "statements show taishin/2026-08")).toBe("finchie statements show taishin/2026-08 --line 51");
    });
});
