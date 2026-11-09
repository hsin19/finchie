# ADR 0015: Exchange rates

## Decision

Balances in different units are compared, sorted, and later added up in NT$. The backend keeps one rate per unit and day, fetched from Fawaz Ahmed's currency API, the feed InTheGreenYet used: free, without a key, about 340 units including stablecoins and other crypto, published daily on jsDelivr with a Cloudflare Pages mirror.

| Part | |
|---|---|
| Fetching | `fetch_exchange_rates`, a River periodic job every 6 hours and at start. It reads `twd.json` from jsDelivr, else from the mirror, and keeps that day's rates. When both fail, River retries and the newest stored rates stand meanwhile |
| Storage | `exchange_rates (day, unit, per_twd)`: how much of the unit one NT$ buys, exactly as the feed prints it, read as a decimal, never a float (ADR 0004) (ADR 0020: kept as prices from `fawaz`, as of the start of the feed's day, for the money units some account holds or held) |
| Use | A balance's worth is its amount divided by the newest rate of its unit; NT$ is worth itself. A unit with no rate, such as a points program's, has no worth and sorts last. The API says which day's rate it used |

Rates are references for comparing, not what a bank would pay. They are never stored on balances or lines, which stay as reported (ADR 0013).

## Alternatives considered

- **The rates statements print.** Several banks print one beside each foreign balance, but not all, each its own and as of its own closing date: no common rate to compare two banks' dollars by.
- **Fetch in the browser, as InTheGreenYet did.** Nothing to run, but no history, a cache per browser, and net worth over time needs rates on the server.
- **A keyed API.** A key to keep and renew is the kind of chore Finchie avoids (`AGENTS.md`).

## Consequences

- The backend calls `cdn.jsdelivr.net` and `currency-api.pages.dev`. Without outbound access, only NT$ balances have a worth.
- ADR 0013's net worth needs nothing more than these rates for money; points still need their worth.
