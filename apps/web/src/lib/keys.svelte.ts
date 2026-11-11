import { listen } from "#lib/keys.js";
import {
    type KeyOf,
    type ScopeName,
} from "#lib/shortcuts.js";

/**
 * A scope's shortcuts (shortcuts.ts) for as long as the calling component is mounted: only keys the
 * keymap lists for it, so the help dialog always says what a key does. The layout binds "global"; a
 * page's handler for the same key wins.
 */
export function onKeys<S extends ScopeName>(scope: S, handlers: Partial<Record<KeyOf<S>, (event: KeyboardEvent) => void>>): void {
    $effect(() => listen(window, handlers as Record<string, (event: KeyboardEvent) => void>, { global: scope === "global" }));
}
