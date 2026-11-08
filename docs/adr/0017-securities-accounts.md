# ADR 0017: Securities accounts

## Decision

A brokerage account is an account (ADR 0013) of kind `securities`: what it holds is shares, one unit per security. Finchie reads Shin Kong Securities' monthly statement (證券月對帳單), the source the owner chose; its daily trade reports (證券帳務回報通知) are not read. The exchanges' daily closing prices give the holdings a worth in NT$, next to the deposit accounts.

### The monthly statement

| Part | |
|---|---|
| E-mail | From `ebill.sk88.com.tw`, subject `新光證券115/07證券月對帳單`, sent only for months with trades (ADR 0013). The PDF opens with the national ID, letters upper case, as the e-mail says |
| Trades (交易資料明細) | Per trade: the date (month and day), the security's name, shares, price, category (集買, 集賣, …), amount (價金), fee, tax, and the net paid or received (應收付, marked `(付)` or `(收)`). A buy paid in advance (預繳) prints 0 to pay, with the amount prepaid (預繳金) on the row below. A 合計 row totals each column |
| Holdings (庫存資料明細) | On the month's last day: each security's name and its shares, in four columns: ordinary (普通), margin (融資), short (融券), pledged (借貸擔保品) |
| Not printed | Codes, prices, market values, cash. The money settles in a bank account (交割戶), one net amount per day |

The parser reconciles its trades against the 合計 row: amounts, fees, taxes, and the net paid or received must each add up (ADR 0005). The owner trades ordinary shares only. A statement with margin, short, or pledged shares raises a layout error until one has been seen; reading them as ordinary shares would be wrong.

### Lines, balances, units

| Part | |
|---|---|
| Account | One per brokerage account, identified by the masked number the statement prints beside the owner's name; institution `skis` |
| Unit | The security's name as the statement prints it, Unicode-normalized (the PDF's text writes 一 as the compatibility form ⼀): `台積電`, `統一`. Names, not codes, because the statement prints no code; the exchanges' lists give a name's code for display |
| Line | A trade: shares bought (positive) or sold (negative) in the security's unit, kind `buy` or `sell`. Its money (price, amount, fee, tax, net paid or received, prepaid) goes into `trades`, one row per line, since no other kind of line has it |
| Balance | Each holding, on the statement's holdings date. The holdings are the whole account: a security held before and not listed now is at 0 on that day |

Continuity (ADR 0013) then checks shares. Between two statements, trades must explain every change. Stock dividends (配股), splits, capital reductions, a renamed security, and a missing statement are not trades, so each shows as a gap.

### Prices

`fetch_quotes`, a River periodic job that runs at start and every 6 hours like the exchange rates (ADR 0015), reads the day's closing prices of the listed (TWSE `exchangeReport/STOCK_DAY_ALL`) and OTC (TPEx `tpex_mainboard_daily_close_quotes`) securities. Both lists are free and need no key; each row has a code, a name, and a close. The job keeps the rows for names some securities account holds or held: `quotes (day, name, code, market, close)`. A holding's worth is its shares times the newest close of its name, in NT$. A name neither list prints (delisted, renamed) has no worth and sorts last, like a unit without a rate.

### What it is for

- Net worth adds the securities' worth.
- A trade is neither spending nor income: money became shares, or shares became money. Search lists trades but counts them as neither, like transfers (ADR 0016); their fees and taxes count against the position's return.
- A position's cost is the shares' average cost. A buy adds its amount and fee. A sale takes the average cost of the shares it sold off the cost; its gain is the sale's amount, less fee and tax, minus that cost. Cost is known only for positions whose buys the statements cover. A position bought before the oldest statement has no cost, never an estimated one. Return per position and per account is InTheGreenYet's measure, knowing where the owner truly stands, applied to securities.

### The money side

The settlement account is at Shin Kong Bank, whose statements Finchie does not read: its e-mail carries no document, only a link to a page that asks for the national ID. Shin Kong Bank is to merge into Taishin Bank, expected on 2027-01-01, and Finchie reads Taishin's statements. Until then Finchie does not see a trade's money or a security's cash dividends. A link between a day's net amount and its settlement line (ADR 0016) is designed once that account is read.

## Alternatives considered

- **Read the daily trade reports too.** They print codes and the settlement date, but they are one document per trading day for what the monthly statement also has, and the owner wants no day-by-day updates yet.
- **Units as codes.** Codes outlast names, but the statement prints none. Each name would be turned into a code from the exchanges' current lists, which drop delisted securities, so one holding could get a code in one statement and not in the next.
- **Keep every quote.** About 15,000 rows a day, mostly for securities the owner never held.
- **Read Shin Kong Bank's statement page.** A page that asks for the national ID, kept for a few months, of a bank about to merge: fragile, and soon gone.
- **A broker's API or a keyed market-data API.** Credentials to keep and renew (`AGENTS.md`).

## Consequences

- `ParseDocumentResponse` gains `SecuritiesStatement securities_statement = 6` next to the card and deposit statements.
- `statements.kind` gains `securities`, transactions gain the kinds `buy` and `sell`, and the backend gains the `trades` and `quotes` tables and the quotes job. It calls `openapi.twse.com.tw` and `www.tpex.org.tw`.
- The Gmail filter is generated from the parsers' claims (ADR 0014), so once re-imported it forwards the monthly statements, and the daily reports stay out by their subject. Statements already in Gmail come in by applying the filter to matching conversations and exporting the label with Takeout.
- The accounts page lists securities accounts with their holdings and worth. The transactions page shows trades in shares.
- A corporate action shows as a gap until some source reports it.
