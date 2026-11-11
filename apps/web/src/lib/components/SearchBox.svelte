<script lang="ts">
import { composing } from "#lib/keys.js";
import {
    Search,
    searchTarget,
} from "#lib/search.svelte.js";
import { afterNavigate } from "$app/navigation";
import { page } from "$app/state";

/**
 * The page's search: the text in `?{param}`, applied while typing (Search). "/" focuses it (the
 * layout's key), Esc clears it. `onleave`, if given, is where Enter and ↓ go after sending: the results.
 */
let { param = "q", asked, onsearch, placeholder, onleave }: {
    param?: string;
    /** The query as last asked for (QueryRequests). */
    asked: () => string;
    onsearch: (q: string) => void;
    placeholder: string;
    onleave?: () => void;
} = $props();

const id = $props.id();
// svelte-ignore state_referenced_locally
const search = new Search(page.url.searchParams.get(param) ?? "", { asked: () => asked(), send: q => onsearch(q) });
afterNavigate(({ to }) => search.landed(to?.url.searchParams.get(param) ?? ""));
$effect(() => () => search.dispose());

function onkeydown(event: KeyboardEvent) {
    if (composing(event)) return;
    if (event.key === "Escape") {
        search.clear();
    } else if (event.key === "Enter" || (onleave && event.key === "ArrowDown")) {
        event.preventDefault();
        search.flush();
        if (onleave) {
            (event.currentTarget as HTMLInputElement).blur();
            onleave();
        }
    }
}
</script>

<div class="flex items-center gap-2 rounded-[4px] border border-line-strong bg-ground px-2.5">
    <label for={id} class="whitespace-nowrap"><span class="text-accent">[/]</span> 搜尋</label>
    <input
        {id}
        type="search"
        autocomplete="off"
        {placeholder}
        class="min-h-10 min-w-0 grow border-none bg-transparent text-ink outline-none"
        bind:value={search.value}
        oninput={() => search.typed()}
        {onkeydown}
        {@attach searchTarget}
    />
    <span class="hidden text-[12px] text-mute sm:inline"><span class="text-accent">[esc]</span> 清除</span>
</div>
