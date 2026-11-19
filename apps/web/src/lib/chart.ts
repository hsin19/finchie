// The arithmetic the column charts share. Drawing only: amounts on screen still go through #lib/money.js.

/** A round step (1, 2, or 5 × 10ⁿ) that splits `longest` into about `count` steps. */
export function niceStep(longest: number, count: number): number {
    const power = 10 ** Math.floor(Math.log10(longest / count));
    return [1, 2, 5].map(f => f * power).find(s => s >= longest / count) ?? 10 * power;
}

/** An axis label: "2K" for 2000, "-500" for -500. */
export function compact(n: number): string {
    return Math.abs(n) >= 1000 ? `${n / 1000}K` : String(n);
}
