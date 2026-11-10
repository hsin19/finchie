// Statement and transaction dates are calendar dates ("2026-10-08", ADR 0004): no time zone, so they
// are handled as strings and UTC dates, never as local time.

const WEEKDAYS = ["日", "一", "二", "三", "四", "五", "六"];

/** "2026-10" */
export type Month = `${number}-${number}`;

export function isMonth(value: string | null): value is Month {
    return value !== null && /^\d{4}-(0[1-9]|1[0-2])$/.test(value);
}

export function monthOf(date: string): Month {
    return date.slice(0, 7) as Month;
}

export function shiftMonth(month: Month, delta: number): Month {
    const [year, m] = month.split("-").map(Number);
    const shifted = new Date(Date.UTC(year, m - 1 + delta, 1));
    return shifted.toISOString().slice(0, 7) as Month;
}

/** First and last day of the month, inclusive. */
export function monthRange(month: Month): { from: string; to: string; } {
    const last = new Date(Date.UTC(Number(month.slice(0, 4)), Number(month.slice(5, 7)), 0));
    return { from: `${month}-01`, to: last.toISOString().slice(0, 10) };
}

/** Newest first, from `newest` back to `oldest`, inclusive. */
export function monthsBetween(oldest: Month, newest: Month): Month[] {
    const months: Month[] = [];
    for (let month = newest; month >= oldest && months.length < 240; month = shiftMonth(month, -1)) {
        months.push(month);
    }
    return months;
}

/** "2026-10-08" → "10-08" */
export function shortDate(date: string): string {
    return date.slice(5);
}

export function weekday(date: string): string {
    return WEEKDAYS[new Date(`${date}T00:00:00Z`).getUTCDay()];
}

/** The owner's day, wherever the browser is (ADR 0004). */
export function today(now = new Date()): string {
    return now.toLocaleDateString("sv-SE", { timeZone: "Asia/Taipei" });
}

export function thisMonth(): Month {
    return monthOf(today());
}

/** A timestamp in local time: "11/05 17:18". */
export function shortDateTime(iso: string): string {
    const d = new Date(iso);
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function formatBytes(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
    return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
