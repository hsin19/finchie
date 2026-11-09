# ADR 0020: Units and prices

## Decision

A unit has been text on every row that holds an amount (ADR 0013), and three tables have said what one is worth, each in its own way: exchange rates by unit (ADR 0015), closes by a security's name (ADR 0017), and the owner's values (ADR 0019), with the query choosing among them by the account's kind. Two things change that. A unit has properties of its own, such as a security's code, which belong to no price; and the owner is about to keep points by hand, the first data no document can rebuild, so what a unit is has to be settled before then.

### Units

`units (id, kind, symbol, code, market)`: what an amount is counted in.

| Kind | What | Symbol | Also |
|---|---|---|---|
| `money` | Currencies and coins, one kind: an exchange's TWD is the bank's TWD, and the rate feed lists both alike | ISO 4217 or the ticker, upper case | |
| `security` | A listed security | Its name as statements print it (ADR 0017) | `code` and `market` (`twse`, `tpex`) from the exchanges' lists |
| `points` | A rewards program | Its name as its source prints it, or as the owner types it (ADR 0019) | |

- A unit is identified by kind and symbol: the same text in two kinds is two units, so a security and a coin can never be taken for each other.
- Like accounts (ADR 0013), a unit is found or created when a source first reports it, its kind by what reports it: card and deposit statements and exchanges report money, securities statements securities, bills' points tables and the owner's points accounts points. A unit is never deleted.
- `balances`, `transactions`, `expiries`, and `valuations` hold a `unit_id` in place of text. The API still shows the symbol, and a security's code beside it.
- A program the owner names with a symbol a points unit has already is that unit: the same program (ADR 0019). The web app says so before it is saved.

### Prices

`prices (unit_id, as_of, amount, quote_unit_id, quote_amount, source)`: `amount` of the unit was worth `quote_amount` of the quote unit at `as_of`.

| Source | Writes | `as_of` | As given |
|---|---|---|---|
| `fawaz` (ADR 0015) | Each money unit some account holds or held, every fetch | The feed's day, at its start in Taipei | `per_twd` of the unit = 1 TWD |
| `twse`, `tpex` (ADR 0017) | Each security some account holds or held | The trading day's close, 13:30 Taipei | 1 = the close in TWD |
| `max`, `binance`, `bitget` (ADR 0018) | Each coin the exchange's snapshot holds, at the exchange's own market | The fetch | 1 = the last price in TWD (MAX) or USDT |
| `owner` (ADR 0019) | What the owner says one unit is worth | When said | 1 = the value, in a unit that has a price (NT$ unless the owner picks another) |

- **The newest price counts, whatever its source**: a later price is fresher information, and there is no reason to doubt it. `as_of` is when the price held, not when Finchie stored it: fetching the same day's rate again does not displace what the owner said that afternoon; the next day's rate does.
- `source` says where a price came from, for showing it and for clearing the owner's; it never decides which price counts.
- **A balance's worth** in NT$ is its amount at its unit's newest price. When that price's quote unit is not NT$, the quote unit's own newest price in NT$ converts it: one step, never a chain. A unit without a price, or priced in a unit that has none, has no worth.
- **Exact**: a price is kept as its source gives it, both amounts (ADR 0004). Nothing is divided until a worth is worked out, which is then rounded to the cent. Fawaz prints how much of a unit one NT$ buys, whose inverse is rarely a finite decimal; exchanges price coins in USDT.
- **One per unit, source, and day**: a later write the same day replaces the day's, so history stays a price a day for whoever needs it later, such as net worth over time. Clearing the owner's value of a unit removes the owner's prices of it.

### Collisions

- Kinds never collide: a unit is its kind and symbol.
- A ticker can mean different coins at the rate feed and at an exchange. An exchange's own price, newer than the feed's day, counts for the coins it holds, so the feed's reading of a ticker matters only where nothing fresher prices it; prices are references for comparing either way (ADR 0015).
- The owner names points units only, and a symbol already in use is the same program, asked first.
- A renamed security is a new unit, as before (ADR 0017); its code could later join the two, not now.

### What this changes

| ADR | Was | Now |
|---|---|---|
| 0013 | A unit is text on each row | A row refers to a unit |
| 0015 | `exchange_rates`, every unit the feed lists | Prices from `fawaz`, for money units some account holds or held |
| 0017 | `quotes` by name, worth chosen by the account's kind | Prices from `twse` and `tpex`; code and market on the unit |
| 0018 | Coins the feed does not list have no worth of their own | Each coin's price at the exchange, kept with the snapshot |
| 0019 | `unit_values`, counting only for a unit nothing else prices | Prices from `owner`, for any unit, counting while they are the newest |

### Migration

Points (ADR 0019) were not released yet, so their migration is rewritten to follow this one rather than changed after it.

- **Units and prices** create the units from every unit in use, each's kind by the kind of the account it is in (card, deposit, and exchange accounts hold money, securities accounts securities); turn the rows of `exchange_rates` and `quotes` into prices for units that exist, dropping the rest; and replace each unit column with its `unit_id`. The owner's details on accounts stay.
- **Points** follow, their expiries holding a `unit_id` from the start, with no `unit_values`. It gives `小樹點(信用卡)`, `e point`, and `台新Point(信用卡)` the owner's price of NT$1 a point as of 2000-01-01: what the owner counts them as, and older than any price that may come later.
- Bills stored before this get their points when re-parsed, as ADR 0019 says.

## Alternatives considered

- **Units as text with a kind in it** (`security:台積電`). Identity inside a string, which every source and query would have to spell alike.
- **A table of prices per source**, as before. Every query that needs a worth must know them all, and an order among them.
- **Prices by source priority** (automatic first, as ADR 0019 had it). A value the owner sets for a unit the feed lists would never count, and an exchange's fresh price would lose to the feed's day-old rate.
- **The latest stored price wins.** Fetching the same day's rate again, every six hours, would displace a newer price the owner gave.
- **Prices as NT$ per unit only.** The feed's rates would have to be inverted and rounded, and an exchange's USDT prices converted at some rate before they are stored.
- **Chains of quotes** (BGB in USDT, USDT in USD, USD in NT$). One step covers every source, and more would need guarding against cycles.
- **Every unit the feed lists, every day** (ADR 0015 kept them). About 340 a day, mostly units no account ever held; a unit enters prices once some account holds it.

## Consequences

- The backend gains `units` and `prices` in place of `exchange_rates`, `quotes`, and `unit_values`; the rows that held a unit hold `unit_id`. The jobs and the ingest find or create units and write prices.
- The exchanges' adapters return each coin's price with the snapshot.
- The API's balance tells its unit's id and its price: when it held, its source, what one unit came to in the quote unit and in NT$, in place of `rateDay`, `price`, and `unitValue`. `GET /api/units` lists the units, and `PUT` and `DELETE /api/units/{id}/price` set and withdraw the owner's.
- The account page lists every unit an account holds with its price and where it came from, and takes the owner's in NT$ for any of them; the API takes another money unit too.
- A price the owner sets for a unit a source also prices counts only until a newer one comes; for most money units, the next day.
