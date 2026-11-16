<script lang="ts">
import type { Component } from "svelte";

/**
 * The key that does what its control does (ADR 0007: square brackets mean a key to press), shown where a
 * keyboard is likely, from `md` up. A phone has none, so there the control shows `icon` in its place, or
 * nothing.
 */
let { hint, icon: Icon }: { hint: string; icon?: Component<{ class?: string; }>; } = $props();
</script>

<span class="hidden text-accent md:inline">[{hint}]</span>{#if Icon}<Icon class="md:hidden" />{/if}
