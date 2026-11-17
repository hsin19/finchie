<script lang="ts">
import Loaded from "#lib/components/Loaded.svelte";
import type { PageProps } from "./$types";
import Content from "./Content.svelte";

// The page draws once its data is in (Content.svelte); until then, its skeleton (stream.svelte.ts).
let { data }: PageProps = $props();
</script>

<Loaded ledger={data.ledger} content={data.content} route="/statements">
    {#snippet children(page)}
        <Content data={page} />
    {/snippet}
</Loaded>
