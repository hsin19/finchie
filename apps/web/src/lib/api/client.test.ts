import {
    describe,
    expect,
    it,
} from "vitest";
import { unwrap } from "./client.js";

describe("unwrap", () => {
    it("takes a response with no content as done", () => {
        expect(() => unwrap({ data: undefined, response: new Response(null, { status: 204, statusText: "No Content" }) })).not.toThrow();
    });

    it("gives the body of a response with one", () => {
        expect(unwrap({ data: { id: 7 }, response: new Response(null, { status: 201 }) })).toEqual({ id: 7 });
    });

    it("throws what the server said went wrong", () => {
        const response = new Response(null, { status: 400, statusText: "Bad Request" });
        expect(() => unwrap({ error: { message: "only points accounts are kept by hand" }, response })).toThrow("only points accounts are kept by hand");
        expect(() => unwrap({ error: undefined, response })).toThrow("Bad Request");
    });
});
