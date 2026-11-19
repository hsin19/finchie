<script lang="ts">
/** A chart panel's "chart · table" switch. */
let { view = $bindable() }: { view: "chart" | "table"; } = $props();

const views = ["chart", "table"] as const;
</script>

<div role="group" aria-label="檢視方式" class="flex gap-1.5 text-[12px]">
    {#each views as option, i (option)}
        {#if i > 0}<span class="text-mute">·</span>{/if}
        <button
            type="button"
            aria-pressed={view === option}
            class={view === option ? "text-accent underline underline-offset-4" : "text-mute hover:text-ink"}
            onclick={() => (view = option)}
        >
            {option}
        </button>
    {/each}
</div>
