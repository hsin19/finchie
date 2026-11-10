<script lang="ts">
import type {
    Document,
    Statement,
} from "#lib/api/client.js";
import {
    monthOf,
    slashDate,
} from "#lib/dates.js";
import { institution } from "#lib/institutions.js";
import {
    statementStatus,
    statementSummary,
} from "#lib/statements.js";
import { resolve } from "$app/paths";
import Tag from "./Tag.svelte";

/** A bill in a list: its tag, its bank, and when it closed, as `month` ("2026-10") or `day` ("10/08 結帳"). */
let { statement, doc, closing = "month" }: { statement: Statement; doc?: Document; closing?: "month" | "day"; } = $props();
</script>

<a href={resolve("/statements/[id]", { id: String(statement.id) })} class="flex min-h-11 flex-col gap-0.5 border-t border-rule py-2.5 text-ink hover:text-ink">
    <span>
        <Tag status={statementStatus(statement, doc)} />
        {institution(statement.institution).name}
        {#if closing === "month"}{monthOf(statement.closingDate)}{:else}<span class="text-mute">{slashDate(statement.closingDate)} 結帳</span>{/if}
    </span>
    <span class="text-[12px] text-mute">{statementSummary(statement)}</span>
</a>
