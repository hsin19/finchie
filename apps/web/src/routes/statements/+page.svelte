<script lang="ts">
import { onKeys } from "#lib/keys.svelte.js";
import { goto } from "$app/navigation";
import { resolve } from "$app/paths";
import type { PageProps } from "./$types";
import All from "./All.svelte";
import Period from "./Period.svelte";

let { data }: PageProps = $props();

onKeys({
    l: () => void goto(data.view === "all" ? resolve("/statements") : resolve("/statements?all")),
});
</script>

{#if data.view === "all"}
    <All statements={data.statements} documents={data.documents} />
{:else}
    <Period {data} />
{/if}
