<script lang="ts">
import type { Month } from "#lib/dates.js";

let { months, value, onchange, all = false }: {
    months: Month[];
    value: Month | "all";
    onchange: (month: Month | "all") => void;
    /** Offer "all" as well as single months. */
    all?: boolean;
} = $props();
</script>

<select class="control" aria-label="期間" {value} onchange={event => onchange(event.currentTarget.value as Month | "all")}>
    {#if all}<option value="all">all</option>{/if}
    {#each months as month (month)}
        <option value={month}>{month}</option>
    {/each}
</select>
