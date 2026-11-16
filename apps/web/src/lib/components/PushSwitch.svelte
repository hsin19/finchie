<script lang="ts">
import Dot from "#lib/components/Dot.svelte";
import {
    type PushState,
    pushState,
    testPush,
    turnOff,
    turnOn,
} from "#lib/push.js";
import { onMount } from "svelte";

/** Pushes to this device: what it can take, and the switch for it. `publicKey`: the deployment's (Settings). */
let { publicKey }: { publicKey: string; } = $props();

let current = $state<PushState>();
let busy = $state(false);
let note = $state<string>();

onMount(() => void pushState().then(s => (current = s)));

async function act(run: () => Promise<PushState | string>) {
    busy = true;
    note = undefined;
    try {
        const outcome = await run();
        if (outcome === "unsupported" || outcome === "denied" || outcome === "off" || outcome === "on") current = outcome;
        else note = outcome;
    } catch (error) {
        note = error instanceof TypeError ? "連不到 backend" : (error as Error).message;
    } finally {
        busy = false;
    }
}
</script>

{#if current}
    <section class="panel flex flex-wrap items-center gap-x-3 gap-y-2 px-3.5 py-2.5 text-[13px]" aria-labelledby="push-heading">
        <h2 id="push-heading" class="label">PUSH · 推播</h2>
        {#if current === "unsupported"}
            <span class="text-mute">這個瀏覽器收不到推播。iPhone 要先在 Safari 用「加入主畫面」，從主畫面打開 Finchie 再來這裡開。</span>
        {:else if current === "denied"}
            <span class="text-mute">通知被擋下了：到瀏覽器或手機的設定裡，允許 Finchie 發通知。</span>
        {:else if current === "off"}
            <span class="text-dim">這台裝置還沒開推播：有要處理的事時，不會通知這台。</span>
            <button type="button" class="control min-h-8 border-accent text-[12px]" disabled={busy} onclick={() => void act(() => turnOn(publicKey))}>在這台裝置接收推播</button>
        {:else}
            <span class="text-dim"><Dot class="text-pos" /> 這台裝置會收到推播</span>
            <button type="button" class="control min-h-8 text-[12px]" disabled={busy} onclick={() => void act(async () => `送出了，${await testPush()} 台裝置收到`)}>送一則測試</button>
            <button type="button" class="control min-h-8 text-[12px] text-dim" disabled={busy} onclick={() => void act(turnOff)}>停止這台的推播</button>
        {/if}
        {#if note}<span class="text-[12px] text-mute">{note}</span>{/if}
    </section>
{/if}
