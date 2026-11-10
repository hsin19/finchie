import {
    beforeEach,
    describe,
    expect,
    it,
    vi,
} from "vitest";

vi.mock("$app/navigation", () => ({ invalidate: vi.fn(async () => {}) }));
const { invalidate } = await import("$app/navigation");
const { change } = await import("./mutations");

beforeEach(() => vi.mocked(invalidate).mockClear());

describe("change", () => {
    it("gives what the server answered and reloads the ledger", async () => {
        const outcome = await change(Promise.resolve({ data: { id: 7 }, response: new Response(null, { status: 201 }) }));
        expect(outcome).toEqual({ data: { id: 7 } });
        expect(invalidate).toHaveBeenCalledWith("app:ledger");
    });

    it("gives why it failed and reloads nothing", async () => {
        const response = new Response(null, { status: 409, statusText: "Conflict" });
        expect(await change(Promise.resolve({ error: { message: "name taken" }, response }))).toEqual({ error: "name taken" });
        expect(await change(Promise.reject(new TypeError("Failed to fetch")))).toEqual({ error: "連不到 backend" });
        expect(invalidate).not.toHaveBeenCalled();
    });
});
