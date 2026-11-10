// While the parser works, the layout looks again and again at which documents are still pending
// (ADR 0007), and reloads the ledger when that changes. The poll owns its loop: each look is awaited,
// and the next is scheduled whatever it brought, an error included. The wait grows while nothing
// changes, since a parser that is down is retried for minutes, and a hidden tab does not poll at all.

export interface Poll {
    /** What is waited for now ("" for nothing), whenever the page learns it: starts or stops the loop. */
    update(waitingFor: string): void;
    /** Call when the tab is hidden or shown. */
    visible(visible: boolean): void;
    stop(): void;
}

/**
 * `check` looks again and resolves to what is still waited for. It may reject: a failed look changes
 * nothing, and the next comes after a longer wait.
 */
export function poll(check: () => Promise<string>, { first = 1500, max = 15_000, hidden = false } = {}): Poll {
    let waitingFor = "";
    let wait = first;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let checking = false;
    let stopped = false;

    function schedule() {
        clearTimeout(timer);
        if (!waitingFor || hidden || stopped || checking) return;
        timer = setTimeout(() => void look(), wait);
    }

    async function look() {
        checking = true;
        let next = waitingFor;
        try {
            next = await check();
        } catch {
            // No answer: as if nothing changed.
        }
        checking = false;
        if (next !== waitingFor) {
            waitingFor = next;
            wait = first;
        } else {
            wait = Math.min(wait * 2, max);
        }
        schedule();
    }

    return {
        update(next) {
            if (next !== waitingFor) {
                waitingFor = next;
                wait = first;
            }
            schedule();
        },
        visible(visible) {
            hidden = !visible;
            // Back in the tab: look again soon, the parser may have finished meanwhile.
            if (visible) wait = first;
            schedule();
        },
        stop() {
            stopped = true;
            clearTimeout(timer);
        },
    };
}
