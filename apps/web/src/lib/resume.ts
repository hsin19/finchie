// Installed to a home screen, the app is not opened and closed like a tab: the phone keeps it for days, and
// coming back to it shows the page as it was left, with no reload button to press. So the layout
// reloads the ledger when the app returns after a while.

/** How long out of sight before what is on screen counts as old. Shorter is a glance at another app. */
export const RESUME_AFTER_MS = 5 * 60_000;

export interface Absence {
    /** Call when the page is hidden or shown: whether it is now back from an absence long enough. */
    change(hidden: boolean): boolean;
}

export function absence(staleAfterMs: number, { hidden = false, now = () => Date.now() } = {}): Absence {
    let since = hidden ? now() : undefined;
    return {
        change(hidden) {
            if (hidden) {
                since ??= now();
                return false;
            }
            const left = since;
            since = undefined;
            return left !== undefined && now() - left >= staleAfterMs;
        },
    };
}
