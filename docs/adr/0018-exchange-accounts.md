# ADR 0018: Exchange accounts

## Decision

A crypto exchange is an account (ADR 0013) of kind `exchange`, one per exchange: institution `max`, `binance`, or `bitget`, number empty. Finchie reads what each one holds from the exchange's API, as InTheGreenYet did, and nothing else: trades, swaps between coins, deposits, withdrawals, and interest are not read as lines. An exchange account has balances and no lines, so its continuity is unknown (ADR 0013).

Exchanges send no statements to forward; their APIs are the only source. Each takes a read-only key without IP restriction (the backend's address is not fixed), set once in the environment; InTheGreenYet ran on such keys without renewing them. ADR 0015 and 0017 turned keyed APIs down because a free source without a key existed; here none does. An exchange whose keys are not all set is not fetched, and setting some of them but not all stops the backend at start.

Fetching is a backend job, as ADR 0002 planned for investment providers: the parser turns documents into data and makes no calls of its own, while sources Finchie calls, the rates and quotes too, are the backend's. The exchanges' code stays in one package, `internal/exchanges`, which ends in a snapshot; nothing after it knows which exchange reported one.

### Fetching

| Part | |
|---|---|
| Job | `fetch_exchange`, one River periodic job per exchange whose keys are set, at start and every 6 hours like the rates (ADR 0015) and the quotes (ADR 0017) |
| Per exchange | Each retries on its own: one that fails leaves the others' snapshots stored. Within an exchange, every call succeeds or nothing is stored: a snapshot missing a wallet would read as its coins sold |
| Day | The fetch's date in Taipei (ADR 0004). A later fetch on the same day replaces that day's snapshot |

### What each exchange reports

A snapshot is what the account holds in each unit, its holdings, and its valuation: what the whole account is worth by the exchange's own prices. An exchange keeps coins in several wallets (spot, funding, earn, futures, …), which are its bookkeeping, not the owner's, and values each of them in USDT. The holdings add up each coin across the wallets that list coins.

The exchanges leave much of this undocumented: which fields make a coin's total, whether margin is net of what is borrowed, whether an earn product also shows in spot. So a wallet is listed by coin only when its coins, at the exchange's own market prices, come to the value the exchange gives it, within 1% (or 1 USDT for a small wallet: the prices and the value are read moments apart). Otherwise, and for a wallet that lists no coins (futures, trading bots), it goes in by its value in USDT. Read wrong, a wallet loses its coins, never its worth.

| Exchange | Keys (`BACKEND_…`) | Wallets and their coins | Valuation |
|---|---|---|---|
| MAX | `MAX_ACCESS_KEY`, `MAX_SECRET_KEY` | The spot wallet only (`/api/v3/wallet/spot/accounts`): each currency's balance, locked, and staked | MAX prints no total: NT$ at MAX's own markets (`/api/v2/tickers`), `<coin>twd`, else `<coin>usdt` × `usdttwd`. A coin with neither leaves the snapshot without a valuation |
| Binance | `BINANCE_API_KEY`, `BINANCE_API_SECRET` | `/sapi/v1/asset/wallet/balance` with `needBalanceDetail`: every wallet's value and the coins it lists (free, locked, frozen, and withdrawing), priced at the spot markets (`/api/v3/ticker/price`) | The wallets' values added up |
| Bitget | `BITGET_API_KEY`, `BITGET_API_SECRET`, `BITGET_PASSPHRASE` | `/api/v2/account/all-account-balance` values each account; spot (`/api/v2/spot/account/assets`), funding (`/api/v2/account/funding-assets`), and earn (`/api/v2/earn/account/assets`) list their coins, priced at the spot markets (`/api/v2/spot/market/tickers`) | The accounts' values added up |

### Storage

- **Balances.** A snapshot's holdings are balances (ADR 0013) without a statement: `balances.statement_id` becomes nullable. A statement's balances stay unique per statement, account, unit, and day; a snapshot's are unique per account, unit, and day. A snapshot is the whole account: a unit whose newest balance is not 0 and that the snapshot does not list is at 0 on its day, and a unit at 0 is one the account no longer holds, as with securities (ADR 0017).
- **Valuations.** `valuations (account_id, as_of, unit, amount)`, one per account and day: what a source says the whole account is worth, in the unit it says it in. Any source may report one; for now only exchanges do.

### Worth

An account's worth in NT$, whatever its kind:

1. When every unit it holds has a worth (ADR 0015, ADR 0017), their sum.
2. Else its valuation on the day of its newest balance, at the newest rate of the valuation's unit.
3. Else none.

The API says which one it used. Holdings at Finchie's own rates come first, so an exchange is valued like every other account, and its worth is the sum of the holdings it lists. The valuation covers what Finchie cannot price: coins the rate feed does not list, such as small coins left over in amounts too small to sell. Statements report no valuation, so deposit and securities accounts keep their worth as before.

### What it is for

- Net worth adds the exchanges, and the accounts page lists them next to deposits and securities.
- An exchange's page shows its worth, its holdings, and a history by day; it has no lines to list.

## Alternatives considered

- **Read trades, swaps, deposits, and withdrawals as lines.** Deposits and withdrawals alone would link money sent from a bank (ADR 0016) and give a return per account (worth less what went in). A cost per coin and continuity need every kind of movement, each from endpoints of its own on each exchange: trades one pair at a time, conversions, dust conversions, earn interest, airdrops, fees. Miss one kind and every snapshot shows a gap. Much data for little use now.
- **The valuation only, as InTheGreenYet had.** Nothing would show what the account holds.
- **The valuation first.** Worth would differ from the sum of the holdings listed, priced by the exchange where Finchie has rates of its own.
- **Trust each wallet's coins as read.** Binance's and Bitget's per-coin endpoints (spot, funding, earn positions) leave which fields and products they cover undocumented; a wrong guess would count coins twice or miss some, silently.
- **Value-only wallets as units of their own** (`合約 (USDT)`). A unit is something held (ADR 0013), not a wallet's value, and no rate would ever find one.
- **An account per wallet.** Wallets are the exchange's bookkeeping; a coin moved between two would show as one account's loss and the other's gain.
- **Fetch in the parser.** It would make calls and hold keys, and stop being a function of the document it is given.

## Consequences

- `accounts.kind` gains `exchange` and `balances.statement_id` becomes nullable; the backend gains the `valuations` table and the `fetch_exchange` jobs. It calls `max-api.maicoin.com`, `api.binance.com`, and `api.bitget.com`.
- Money a deposit account sends to MAX has no line on the other side to link to, so it counts as spending (ADR 0016); coins moved between exchanges show only as changed holdings. Both wait for deposits and withdrawals as lines.
- An exchange has a worth, not a return.
- A coin the rate feed does not list (MAX's own token, BGB, most small coins) has no worth of its own, though the exchange priced it when checking its wallet; only the account's valuation counts it. Keeping each coin's price with the snapshot would give it one (ADR 0020 does: each coin held has the exchange's price, in NT$ or USDT).
- A snapshot adds one balance per unit and day: a few dozen rows a day.
- Bitget is moving accounts to its unified account, whose API is v3; an account moved over may need its endpoints changed.
- The keys join the secrets in `AGENTS.md`.
