<script lang="ts">
import { documentsById } from "#lib/documents.js";
import { onKeys } from "#lib/keys.svelte.js";
import { statementsHref } from "#lib/urls.js";
import { goto } from "$app/navigation";
import type { PageProps } from "./$types";
import All from "./All.svelte";
import Period from "./Period.svelte";

let { data }: PageProps = $props();

onKeys("statements", {
    l: () => void goto(statementsHref(data.view === "all" ? undefined : "all")),
});
</script>

{#if data.view === "all"}
    <All statements={data.statements} documents={documentsById(data.documents, data.pending)} />
{:else}
    <Period {data} />
{/if}
