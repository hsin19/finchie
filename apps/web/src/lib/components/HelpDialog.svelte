<script lang="ts">
import { helpGroups } from "#lib/shortcuts.js";
import Dialog from "./Dialog.svelte";

let { open = $bindable(false) }: { open: boolean; } = $props();

const groups = helpGroups();
</script>

<Dialog bind:open title="快捷鍵">
    <div class="flex items-center justify-between border-b border-line px-4 py-3">
        <h2 class="label">KEYS · 快捷鍵</h2>
        <button type="button" class="text-mute hover:text-ink" onclick={() => (open = false)} aria-label="關閉"><span class="text-accent">[esc]</span></button>
    </div>
    <div class="grid gap-4 p-4 sm:grid-cols-2">
        {#each groups as group (group.title)}
            <section>
                <h3 class="label mb-2">{group.title}</h3>
                <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
                    {#each group.lines as line, i (i)}
                        <dt class="text-accent">[{line.shown}]</dt>
                        <dd class="m-0 text-dim">{line.label}</dd>
                    {/each}
                </dl>
            </section>
        {/each}
    </div>
</Dialog>
