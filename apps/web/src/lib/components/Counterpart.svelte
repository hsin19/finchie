<script lang="ts">
import type { LineCounterpart } from "#lib/api/client.js";
import { counterpartLink } from "#lib/transactions.js";

/** `out`: the money left this line for the other one. */
let { other, out, class: className = "" }: { other: LineCounterpart; out: boolean; class?: string; } = $props();

const link = $derived(counterpartLink(other));
</script>

<a href={link.href} class={className}>{out ? "→" : "←"} {link.text}</a>
