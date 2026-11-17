// Single-key shortcuts ("t", "j", "/"), terminal style, must never take a key the browser or a dialog
// needs. Kept free of runes so it can be tested on its own.

/** Inputs that take no typing; a focused checkbox must not stop the shortcuts. */
const NOT_TEXT = ["checkbox", "radio", "button", "submit", "reset", "image", "file", "color", "range"].map(type => `:not([type='${type}'])`).join("");

/** Where keys are text: shortcuts would eat letters. A select picks options by typing too. */
const TEXT = `input${NOT_TEXT}, textarea, select, [contenteditable]:not([contenteditable='false'])`;

/** Controls that Enter or Space activates. */
const CONTROL = "button, a[href], summary, input, [role='button'], [role='link'], [role='option'], [role='checkbox'], [role='switch'], [role='tab']";

/**
 * A key press that belongs to an IME composition, such as the Enter that picks a Zhuyin candidate.
 * Safari sends that Enter with isComposing already false, but still with keyCode 229.
 */
export function composing(event: KeyboardEvent): boolean {
    return event.isComposing || event.keyCode === 229;
}

export type KeyHandlers = Record<string, (event: KeyboardEvent) => void>;

/**
 * Listens on `target` (the window) for single-key shortcuts; returns the function that stops. Page
 * shortcuts listen in the capture phase and global ones (the layout's) in the bubble phase, so a page
 * always gets the key first. Mount order cannot decide it: after a client-side navigation the layout
 * has been listening longer than the page that just mounted. While `active` says no, the keys are
 * left alone, for the layout to take.
 */
export function listen(target: EventTarget, handlers: KeyHandlers, { global = false, active = (): boolean => true }: { global?: boolean; active?: () => boolean; } = {}): () => void {
    const options = { capture: !global };
    const listener = (event: Event) => {
        const key = event as KeyboardEvent;
        const handler = handlers[key.key];
        if (!handler || !active() || !claims(key)) return;
        key.preventDefault();
        handler(key);
    };
    target.addEventListener("keydown", listener, options);
    return () => target.removeEventListener("keydown", listener, options);
}

/**
 * Whether a page shortcut may take this key press. Not with Ctrl, Alt, or Meta held; not while
 * typing or composing; not while a modal dialog is open (it owns the keyboard, Esc included); and
 * not Enter or Space on a focused control, which the browser uses to press it.
 */
export function claims(event: KeyboardEvent): boolean {
    if (event.defaultPrevented || event.ctrlKey || event.metaKey || event.altKey || composing(event)) return false;
    const target = event.target instanceof Element ? event.target : null;
    if ((target?.ownerDocument ?? document).querySelector("dialog[open]")) return false;
    if (!target) return true;
    if (target.closest(TEXT)) return false;
    return !((event.key === "Enter" || event.key === " ") && target.closest(CONTROL));
}
