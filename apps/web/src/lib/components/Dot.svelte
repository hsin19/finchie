<script lang="ts">
import Circle from "@lucide/svelte/icons/circle";

/** A status's dot (ADR 0007), in the colour of the text around it or of `class`. */
let { class: className = "" }: { class?: string; } = $props();
</script>

<Circle fill="currentColor" strokeWidth={0} class="size-[0.6em] align-[0.05em] {className}" />
