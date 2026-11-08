# parser

Stateless gRPC service that turns a statement e-mail into structured data: a card bill (statement fields, transactions, and a reconciliation of the transactions against the printed total), or a deposit statement (each account's lines and balances by currency, reconciled through its running balances with `reconcile_account`, ADR 0013). It takes e-mails only, never a bare PDF (ADR 0014). It also opens a document for reading (`OpenDocument`, `reading.py`): the bank's headers, the body, and the attachments with the PDFs decrypted, so the web app can show them without the password (ADR 0012). The file it marks primary, the one the app opens, is the PDF parsing would take as the statement (step 3 below), or the first PDF when none is. It keeps no state and talks to nothing but its caller, the backend (ADR 0002). The contract is [`proto/finchie/parser/v1/parser.proto`](../../proto/finchie/parser/v1/parser.proto).

## How a document is parsed

1. **Read the bank's e-mail** (`documents.py`). One the owner forwarded by hand is unwrapped first: the message attached to it, or the header block of Gmail's inline forward (`---------- Forwarded message ---------`, labels in English or Chinese), innermost first. The parsers then see the bank's sender, subject, and date.
2. **Claim it** (`claimants` in `pipeline.py`). Each parser declares the sender domains (a subdomain counts) and the terms its e-mail's subject carries, all of them, case aside; the subject also tells a bank's card bill from its deposit statement. Terms, not a pattern: the Gmail filter searches for the same claims. An e-mail nobody claims is unsupported and never opened.
3. **Pick the statement** (`select_statement`). The claiming parser opens the PDFs with its own passwords only (`pdf.py`, `passwords.py`): none, then one the owner chose (`PARSER_PDF_PASSWORDS__<INSTITUTION>`), then the bank's rule built from the owner's details. It takes the PDF whose name it knows (`TSB_Creditcard_Estatement_*.pdf`) and whose first page names its bank, so payment slips are ignored; a known name that none of its passwords opens fails the document for its password; otherwise any other PDF whose first page names the bank. A rule whose detail is not set fails naming the variable to set.
4. **Parse** (`parsers/<bank>.py`). Read the statement fields and every line, classify each line (purchase, foreign transaction fee, annual fee, refund, rebate, payment), and add up the new charges to compare with the amount the statement prints.

| Parser | Password, as the bank's e-mail states it | Notes |
|---|---|---|
| `esun.credit_card` | None stated (can be turned off at the bank) | Several cards per bill; rebates listed separately |
| `esun.deposit` | National ID | Combined statement (綜合對帳單): every deposit account's balance, and the lines of those that had any. Withdrawal and deposit are told apart by column position (`Document.page_words`), since only one holds an amount; reconciled through every running balance. No opening balance is printed |
| `taishin.credit_card` | Last 2 characters of the national ID + birth month and day; up to the 113/11 bill, the national ID | Several Richart cards per bill; long descriptions wrap onto the lines around the row, never across a page break |
| `taishin.deposit` | National ID | Combined statement (綜合對帳單): NT$ and foreign currency summaries (a foreign account's currency sits half a row above it; its balance is the original currency's, not its NT$ worth), then tables of lines that carry the account number on every row and run onto the next page. An amount can print in both columns, as on a foreign currency interest line: the line is their difference. 存款息 and INTEREST are interest |
| `fubon.deposit` | National ID (or one agreed with the bank) | Bank statement (銀行對帳單): 資產持有明細 lists each account's balance; 當月交易明細 opens each account's lines with the balance brought forward (承轉結餘), its number on that row only, which serves as the opening balance. Headings are centred over their columns, so words go to the nearest heading's centre. An account number past the amounts is the counterparty |
| `nextbank.deposit` | National ID | Combined statement (綜合對帳單): a section per account and currency, its balance in the heading; one with lines prints the balance brought forward (上期餘額) as the opening balance. Lines are dated by month and day only, placed within the statement's period, 收入 before 支出, which is printed negative. A long memo wraps onto rows above and below its line's. A transfer's memo names the other side's bank and account (玉山銀行-…), which becomes the counterparty with the bank's code where known. Pockets (活存口袋) are accounts of their own, by serial number; a pocket's lines raise a layout error until one has been seen. 利息存入 is interest |
| `skis.securities` | National ID | Shin Kong Securities' monthly statement (證券月對帳單, ADR 0017); its daily trade reports (帳務回報通知) are not claimed. Trades are read by column position, and checked against the 合計 row column by column (價金, 手續費, 交易稅, 應收付金額). A buy paid in advance prints 0 to pay and its 預繳金 on the row below. Holdings, two to a row, are as of the day the statement prints, a few days into the next month, counting that day's trades. Names are Unicode-normalized (the text layer writes 一 as ⼀). Margin, short selling, and pledged shares raise a layout error until one has been seen |
| `ctbc.deposit` | National ID (can be changed or turned off at the bank) | Bank statement (對帳單), mid-month to mid-month. Its Chinese text has no Unicode mapping: digits, dates, and account numbers come from the text layer, everything in Chinese (section titles, account types, currencies by name, descriptions, the 上期結餘 and 本期結餘 rows) from OCR of the whole page, matched to text-layer rows by height. Each NT$ account's lines run from the balance brought forward to 本期結餘, which must equal its holding. Needs OCR; foreign currency lines raise a layout error until one has been seen |
| `cathay.credit_card` | National ID | Due date taken from the auto-debit notice, which already accounts for holidays. Payments are checked against 繳款小計; a mismatch is a warning, since payments are not part of the reconciled new charges |
| `hsbc.credit_card` | National ID | Labels and descriptions are images. Rows come from the text layer, descriptions from OCR of the description column, matched by baseline. The new-charges subtotal and total due are the 4th and 5th unlabelled amounts on the transaction pages. Rows without a country or currency are classified by words in their description (年費, 回饋/折抵, 國外交易手續費; interest and other charges count as purchases, as at the other banks); without one, a payment in the payments section and a foreign transaction fee among the new charges |

Adding a kind of statement: subclass `StatementParser` with its sender domains, subject terms, and password rule (quote where the bank's e-mail states it), register it in `parsers/__init__.py`, write a synthetic fixture test and a case in `test_claims.py`, run `task parser:generate` for the Gmail filter, and run `task parser:test-real` against real e-mails.

## Errors

| Exception | gRPC status | Backend retries? |
|---|---|---|
| `UnsupportedDocumentError` | `UNIMPLEMENTED` | No |
| `InvalidDocumentError` | `INVALID_ARGUMENT` | No |
| `PasswordRequiredError` | `FAILED_PRECONDITION` | No |
| `LayoutError` (bank changed its layout) | `INTERNAL` | No |

An unbalanced statement is not an error: it comes back `OK` with `reconciliation.balanced = false`.

## Configuration

Environment variables, prefix `PARSER_`, nested keys split by `__`. Local runs also read `../../.local/parser.env`.

| Variable | Default | |
|---|---|---|
| `PARSER_IDENTITY__NATIONAL_ID` | — | National ID; most banks' password rules use it |
| `PARSER_IDENTITY__BIRTH_DATE` | — | `YYYY-MM-DD`; Taishin's card bill uses its month and day |
| `PARSER_PDF_PASSWORDS__<INSTITUTION>` | — | Only for a bank whose password the owner changed, e.g. `PARSER_PDF_PASSWORDS__TAISHIN`; tried before that bank's rule, and only for that bank |
| `PARSER_HOST` | `[::]` | Address to listen on |
| `PARSER_PORT` | `50051` | |
| `PARSER_WORKERS` | `4` | Threads for every call: `ParseDocument`, `OpenDocument`, and health checks. Keep it above the backend's `BACKEND_PARSE_CONCURRENCY` (2), or parses that take every thread starve previews and the container healthcheck, which gives up after 3 seconds |
| `PARSER_MAX_MESSAGE_MB` | `33` | gRPC message limit. Keep it 1 above the backend's `BACKEND_MAX_UPLOAD_MB`: a request is the whole file plus its other fields |
| `PARSER_OCR` | `true` | Without OCR, HSBC descriptions are empty and a warning is returned, and CTBC statements cannot be read |
| `PARSER_LOG_LEVEL` | `INFO` | |

## Development

```sh
task parser:test        # synthetic fixtures
task parser:test-real   # every .eml in .local/statements must reconcile
task parser:lint
task parser:run         # gRPC on :50051, with health checking and reflection
task parser:generate    # the Gmail filter, from the parsers' claims
```

Code under `src/finchie/` is generated from `proto/` (`task proto`); don't edit it. `task parser:generate` writes the Gmail filter to [`../backend/internal/mailfilter/gmail-filters.xml`](../backend/internal/mailfilter/gmail-filters.xml), which the backend serves (ADR 0014); commit it with the parser change.

## Image

`Dockerfile`: Python 3.13 slim, runs as uid 1654, OCR models baked in at build time. The container healthcheck runs `python -m finchie_parser.healthcheck`. Expose the port only on the internal network.
