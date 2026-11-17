import { listen } from "#lib/keys.js";
import {
    type KeyOf,
    type ScopeName,
} from "#lib/shortcuts.js";
import {
    navigating,
    page,
} from "$app/state";

/**
 * A scope's shortcuts (shortcuts.ts) for as long as the calling component is mounted: only keys the
 * keymap lists for it, so the help dialog always says what a key does. The layout binds "global"; a
 * page's handler for the same key wins.
 */
export function onKeys<S extends ScopeName>(scope: S, handlers: Partial<Record<KeyOf<S>, (event: KeyboardEvent) => void>>): void {
    // A page's keys rest while another page loads: the page left stays, unseen, under its skeleton until
    // then, and its keys would act on what is not shown (and start a navigation over the one asked for).
    const shown = () => navigating.to === null || navigating.to.route.id === page.route.id;
    $effect(() => listen(window, handlers as Record<string, (event: KeyboardEvent) => void>, { global: scope === "global", active: scope === "global" ? undefined : shown }));
}
