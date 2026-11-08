<script lang="ts">
import type { LineCounterpart } from "#lib/api/client.js";
import { institution } from "#lib/labels.js";
import { resolve } from "$app/paths";

/** `out`: the money left this line for the other one. */
let { other, out, class: className = "" }: { other: LineCounterpart; out: boolean; class?: string; } = $props();

// Where a linked line's money went or came from (ADR 0016): another of the owner's accounts, or the
// card bill a payment went to, scrolled to its payment line.
const href = $derived(
    other.accountId !== undefined
        ? resolve("/accounts/[id]", { id: String(other.accountId) })
        : resolve(`/statements/[id]?line=${other.id}`, { id: String(other.statementId) }),
);
const text = $derived(
    other.accountId !== undefined
        ? [institution(other.institution).name, other.accountName, other.accountNumber].filter(Boolean).join(" ")
        : `${institution(other.institution).name} 信用卡帳單`,
);
</script>

<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- resolved above -->
<a {href} class={className}>{out ? "→" : "←"} {text}</a>
