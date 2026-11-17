// The command the header shows for a page in place of its path's (`accounts show esun/12345`), named by the
// page's content once its data is in: a page's data comes after the layout draws (stream.svelte.ts).

let named = $state<string>();

export const pageCommand = {
    get current(): string | undefined {
        return named;
    },
};

/** Names the page's command while the calling component is shown. Called while it initializes. */
export function nameCommand(command: () => string): void {
    $effect(() => {
        named = command();
        return () => {
            named = undefined;
        };
    });
}
