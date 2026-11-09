<script lang="ts">
import Dialog from "./Dialog.svelte";

let { open = $bindable(false) }: { open: boolean; } = $props();

const groups = [
    { name: "GLOBAL · 全域", keys: [["o", "總覽"], ["s", "帳單"], ["t", "交易"], ["a", "帳戶"], ["d", "文件與上傳"], ["/", "搜尋交易"], ["⌘K", "指令"], ["?", "這個說明"]] },
    { name: "TRANSACTIONS · 交易", keys: [["j / k", "上下移動"], ["enter", "詳細"], ["x", "關閉詳細"], ["n / p", "下一頁 / 上一頁"], ["esc", "清除搜尋"]] },
    { name: "STATEMENTS · 帳單", keys: [["p / n", "上個月 / 下個月"], ["l", "全部帳單 / 按月看"]] },
    { name: "STATEMENT · 單張帳單", keys: [["f", "開啟帳單 PDF"], ["r", "重新解析"]] },
    { name: "DOCUMENTS · 文件", keys: [["enter", "選擇要上傳的檔案"], ["/", "搜尋檔名"], ["esc", "清除搜尋"]] },
    { name: "DOCUMENT · 單份文件", keys: [["r", "重新解析"], ["f", "開啟帳單 PDF"], ["1–9", "開啟第幾個附件"]] },
];
</script>

<Dialog bind:open title="快捷鍵">
    <div class="flex items-center justify-between border-b border-line px-4 py-3">
        <h2 class="label">KEYS · 快捷鍵</h2>
        <button type="button" class="text-mute hover:text-ink" onclick={() => (open = false)} aria-label="關閉"><span class="text-accent">[esc]</span></button>
    </div>
    <div class="grid gap-4 p-4 sm:grid-cols-2">
        {#each groups as group (group.name)}
            <section>
                <h3 class="label mb-2">{group.name}</h3>
                <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
                    {#each group.keys as [key, action] (key)}
                        <dt class="text-accent">[{key}]</dt>
                        <dd class="m-0 text-dim">{action}</dd>
                    {/each}
                </dl>
            </section>
        {/each}
    </div>
</Dialog>
