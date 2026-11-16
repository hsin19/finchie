import {
    describe,
    expect,
    it,
} from "vitest";
import { applicationServerKey } from "./push";

describe("applicationServerKey", () => {
    it("reads a base64url key, padding and all, as its bytes", () => {
        expect([...applicationServerKey("AQID_-8")]).toEqual([1, 2, 3, 255, 239]);
        expect(applicationServerKey("BA").length).toBe(1);
    });
});
