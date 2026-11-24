# ADR 0004: Money and dates

## Decision

**Amounts are never floating point.**

| Layer | Type |
|---|---|
| Postgres | `NUMERIC` (no fixed scale: crypto needs more decimals than TWD) |
| Wire (proto) | `google.type.Decimal`, a decimal string |
| Python | `decimal.Decimal` |
| Go | `pgtype.Numeric` for storage; a decimal type, never `float64`, if arithmetic is ever needed (it came with worth and net worth: exact `math/big.Rat`, converted at the edges by `internal/numeric`) |

**Each amount carries its currency** (ISO 4217 code). (Since ADR 0013 and ADR 0020, its unit in `units`: a currency or coin by its code, a security or a points program by its name.) A transaction stores the amount billed on the statement, in the statement's currency, and, for foreign transactions, the original amount and currency as printed.

**Sign follows the statement:** charges are positive, credits (payments, refunds, rebates, fee waivers) are negative.

**Dates on statements are calendar dates** with no time or time zone: `google.type.Date` on the wire, `DATE` in Postgres. Event times such as "received at" are `TIMESTAMPTZ`. ROC (Minguo) years, as in `115/09/07`, are converted to Gregorian in the parser.

## Background

V1 stored amounts as `float64`. Statements mix currencies (TWD bills with EUR, JPY, USD, AED charges), and the reconciliation in ADR 0005 compares sums exactly, which floating point cannot do reliably. Proto has no decimal scalar, so `double` was the tempting wrong choice.
