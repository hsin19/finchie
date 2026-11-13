# ADR 0023: Net worth over time

## Decision

What the owner had on any day since Finchie started is worked out from what it already keeps, never stored: each account's newest balance on or before the day (with the lines after it up to the day, the owner's fill of a gap among them: migration 00021), each unit's newest price on or before the end of that day in Taipei, and each account's worth as on any day (`domain.Assess`, ADR 0022 for liabilities). `GET /api/net-worth?on=` gives one day, `GET /api/net-worth/history?from=&to=&step=` a series, by day or by month end.

- **One price rule for every day.** `price_at(unit, moment)` (migration 00020) is a unit's newest price at a moment, from any source, through its quote unit's newest price at that moment when that one is in NT$ (ADR 0020). The newest prices are `price_at` at no moment in particular; a day's are `price_at` at its end.
- **History is never deleted.** Balances and prices are kept as their sources gave them (ADR 0020); a price the owner withdraws is kept, marked withdrawn, and counts for the days before it was (migration 00019).
- **Exact as of.** Sources report on days of their own (ADR 0022), so a day's figure is exact only once every account has reported on or after it. `completeAsOf` is the newest such day: the oldest of the accounts' newest reports, leaving out accounts the owner keeps by hand, which report when they do, and accounts that hold nothing. A figure for a later day is an estimate, and the accounts that have not reported since are named.
- **From when it started.** Nothing is fetched backwards: a balance older than its unit's first price has no worth, and the account counts as unvalued that day.

## Alternatives considered

- **Store a daily snapshot of net worth.** Simple to read, but stale whenever a statement arrives late or is parsed again, which is how statements arrive.
- **Fetch historical prices for statements older than Finchie.** Possible for rates and closes, but nothing asks for it yet.
- **Advance a card account's balance by payments seen since its bill.** See ADR 0022.
