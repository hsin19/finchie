import { reportError } from "#lib/telemetry.js";
import type { HandleClientError } from "@sveltejs/kit/hooks";

// SvelteKit catches what a load or a navigation throws, so window.onerror never sees it (ADR 0024). A
// framework error is an address with no route behind it: the owner's typing, not a fault. An app error is a
// load's error(), from need(): below 500 the API answered as it should (not there, the session ended).
export const handleError: HandleClientError = ({ kind, error }) => {
    if (kind === "framework") return;
    if (kind === "app") {
        if (error.status < 500) return;
        reportError(new Error(`HTTP ${error.status}: ${error.message}`));
        return;
    }
    reportError(error);
};
