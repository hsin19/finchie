// Every keyboard shortcut, once: the pages bind their handlers to the keys listed here (onKeys), and
// the help dialog lists them from here. A page's key wins over a global one (keys.ts).

interface Shortcut {
    /** KeyboardEvent.key values. */
    keys: readonly string[];
    label: string;
    /** How the help writes the keys, where listing each would be too long. */
    shown?: string;
}

interface Scope {
    title: string;
    shortcuts: readonly Shortcut[];
    /** Keys the help lists that are no page shortcuts: a chord, or a key inside a text field. */
    also?: readonly { shown: string; label: string; }[];
}

const digits = ["1", "2", "3", "4", "5", "6", "7", "8", "9"] as const;

export const keymap = {
    global: {
        title: "GLOBAL · 全域",
        shortcuts: [
            { keys: ["o"], label: "總覽" },
            { keys: ["s"], label: "帳單" },
            { keys: ["t"], label: "交易" },
            { keys: ["a"], label: "帳戶" },
            { keys: ["d"], label: "文件與上傳" },
            { keys: ["/"], label: "搜尋：這頁有搜尋框就用它，沒有就搜尋交易" },
            { keys: ["?"], label: "這個說明" },
        ],
        also: [{ shown: "⌘K", label: "指令" }],
    },
    transactions: {
        title: "TRANSACTIONS · 交易",
        shortcuts: [
            { keys: ["j", "k"], label: "上下移動" },
            { keys: ["Enter"], label: "詳細" },
            { keys: ["x"], label: "關閉詳細" },
            { keys: ["Escape"], label: "取消選取" },
            { keys: ["n", "p"], label: "下一頁 / 上一頁" },
        ],
        also: [{ shown: "esc", label: "在搜尋框裡：清除搜尋" }],
    },
    statements: {
        title: "STATEMENTS · 帳單",
        shortcuts: [
            { keys: ["p", "n"], label: "上個月 / 下個月" },
            { keys: ["l"], label: "全部帳單 / 按月看" },
        ],
    },
    statement: {
        title: "STATEMENT · 單張帳單",
        shortcuts: [
            { keys: ["f"], label: "開啟帳單 PDF" },
            { keys: ["r"], label: "重新解析" },
        ],
    },
    documents: {
        title: "DOCUMENTS · 文件",
        shortcuts: [{ keys: ["Enter"], label: "選擇要上傳的檔案" }],
        also: [{ shown: "esc", label: "在搜尋框裡：清除搜尋" }],
    },
    document: {
        title: "DOCUMENT · 單份文件",
        shortcuts: [
            { keys: ["r"], label: "重新解析" },
            { keys: ["f"], label: "開啟帳單 PDF" },
            { keys: digits, label: "開啟第幾個附件", shown: "1–9" },
        ],
    },
} as const satisfies Record<string, Scope>;

export type ScopeName = keyof typeof keymap;

/** The keys a scope has. */
export type KeyOf<S extends ScopeName> = (typeof keymap)[S]["shortcuts"][number]["keys"][number];

/** How a key is written in brackets: "enter", "esc", "j". */
export function keyName(key: string): string {
    return ({ "Enter": "enter", "Escape": "esc", " ": "space" } as Record<string, string>)[key] ?? key;
}

/** The help dialog's lines, scope by scope: "[j / k] 上下移動". */
export function helpGroups(): { title: string; lines: { shown: string; label: string; }[]; }[] {
    return Object.values(keymap).map((scope: Scope) => ({
        title: scope.title,
        lines: [
            ...scope.shortcuts.map(s => ({ shown: s.shown ?? s.keys.map(keyName).join(" / "), label: s.label })),
            ...(scope.also ?? []),
        ],
    }));
}
