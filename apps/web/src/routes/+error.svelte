<script lang="ts">
import { api } from "#lib/api/client.js";
import { resolve } from "$app/paths";
import { page } from "$app/state";

// A page can fail for want of a session before any API request says so: the code of a page not opened
// yet is behind Cloudflare Access too, and importing it fails like any network error. So ask the API:
// a read that finds the session gone signs in again (session.ts).
$effect(() => {
    if (page.status !== 404) api().GET("/api/settings").catch(() => {});
});
</script>

<section class="panel flex flex-col gap-2 p-4">
    <div><span class="whitespace-pre text-neg">[ERR ]</span> <span class="text-dim">HTTP {page.status}</span></div>
    <p class="m-0 text-ink">{page.error?.message ?? "Something went wrong"}</p>
    {#if page.status === 404}
        <p class="m-0 text-mute">找不到這個頁面或資料。<a href={resolve("/")}>← 回總覽</a></p>
    {:else}
        <!-- An expired Cloudflare Access session signs in again by itself (above); what stays here is a
             backend out of reach, or a sign-in that did not help, which a 401 says itself. -->
        {#if page.status !== 401}<p class="m-0 text-mute">連不到 backend，或登入過期了：重新整理會重新登入。</p>{/if}
        <div>
            <button type="button" class="control" onclick={() => location.reload()}>重新整理</button>
        </div>
    {/if}
</section>
