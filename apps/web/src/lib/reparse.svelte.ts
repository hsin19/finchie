import { reparse } from "#lib/api/mutations.js";
import { timeOfDay } from "#lib/dates.js";

/**
 * A "re-parse" control's state for one document. The document turns pending and the layout follows it
 * until the parser is done; a parse that fails the same way again can be over before the page shows it
 * pending, so the control says when it asked.
 */
export class Reparse {
    #id: () => number;
    /** The request is on its way. */
    requesting = $state(false);
    /** Why the request failed. */
    error = $state<string>();
    /** When the last request went through, "17:18:03". */
    askedAt = $state<string>();

    constructor(id: () => number) {
        this.#id = id;
    }

    async run(): Promise<void> {
        if (this.requesting) return;
        this.requesting = true;
        this.error = undefined;
        const { error } = await reparse(this.#id());
        this.requesting = false;
        if (error) this.error = error;
        else this.askedAt = timeOfDay();
    }
}
