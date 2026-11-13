// Amounts arrive as decimal strings ("-6552.97", ADR 0004) and stay exact: they are added as scaled
// integers, never as floating point.

interface Fixed {
    units: bigint;
    scale: number;
}

function parse(value: string): Fixed {
    const match = /^([+-]?)(\d+)(?:\.(\d+))?$/.exec(value.trim());
    if (!match) throw new Error(`not a decimal amount: ${JSON.stringify(value)}`);
    const [, sign, whole, fraction = ""] = match;
    const units = BigInt(whole + fraction);
    return { units: sign === "-" ? -units : units, scale: fraction.length };
}

function rescale(value: Fixed, scale: number): bigint {
    return value.units * 10n ** BigInt(scale - value.scale);
}

function render({ units, scale }: Fixed): string {
    const negative = units < 0n;
    const digits = (negative ? -units : units).toString().padStart(scale + 1, "0");
    const whole = digits.slice(0, digits.length - scale);
    const fraction = scale > 0 ? `.${digits.slice(-scale)}` : "";
    return `${negative ? "-" : ""}${whole}${fraction}`;
}

export function sum(values: Iterable<string>): string {
    let total: Fixed = { units: 0n, scale: 0 };
    for (const value of values) {
        const next = parse(value);
        const scale = Math.max(total.scale, next.scale);
        total = { units: rescale(total, scale) + rescale(next, scale), scale };
    }
    return render(total);
}

export function negate(value: string): string {
    const parsed = parse(value);
    return render({ units: -parsed.units, scale: parsed.scale });
}

export function subtract(a: string, b: string): string {
    return sum([a, negate(b)]);
}

export function sign(value: string): -1 | 0 | 1 {
    const { units } = parse(value);
    return units < 0n ? -1 : units > 0n ? 1 : 0;
}

/** `a` times `b`, exactly, every decimal kept: an amount of points at a price. ("1234.5", "0.03") → "37.035". */
export function multiply(a: string, b: string): string {
    const x = parse(a);
    const y = parse(b);
    return render({ units: x.units * y.units, scale: x.scale + y.scale });
}

/**
 * `value` divided by a whole number `n`, to `places` decimals, halves away from zero: an average.
 * ("2000", 3) → "667", ("-10", 4, 1) → "-2.5".
 */
export function divide(value: string, n: number, places = 0): string {
    if (!Number.isSafeInteger(n) || n <= 0) throw new Error(`not a positive whole divisor: ${n}`);
    const { units, scale } = parse(value);
    // Scaled to `places` decimals and one more, then rounded as roundTo does.
    const extra = Math.max(0, places + 1 - scale);
    const quotient = (units * 10n ** BigInt(extra)) / BigInt(n);
    const remainder = (units * 10n ** BigInt(extra)) % BigInt(n);
    // The digits below the quotient's last only matter for an exact half: a remainder tips it over.
    const tipped = remainder === 0n ? quotient : quotient * 10n + (units < 0n ? -1n : 1n);
    return roundTo(render({ units: tipped, scale: scale + extra + (remainder === 0n ? 0 : 1) }), places);
}

/** To the nearest whole unit, halves away from zero: "296380.87" → "296381", "-2.5" → "-3". */
export function roundWhole(value: string): string {
    return roundTo(value, 0);
}

/** To `places` decimals, halves away from zero: ("32.8538", 2) → "32.85"; fewer decimals stay as they are. */
export function roundTo(value: string, places: number): string {
    const { units, scale } = parse(value);
    if (scale <= places) return render({ units, scale });
    const factor = 10n ** BigInt(scale - places);
    const magnitude = ((units < 0n ? -units : units) * 2n + factor) / (2n * factor);
    return render({ units: units < 0n ? -magnitude : magnitude, scale: places });
}

/**
 * A price for display: two decimals from 1 up, else four significant digits, which a coin worth a
 * fraction of a cent needs: "32.8538370571241164" → "32.85", "0.000144510061" → "0.0001445".
 */
export function formatPrice(value: string): string {
    const { units, scale } = parse(value);
    const magnitude = units < 0n ? -units : units;
    const whole = magnitude / 10n ** BigInt(scale);
    if (whole > 0n || magnitude === 0n) return formatAmount(roundTo(value, 2));
    // Leading zeros of the fraction, then four digits.
    const leading = scale - magnitude.toString().length;
    return formatAmount(roundTo(value, leading + 4));
}

/** For proportions only (bar widths, percentages); never for arithmetic on money. */
export function approx(value: string): number {
    return Number(value);
}

/**
 * Display form: thousands separators, whole amounts without decimals, others with at least two.
 * "23104" → "23,104", "-35.5" → "-35.50", "780.00" → "780". `signed` adds "+" to positive amounts.
 */
export function formatAmount(value: string, { signed = false } = {}): string {
    const { units, scale } = parse(value);
    const negative = units < 0n;
    const digits = (negative ? -units : units).toString().padStart(scale + 1, "0");
    const whole = digits.slice(0, digits.length - scale).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
    let fraction = scale > 0 ? digits.slice(-scale).replace(/0+$/, "") : "";
    if (fraction) fraction = `.${fraction.padEnd(2, "0")}`;
    const prefix = negative ? "-" : signed && units > 0n ? "+" : "";
    return `${prefix}${whole}${fraction}`;
}
