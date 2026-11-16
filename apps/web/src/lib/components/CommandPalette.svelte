<script lang="ts">
import { composing } from "#lib/keys.js";
import ChevronRight from "@lucide/svelte/icons/chevron-right";
import Dialog from "./Dialog.svelte";

export interface Command {
    id: string;
    label: string;
    hint?: string;
    run: () => void;
}

let { open = $bindable(false), commands }: { open: boolean; commands: Command[]; } = $props();
let query = $state("");
let active = $state(0);

const matches = $derived.by(() => {
    const words = query.toLowerCase().split(/\s+/).filter(Boolean);
    return commands.filter(c => words.every(w => `${c.label} ${c.hint ?? ""}`.toLowerCase().includes(w))).slice(0, 12);
});

$effect(() => {
    if (open) {
        query = "";
        active = 0;
    }
});

function run(command: Command | undefined) {
    if (!command) return;
    open = false;
    command.run();
}

function onkeydown(event: KeyboardEvent) {
    if (composing(event)) return;
    if (event.key === "ArrowDown") active = Math.min(active + 1, matches.length - 1);
    else if (event.key === "ArrowUp") active = Math.max(active - 1, 0);
    else if (event.key === "Enter") run(matches[active]);
    else return;
    event.preventDefault();
}
</script>

<Dialog bind:open title="指令">
    <div class="flex items-center gap-2 border-b border-line px-4">
        <label for="command" class="text-accent">&gt;</label>
        <!-- svelte-ignore a11y_autofocus -->
        <input
            id="command"
            autofocus
            autocomplete="off"
            class="min-h-12 grow border-none bg-transparent text-ink outline-none"
            placeholder="前往頁面、切換月份…"
            bind:value={query}
            oninput={() => (active = 0)}
            {onkeydown}
            role="combobox"
            aria-expanded="true"
            aria-controls="command-list"
            aria-activedescendant={matches[active] ? `command-${matches[active].id}` : undefined}
        />
    </div>
    <ul id="command-list" role="listbox" class="m-0 max-h-[50vh] list-none overflow-y-auto p-2">
        {#each matches as command, i (command.id)}
            <li
                id="command-{command.id}"
                role="option"
                aria-selected={i === active}
                class="flex cursor-pointer justify-between gap-4 rounded-[4px] px-3 py-2 {i === active ? 'bg-raised text-ink' : 'text-dim'}"
                onmousemove={() => (active = i)}
                onclick={() => run(command)}
                onkeydown={() => {}}
            >
                <span class="flex items-center gap-1"><ChevronRight class={i === active ? "" : "invisible"} />{command.label}</span>
                {#if command.hint}<span class="text-mute">{command.hint}</span>{/if}
            </li>
        {:else}
            <li class="px-3 py-2 text-mute">沒有符合的指令</li>
        {/each}
    </ul>
</Dialog>
