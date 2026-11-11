// A search box whose text is a URL parameter (?q=), applied while typing. Kept apart from the markup
// (SearchBox.svelte) so the timing can be tested.

export interface SearchOptions {
    /** The query as last asked for: the page's, or the one still loading (QueryRequests). */
    asked: () => string;
    /** Sends a query: navigates to the URL with it. */
    send: (q: string) => void;
    /** Milliseconds after the last key. */
    delay?: number;
}

/**
 * The box's text, and when it is sent. Typing sends a moment after the last key. The box follows the
 * URL whenever a navigation lands (Back, a page link, the command palette), except when what landed is
 * the query the box sent itself: by then the user may have typed more. That exception lasts for one
 * landing only, whatever it brought, so it can never swallow a later one.
 */
export class Search {
    value = $state("");
    #options: SearchOptions;
    #timer: ReturnType<typeof setTimeout> | undefined;
    /** What the box sent and has not seen a navigation land since. */
    #sent: string | undefined;

    constructor(initial: string, options: SearchOptions) {
        this.value = initial;
        this.#options = options;
    }

    /** The text changed: send it a moment after the last change. */
    typed(): void {
        clearTimeout(this.#timer);
        this.#timer = setTimeout(() => this.flush(), this.#options.delay ?? 250);
    }

    /** Send the text now, unless it is what was asked for already. */
    flush(): void {
        clearTimeout(this.#timer);
        const q = this.value.trim();
        if (q === this.#options.asked()) return;
        this.#sent = q;
        this.#options.send(q);
    }

    /** Empty the box and the search. */
    clear(): void {
        this.value = "";
        this.flush();
    }

    /** A navigation landed with `q` in the URL. */
    landed(q: string): void {
        const own = this.#sent !== undefined && q === this.#sent;
        this.#sent = undefined;
        if (own) return;
        clearTimeout(this.#timer);
        this.value = q;
    }

    /** The page is going: a search not sent yet is dropped, rather than navigating back to it. */
    dispose(): void {
        clearTimeout(this.#timer);
    }
}

// The page's search box, for the "/" key: the layout focuses whichever page has one mounted.

let current: HTMLInputElement | undefined;

/** Attach to the page's search input: it becomes what focusSearch() focuses while it is mounted. */
export function searchTarget(input: HTMLInputElement): () => void {
    current = input;
    return () => {
        if (current === input) current = undefined;
    };
}

/** Focuses the page's search box; false when the page has none. */
export function focusSearch(): boolean {
    if (!current?.isConnected) return false;
    current.focus();
    current.select();
    return true;
}
