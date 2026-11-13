<script lang="ts">
import type { Need } from "#lib/attention.js";

/** What needs the owner (attention.ts): shown first, and only when there is something. */
let { needs }: { needs: Need[]; } = $props();

// An account gone quiet may be no more than a month without lines; the others are wrong until dealt with.
const tone = (need: Need) => (need.key === "stale" ? "text-accent" : "text-neg");
</script>

<section class="panel border-neg px-3.5 py-2.5" aria-labelledby="needs-heading">
    <h2 id="needs-heading" class="label text-neg">ATTENTION · 待處理（{needs.length}）</h2>
    <ul class="m-0 mt-1 list-none p-0">
        {#each needs as need (need.key)}
            <li class="flex flex-col gap-0.5 border-t border-rule py-2 first:border-t-0 md:flex-row md:items-baseline md:gap-3">
                <a href={need.href} class="shrink-0 text-ink hover:text-ink-bright"><span class={tone(need)}>●</span> {need.text}</a>
                {#if need.names.length > 0}
                    <span class="text-[12px] text-dim">
                        {#each need.names as name, i (name.href)}{i > 0 ? "、" : ""}<a href={name.href}>{name.text}</a>{/each}
                    </span>
                {/if}
            </li>
        {/each}
    </ul>
</section>
