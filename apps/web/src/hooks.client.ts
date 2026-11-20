import { reportError } from "#lib/telemetry.js";
import type { HandleClientError } from "@sveltejs/kit/hooks";

// SvelteKit catches what a load or a navigation throws, so window.onerror never sees it (ADR 0024). A
// framework error is an address with no route behind it: the owner's typing, not a fault.
export const handleError: HandleClientError = ({ kind, error }) => {
    if (kind === "framework") return;
    reportError(kind === "app" ? new Error(error.message) : error);
};
