// The banks, brokers, and exchanges Finchie reads from, as the UI names them. Any other institution
// still shows, by its id.

import type { AccountKind } from "#lib/api/client.js";

/** The institution of an account the owner keeps by hand (ADR 0019). */
export const OWNER = "owner";

export interface Institution {
    name: string;
    code: string;
    /** Chart color. Fixed per bank, so a bank keeps its color whichever others are on screen. */
    color: string;
    /** How statements of an account of this kind get here, where the owner has a part in it. */
    source?: Partial<Record<AccountKind, string>>;
}

// Chart colors in this order pass the color-vision checks for neighbors in a stack, on the panel color.
const cardIssuers: Record<string, Institution> = {
    taishin: { name: "台新", code: "TSB", color: "#3987e5" },
    cathay: {
        name: "國泰",
        code: "CUB",
        color: "#d95926",
        source: {
            deposit: "CUBE App → 更多 → 臺幣 → 匯出臺幣電子存摺 → Gmail 篩選器自動轉寄 → 解析入帳（單次上限 220 筆；期間和之前匯出的重疊沒關係，重疊的日子以結束日較晚的那份為準）",
        },
    },
    hsbc: { name: "滙豐", code: "HSBC", color: "#199e70" },
    esun: { name: "玉山", code: "ESUN", color: "#c98500" },
};

// Institutions Finchie reads no card bills from, so never in a spending chart: banks it reads deposit
// statements from, brokers (ADR 0017), exchanges (ADR 0018), and the owner, for accounts kept by hand
// (ADR 0019).
const others: Record<string, Institution> = {
    fubon: { name: "富邦", code: "TFB", color: "var(--color-mute)" },
    ctbc: { name: "中信", code: "CTBC", color: "var(--color-mute)" },
    nextbank: { name: "將來", code: "NEXT", color: "var(--color-mute)" },
    skis: { name: "新光證券", code: "SKIS", color: "var(--color-mute)" },
    max: { name: "MAX", code: "MAX", color: "var(--color-mute)" },
    binance: { name: "Binance", code: "BINANCE", color: "var(--color-mute)" },
    bitget: { name: "Bitget", code: "BITGET", color: "var(--color-mute)" },
    [OWNER]: { name: "手動", code: "MANUAL", color: "var(--color-mute)" },
};

/** Banks in chart order; a bank without a color comes after them. */
export const institutionOrder = Object.keys(cardIssuers);

export function institution(id: string): Institution {
    return cardIssuers[id] ?? others[id] ?? { name: id, code: id.toUpperCase(), color: "var(--color-mute)" };
}

/** "台新、國泰" */
export function institutionNames(ids: readonly string[]): string {
    return ids.map(id => institution(id).name).join("、");
}

/** "HSBC··4417", or just "HSBC" for rows without a card. */
export function cardLabel(institutionId: string, last4?: string | null): string {
    const { code } = institution(institutionId);
    return last4 ? `${code}··${last4}` : code;
}

/** Chart order, then the rest by id. */
export function sortInstitutions(ids: string[]): string[] {
    const rank = (id: string) => {
        const i = institutionOrder.indexOf(id);
        return i === -1 ? institutionOrder.length : i;
    };
    return ids.toSorted((a, b) => rank(a) - rank(b) || a.localeCompare(b));
}
