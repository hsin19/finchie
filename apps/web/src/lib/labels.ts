import type {
    Account,
    AccountKind,
    Document,
    DocumentStatus,
    TransactionKind,
} from "#lib/api/client.js";

// Full class names, so Tailwind finds them in the source.
export const tone = {
    ink: "text-ink",
    dim: "text-dim",
    mute: "text-mute",
    accent: "text-accent",
    pos: "text-pos",
    neg: "text-neg",
} as const;
export type Tone = keyof typeof tone;

interface Institution {
    name: string;
    code: string;
    /** Chart color. Fixed per bank, so a bank keeps its color whichever others are on screen. */
    color: string;
}

// Chart colors in this order pass the color-vision checks for neighbors in a stack, on the panel color.
const institutions: Record<string, Institution> = {
    taishin: { name: "台新", code: "TSB", color: "#3987e5" },
    cathay: { name: "國泰", code: "CUB", color: "#d95926" },
    hsbc: { name: "滙豐", code: "HSBC", color: "#199e70" },
    esun: { name: "玉山", code: "ESUN", color: "#c98500" },
};

// Banks Finchie reads deposit statements from only: no card bills, so never in a spending chart.
const depositOnly: Record<string, Institution> = {
    fubon: { name: "富邦", code: "TFB", color: "var(--color-mute)" },
    ctbc: { name: "中信", code: "CTBC", color: "var(--color-mute)" },
    nextbank: { name: "將來", code: "NEXT", color: "var(--color-mute)" },
};

/** Banks in chart order; a bank without a color comes after them. */
export const institutionOrder = Object.keys(institutions);

export function institution(id: string): Institution {
    return institutions[id] ?? depositOnly[id] ?? { name: id, code: id.toUpperCase(), color: "var(--color-mute)" };
}

/** "HSBC··4417", or just "HSBC" for rows without a card. */
export function cardLabel(institutionId: string, last4?: string | null): string {
    const { code } = institution(institutionId);
    return last4 ? `${code}··${last4}` : code;
}

export const accountKinds: Record<AccountKind, { short: string; label: string; }> = {
    deposit: { short: "deposit", label: "存款" },
    credit_card: { short: "card", label: "信用卡" },
};

/** The owner's name for an account, else the bank's ("臺幣活存"), else its kind's. */
export function accountName(account: Pick<Account, "kind" | "label" | "name">): string {
    return account.name ?? account.label ?? accountKinds[account.kind].label;
}

interface Kind {
    /** Column value, terminal style. */
    short: string;
    label: string;
    tone: Tone;
}

export const kinds: Record<TransactionKind, Kind> = {
    purchase: { short: "purchase", label: "消費", tone: "ink" },
    foreign_transaction_fee: { short: "fx_fee", label: "海外手續費", tone: "neg" },
    annual_fee: { short: "annual_fee", label: "年費", tone: "neg" },
    refund: { short: "refund", label: "退款", tone: "pos" },
    rebate: { short: "rebate", label: "回饋", tone: "pos" },
    payment: { short: "payment", label: "繳款", tone: "mute" },
};

export const kindOrder: TransactionKind[] = ["purchase", "foreign_transaction_fee", "annual_fee", "refund", "rebate", "payment"];

interface Status {
    tag: string;
    label: string;
    tone: Tone;
}

/** A short word after a coloured dot ("● ok"); square brackets are for keys only. */
export const statuses: Record<DocumentStatus | "uploading" | "same" | "skipped", Status> = {
    uploading: { tag: "up", label: "上傳中", tone: "accent" },
    same: { tag: "same", label: "上傳過了", tone: "mute" },
    pending: { tag: "run", label: "解析中", tone: "accent" },
    parsed: { tag: "ok", label: "已對帳", tone: "pos" },
    needs_review: { tag: "chk", label: "需確認", tone: "neg" },
    duplicate: { tag: "dup", label: "重複帳單", tone: "mute" },
    failed: { tag: "err", label: "解析失敗", tone: "neg" },
    /** Failed because no parser handles that kind of file: nothing to fix or retry. */
    skipped: { tag: "skip", label: "不支援", tone: "mute" },
};

/** A document's tag. A failure because no parser handles the file is "skipped": nothing to retry. */
export function documentStatus(doc: Pick<Document, "status" | "error">): DocumentStatus | "skipped" {
    return doc.status === "failed" && doc.error?.startsWith("Unimplemented") ? "skipped" : doc.status;
}
