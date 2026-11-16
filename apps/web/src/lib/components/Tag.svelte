<script lang="ts">
import Dot from "#lib/components/Dot.svelte";
import {
    statuses,
    type TagStatus,
} from "#lib/statuses.js";
import { tone } from "#lib/tone.js";

let { status, label = false }: { status: TagStatus; label?: boolean; } = $props();
const s = $derived(statuses[status]);
</script>

<!-- Alone, at least as wide as the longest (a dot and "skip"), so a column of them lines up. -->
<span class="inline-block whitespace-nowrap {label ? '' : 'min-w-[7ch]'} {tone[s.tone]}" title={s.label}><Dot /> {s.tag}</span>{#if label}<span class="ml-2 text-dim">{s.label}</span>{/if}
