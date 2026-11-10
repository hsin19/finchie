// What kinds of accounts and lines there are, as the UI names them.

import type {
    AccountKind,
    TransactionKind,
} from "#lib/api/client.js";
import type { Tone } from "#lib/tone.js";

/**
 * In the order /accounts lists them: assets, then liabilities (ADR 0022). `worth`: the heading of what
 * accounts of the kind are worth together.
 */
export const accountKinds: Record<AccountKind, { short: string; label: string; worth: string; }> = {
    deposit: { short: "deposit", label: "存款", worth: "DEPOSITS · 存款" },
    securities: { short: "securities", label: "證券", worth: "SECURITIES · 證券市值" },
    exchange: { short: "exchange", label: "交易所", worth: "EXCHANGES · 交易所" },
    points: { short: "points", label: "點數", worth: "POINTS · 點數" },
    credit_card: { short: "card", label: "信用卡", worth: "CARDS · 信用卡" },
    loan: { short: "loan", label: "貸款", worth: "LOANS · 貸款" },
};

/** Every account kind, in the order of `accountKinds`. */
export const accountKindOrder = Object.keys(accountKinds) as AccountKind[];

interface Kind {
    /** Column value, terminal style. */
    short: string;
    label: string;
    tone: Tone;
}

/** In the order lists and filters show them. */
export const kinds: Record<TransactionKind, Kind> = {
    purchase: { short: "purchase", label: "消費", tone: "ink" },
    foreign_transaction_fee: { short: "fx_fee", label: "海外手續費", tone: "neg" },
    annual_fee: { short: "annual_fee", label: "年費", tone: "neg" },
    refund: { short: "refund", label: "退款", tone: "pos" },
    rebate: { short: "rebate", label: "回饋", tone: "pos" },
    payment: { short: "payment", label: "繳款", tone: "mute" },
    // A deposit account's lines (ADR 0013): banks print no more than these apart.
    interest: { short: "interest", label: "利息", tone: "pos" },
    fee: { short: "fee", label: "手續費", tone: "neg" },
    other: { short: "other", label: "存提與轉帳", tone: "ink" },
    // A trade, in shares (ADR 0017).
    buy: { short: "buy", label: "買進", tone: "ink" },
    sell: { short: "sell", label: "賣出", tone: "ink" },
    // A points bill's period totals, in points (ADR 0019).
    earn: { short: "earn", label: "獲得點數", tone: "ink" },
    adjust: { short: "adjust", label: "點數調整", tone: "ink" },
    redeem: { short: "redeem", label: "使用點數", tone: "ink" },
};

/** Every kind, in order: the keys of `kinds`, so neither can miss one. */
export const kindOrder = Object.keys(kinds) as TransactionKind[];

export function isKind(value: string): value is TransactionKind {
    return Object.hasOwn(kinds, value);
}
