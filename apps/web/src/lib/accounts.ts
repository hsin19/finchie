import type {
    Account,
    AccountKind,
    Balance,
    Position,
    Price,
    PriceSource,
    Transaction,
} from "#lib/api/client.js";
import {
    cardLabel,
    institution,
    OWNER,
} from "#lib/institutions.js";
import { accountKinds } from "#lib/kinds.js";
import {
    formatAmount,
    formatPrice,
    negate,
    roundWhole,
    sign,
    subtract,
} from "#lib/money.js";
import { resolve } from "$app/paths";
import type { ResolvedPathname } from "$app/types";

/** The owner's name for an account, else the bank's ("臺幣活存"), else its kind's. */
export function accountName(account: Pick<Account, "kind" | "label" | "name">): string {
    return account.name ?? account.label ?? accountKinds[account.kind].label;
}

/** Where a line is: "ESUN··3381" for a card, "ESUN 臺幣活存" for a deposit account, "SKIS 證券" for a brokerage. */
export function lineAccount(t: Pick<Transaction, "institution" | "cardLast4" | "accountKind" | "accountName">): string {
    if (t.accountKind === "credit_card") return cardLabel(t.institution, t.cardLast4);
    return `${institution(t.institution).code} ${t.accountName ?? accountKinds[t.accountKind].label}`;
}

/** An account the owner keeps by hand (ADR 0019). */
export function isOwned(account: Pick<Account, "institution">): boolean {
    return account.institution === OWNER;
}

export type AccountSort = "bank" | "balance";

/** By worth unless the URL asks for the bank order. */
export function accountSort(value: string | null): AccountSort {
    return value === "bank" ? "bank" : "balance";
}

/** /accounts: in which order (?sort=bank), and whether small accounts and balances are listed (?all). */
export function readAccountsView(params: Pick<URLSearchParams, "get" | "has">): { sort: AccountSort; all: boolean; } {
    return { sort: accountSort(params.get("sort")), all: params.has("all") };
}

export function accountsHref({ sort, all }: { sort: AccountSort; all: boolean; }): ResolvedPathname {
    const query = [sort === "bank" ? "sort=bank" : "", all ? "all" : ""].filter(Boolean).join("&");
    return query ? resolve(`/accounts?${query}`) : resolve("/accounts");
}

/**
 * By worth in NT$ either way, largest first (ADR 0015): the largest debt first among liabilities, whose
 * worth is negative (ADR 0022). Or by bank, as the API lists them. An account without a worth, holding
 * a unit with no rate, goes last; ties keep the bank order.
 */
export function sortAccounts(accounts: Account[], by: AccountSort): Account[] {
    if (by === "bank") return accounts;
    return [...accounts].sort((a, b) => {
        if (a.worth === undefined || b.worth === undefined) return a.worth === undefined ? (b.worth === undefined ? 0 : 1) : -1;
        return sign(subtract(magnitude(b.worth), magnitude(a.worth)));
    });
}

function magnitude(value: string): string {
    return sign(value) < 0 ? negate(value) : value;
}

/** Below this worth in NT$, either way, an account or a balance is small: the accounts page leaves it out unless ?all. */
export const SMALL_WORTH = "1000";

/** How many balances an account lists while small ones are hidden. */
export const LISTED_BALANCES = 3;

function small(worth: string): boolean {
    return sign(subtract(magnitude(worth), SMALL_WORTH)) < 0;
}

/**
 * An account worth less than NT$1,000 either way; one whose worth is not known is not. Gaps do not keep one
 * listed: the page names every account with gaps above the list.
 */
export function isSmallAccount(account: Account): boolean {
    return account.worth !== undefined && small(account.worth);
}

export interface ListedBalances {
    listed: Balance[];
    /** Balances with a worth left out, and the largest worth among them. */
    others: number;
    othersAtMost?: string;
    /** Balances left out for having no rate. */
    unpriced: number;
}

/**
 * The balances an account lists, largest worth first; one at 0 is none at all. Hiding small ones keeps the
 * three largest known to be worth NT$1,000 or more, and tells what it leaves out: how many others, the
 * most any of them is worth, and how many have no rate. A balance worth nothing once rounded is not
 * counted either.
 */
export function listedBalances(account: Account, hideSmall: boolean): ListedBalances {
    const sorted = holdingsByWorth(account.balances.filter(b => sign(b.amount) !== 0));
    if (!hideSmall) return { listed: sorted, others: 0, unpriced: 0 };
    const priced = sorted.filter(b => b.worth !== undefined);
    if (priced.length === 0) {
        // Nothing to weigh by worth, such as a points account's programs (ADR 0019): the first few.
        const listed = sorted.slice(0, LISTED_BALANCES);
        return { listed, others: 0, unpriced: sorted.length - listed.length };
    }
    const listed = priced.filter(b => !small(b.worth!)).slice(0, LISTED_BALANCES);
    const others = priced.filter(b => !listed.includes(b) && sign(b.worth!) !== 0);
    return { listed, others: others.length, othersAtMost: others[0]?.worth, unpriced: sorted.length - priced.length };
}

/** "另 11 種 ≤ NT$3,193、32 種沒有價格", or "" when nothing is left out. */
export function leftOut({ others, othersAtMost, unpriced }: ListedBalances): string {
    const parts: string[] = [];
    if (others > 0 && othersAtMost !== undefined) parts.push(`${others} 種 ≤ NT$${formatAmount(roundWhole(othersAtMost))}`);
    if (unpriced > 0) parts.push(`${unpriced} 種沒有價格`);
    return parts.length > 0 ? `另 ${parts.join("、")}` : "";
}

/**
 * "另 9 個 ≤ NT$874": the small accounts a kind's list leaves out, as leftOut says of an account's balances:
 * how many, and the most any of them is worth either way. One worth nothing is none at all; "" when that
 * leaves none.
 */
export function leftOutAccounts(hidden: readonly Account[]): string {
    const worths = hidden.flatMap(a => a.worth !== undefined && sign(a.worth) !== 0 ? [magnitude(a.worth)] : []);
    if (worths.length === 0) return "";
    const most = worths.reduce((a, b) => sign(subtract(b, a)) > 0 ? b : a);
    return `另 ${worths.length} 個 ≤ NT$${formatAmount(roundWhole(most))}`;
}

/** Holdings by worth in NT$, largest first; those without a rate go last, by unit. */
export function holdingsByWorth(balances: Balance[]): Balance[] {
    return [...balances].sort((a, b) => {
        if (a.worth === undefined || b.worth === undefined) {
            if (a.worth !== b.worth) return a.worth === undefined ? 1 : -1;
            return a.unit.localeCompare(b.unit);
        }
        return sign(subtract(b.worth, a.worth)) || a.unit.localeCompare(b.unit);
    });
}

/**
 * A snapshot history by day, newest first, each day's units in the order the API lists them (ADR 0018).
 * A unit at 0 is one sold out that day.
 */
export function historyByDay(history: Balance[]): { asOf: string; balances: Balance[]; }[] {
    const days = new Map<string, Balance[]>();
    for (const b of history) days.set(b.asOf, [...(days.get(b.asOf) ?? []), b]);
    return [...days].sort(([a], [b]) => b.localeCompare(a)).map(([asOf, balances]) => ({ asOf, balances }));
}

/** Where a price came from (ADR 0020). */
export const priceSources: Record<PriceSource, string> = {
    fawaz: "匯率",
    twse: "證交所收盤",
    tpex: "櫃買收盤",
    max: "MAX",
    binance: "Binance",
    bitget: "Bitget",
    owner: "自訂",
};

const taipei = new Intl.DateTimeFormat("sv-SE", {
    timeZone: "Asia/Taipei",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
});

/** When a price held, in Taipei: its day, and its time unless it is the start of the day (a rate's). */
export function pricedAt(price: Price): string {
    return taipei.format(new Date(price.asOf)).replace(/ 00:00$/, "");
}

/**
 * A unit's price in words (ADR 0020): "1 USD ≈ NT$32.85 · 匯率 2026-11-07", or through its quote unit,
 * "1 BGB = 0.85 USDT ≈ NT$27.93 · Bitget 2026-11-09 14:00".
 */
export function describePrice(unit: string, price: Price, base: string): string {
    const quoted = price.quote === base ? `≈ NT$${formatPrice(price.quoted)}` : `= ${formatPrice(price.quoted)} ${price.quote}`;
    const twd = price.quote === base ? "" : price.twd !== undefined ? ` ≈ NT$${formatPrice(price.twd)}` : `（${price.quote} 沒有新臺幣價格）`;
    return `1 ${unit} ${quoted}${twd} · ${priceSources[price.source]} ${pricedAt(price)}`;
}

/**
 * The number an account shows: the owner's full one where they gave it, else the one its source prints; ""
 * for neither, as for an account the owner keeps until they give it one.
 */
export function shownNumber(account: Pick<Account, "number" | "fullNumber">): string {
    return account.fullNumber ?? account.number;
}

/** Whether points lapse within this many days of `today` (YYYY-MM-DD): soon enough to stand out. */
export function lapsesSoon(by: string, today: string, days = 60): boolean {
    const limit = new Date(`${today}T00:00:00Z`);
    limit.setUTCDate(limit.getUTCDate() + days);
    return by <= limit.toISOString().slice(0, 10);
}

/**
 * A statement comes monthly, and up to about three weeks after it closes (台新's took 16 days in 2026,
 * 中信's 20 after month end): past this, one that should have come has not.
 */
const STATEMENT_DAYS = 50;
/** An exchange is read every few hours (ADR 0018). */
const SNAPSHOT_DAYS = 1;

export interface AccountStatus {
    /** The day of its newest balance, and how many days before `today` that was. */
    asOf?: string;
    days?: number;
    /** Nothing new for longer than its source takes. Why is not said: a bank sends nothing for a month without a line. */
    stale: boolean;
    /** Why an old balance is still fine. */
    note?: string;
    /** Stretches where its balance moved and no line says why. */
    gaps: number;
    /** Whether its balances can be checked against its lines at all. */
    checked: boolean;
}

/** How current an account is and whether it joins up. One the owner keeps is as old as they leave it. */
export function accountStatus(account: Account, today: string): AccountStatus {
    const status: AccountStatus = { stale: false, gaps: account.gaps.length, checked: account.continuity === "checked" };
    const asOf = account.balances.map(b => b.asOf).sort().at(-1);
    if (!asOf) return status;
    const days = Math.round((Date.parse(today) - Date.parse(asOf)) / 86_400_000);
    status.asOf = asOf;
    status.days = days;
    if (isOwned(account)) status.note = "手動";
    else status.stale = days > (account.kind === "exchange" ? SNAPSHOT_DAYS : STATEMENT_DAYS);
    return status;
}

/** "今天", "昨天", "41 天前". */
export function daysAgo(days: number): string {
    return days <= 0 ? "今天" : days === 1 ? "昨天" : `${days} 天前`;
}

// ---- What an account's page shows, by kind

/**
 * The parts of an account's page. Each kind says what it has in one place (`pages`), so a new kind is
 * a type error until it is described, rather than a branch missed somewhere in the page.
 */
export interface AccountPage {
    /** The header leads with the account's worth (shares, coins), or with its balances. */
    headline: "worth" | "balances";
    /** Said in place of a worth that is not known. */
    noWorth: string;
    /** A table of what it holds and each one's worth: an exchange's coins (ADR 0018). */
    holdings: boolean;
    /** Each security's shares, cost, and gains (ADR 0017). */
    positions: boolean;
    /** What will lapse by when (ADR 0019). */
    expiring: boolean;
    /** Its lines, and what their amounts mean; none for a source that reports no lines. */
    lines?: { note: string; trade: boolean; };
    /** Its history: a balance a day (snapshots), or one per statement. */
    history: "days" | "statements";
    /** What a gap in its balances most likely means. */
    gaps: string;
    /** Why its balances are not checked against its lines, when they are not. */
    unchecked: string;
    /** The owner keeps it by hand (ADR 0019): it takes what it holds, a row a unit. */
    form: boolean;
}

const MISSING_STATEMENT = "這兩個日期之間有錢進出、卻沒有任何明細：多半是少了一期對帳單。上傳那期之後就會接起來。";
const NO_LINES = "來源只給餘額、沒有明細，接不起來";

const pages: Record<AccountKind, Omit<AccountPage, "form">> = {
    deposit: {
        headline: "balances",
        noWorth: "",
        holdings: false,
        positions: false,
        expiring: false,
        lines: { note: "存入是正數", trade: false },
        history: "statements",
        gaps: MISSING_STATEMENT,
        unchecked: NO_LINES,
    },
    // A liability (ADR 0022): what each bill says is owed, on its closing date; its lines are the bills'.
    credit_card: {
        headline: "balances",
        noWorth: "",
        holdings: false,
        positions: false,
        expiring: false,
        lines: { note: "照帳單印的：消費是正數，繳款與退款是負數", trade: false },
        history: "statements",
        gaps: MISSING_STATEMENT,
        // A bill dates its lines by purchase, which can fall before the bill before it closed (ADR 0022).
        unchecked: "每期帳單自己對帳，不接前後期的餘額",
    },
    // A liability (ADR 0022): the principal outstanding, as the owner says, until a loan statement is parsed.
    loan: {
        headline: "balances",
        noWorth: "",
        holdings: false,
        positions: false,
        expiring: false,
        lines: { note: "照對帳單印的", trade: false },
        history: "statements",
        gaps: MISSING_STATEMENT,
        unchecked: NO_LINES,
    },
    // Shares, one unit per security; its lines are trades (ADR 0017).
    securities: {
        headline: "worth",
        noWorth: "有證券沒有收盤價",
        holdings: false,
        positions: true,
        expiring: false,
        lines: { note: "買進的股數是正數", trade: true },
        history: "statements",
        gaps: MISSING_STATEMENT,
        unchecked: NO_LINES,
    },
    // Coins, and nothing but snapshots of them (ADR 0018).
    exchange: {
        headline: "worth",
        noWorth: "有幣種沒有價格",
        holdings: true,
        positions: false,
        expiring: false,
        history: "days",
        gaps: MISSING_STATEMENT,
        unchecked: NO_LINES,
    },
    // Its bills print its programs' balances, their totals as lines, and what will lapse (ADR 0019).
    points: {
        headline: "balances",
        noWorth: "",
        holdings: false,
        positions: false,
        expiring: true,
        lines: { note: "帳單上每一期的合計，獲得是正數", trade: false },
        history: "statements",
        gaps: "這兩期之間少了點數，帳單卻沒列出：多半是點數過期了（過期的點數不會出現在任何欄位），或少了一期帳單。",
        unchecked: NO_LINES,
    },
};

/** What an account's page shows: its kind's parts; one the owner keeps has no lines, and a form. */
export function accountPage(account: Pick<Account, "kind" | "institution">): AccountPage {
    const owned = isOwned(account);
    const page = pages[account.kind];
    return { ...page, lines: owned ? undefined : page.lines, form: owned };
}

/** Kinds an owner can keep by hand: every one but a card's, which its bank's bills report (ADR 0022). */
export const ownedKinds: AccountKind[] = ["deposit", "securities", "exchange", "points", "loan"];

/** A securities account's positions: held first, then those sold out whose sales are known to have gained or lost. */
export function shownPositions(positions: Position[] = []): Position[] {
    const held = positions.filter(p => sign(p.shares) !== 0);
    const soldOut = positions.filter(p => sign(p.shares) === 0 && p.realized !== undefined && sign(p.realized) !== 0);
    return [...held, ...soldOut];
}
