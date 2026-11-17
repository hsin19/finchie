<script lang="ts">
/**
 * The page being opened, roughly as it will look, while its data loads (the layout shows it once that
 * takes longer than a glance): panels where its panels go, bars where its figures and rows go. `route` is
 * the page's route id.
 */
let { route }: { route: string | null; } = $props();

const kind = $derived.by(() => {
    if (route === "/") return "overview";
    if (route === "/statements" || route === "/accounts" || route === "/transactions") return route.slice(1);
    if (route?.endsWith("/[id]")) return "detail";
    return "list";
});
</script>

{#snippet bar(width: string, height = "h-3")}
    <div class="{height} {width} rounded-[3px] bg-raised"></div>
{/snippet}

{#snippet tiles()}
    <div class="grid grid-cols-2 gap-2 md:grid-cols-4 md:gap-3">
        {#each [0, 1, 2, 3] as i (i)}
            <div class="panel flex flex-col gap-2 p-3 md:p-3.5">
                {@render bar("w-20", "h-2.5")}
                {@render bar("w-28", "h-6")}
                {@render bar("w-24", "h-2.5")}
            </div>
        {/each}
    </div>
{/snippet}

{#snippet rows(count: number)}
    <div class="panel flex flex-col p-3 md:p-3.5">
        {@render bar("w-32", "h-2.5")}
        {#each Array.from({ length: count }, (_, i) => i) as i (i)}
            <div class="flex items-center gap-3 border-t border-rule py-3 first-of-type:border-t-0">
                {@render bar("w-12")}
                {@render bar(i % 3 === 0 ? "w-1/2" : i % 3 === 1 ? "w-1/3" : "w-2/5")}
                <div class="ml-auto">{@render bar("w-16")}</div>
            </div>
        {/each}
    </div>
{/snippet}

<div class="flex flex-col gap-3 motion-safe:animate-pulse" aria-hidden="true">
    {#if kind === "overview"}
        {@render tiles()}
        <div class="panel h-64"></div>
    {:else if kind === "statements"}
        <div class="flex gap-2">
            {@render bar("w-20", "h-9")}
            {@render bar("w-28", "h-9")}
            {@render bar("w-20", "h-9")}
        </div>
        {@render tiles()}
        <div class="grid gap-3 md:grid-cols-2">
            <div class="panel h-40"></div>
            <div class="panel h-40"></div>
        </div>
    {:else if kind === "transactions"}
        <div class="panel flex flex-col gap-3 p-3 md:p-3.5">
            {@render bar("w-full", "h-10")}
            <div class="flex flex-wrap gap-2">
                {#each [0, 1, 2, 3, 4, 5, 6, 7] as i (i)}{@render bar("w-20", "h-8")}{/each}
            </div>
        </div>
        {@render rows(10)}
    {:else if kind === "accounts"}
        <!-- One panel of figures: the net worth large, then assets, liabilities, and each kind's worth. -->
        <div class="panel flex flex-wrap items-end gap-x-8 gap-y-3 px-3.5 py-2.5">
            <div class="flex flex-col gap-2">
                {@render bar("w-20", "h-2.5")}
                {@render bar("w-40", "h-6")}
            </div>
            {#each [0, 1, 2, 3, 4, 5] as i (i)}
                <div class="flex flex-col gap-2">
                    {@render bar("w-20", "h-2.5")}
                    {@render bar("w-24")}
                </div>
            {/each}
        </div>
        <div class="panel h-48"></div>
        {@render rows(6)}
    {:else if kind === "detail"}
        {@render bar("w-48", "h-5")}
        <div class="grid gap-3 md:grid-cols-2">
            <div class="panel h-48"></div>
            <div class="panel h-48"></div>
        </div>
        {@render rows(6)}
    {:else}
        {@render rows(8)}
    {/if}
</div>
<span class="sr-only">載入中…</span>
