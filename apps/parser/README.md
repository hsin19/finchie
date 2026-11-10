# parser

Stateless gRPC service that turns a statement e-mail into structured data: a card bill (statement fields, transactions, and a reconciliation of the transactions against the printed total), or a deposit statement (each account's lines and balances by currency, reconciled through its running balances with `reconcile_account`, ADR 0013). It takes e-mails only, never a bare PDF (ADR 0014). It also opens a document for reading (`OpenDocument`, `reading.py`): the bank's headers, the body, and the attachments with the PDFs decrypted, so the web app can show them without the password (ADR 0012). And it says what it can parse (`DescribeParsers`): each parser's version, and an instance that changes at every start, so the backend parses again what an older version read or what failed before passwords, details, or OCR were last set. The file it marks primary, the one the app opens, is the PDF parsing would take as the statement (step 3 below), or the first PDF when none is. It keeps no state and talks to nothing but its caller, the backend (ADR 0002). The contract is [`proto/finchie/parser/v1/parser.proto`](../../proto/finchie/parser/v1/parser.proto).

## How a document is parsed

1. **Read the bank's e-mail** (`documents.py`). One the owner forwarded by hand is unwrapped first: the message attached to it, or the header block of Gmail's inline forward (`---------- Forwarded message ---------`, labels in English or Chinese), innermost first. The parsers then see the bank's sender, subject, and date.
2. **Claim it** (`claimants` in `selection.py`). Each parser declares the sender domains (a subdomain counts) and the terms its e-mail's subject carries, all of them, case aside; the subject also tells a bank's card bill from its deposit statement. Terms, not a pattern: the Gmail filter searches for the same claims. An e-mail nobody claims is unsupported and never opened.
3. **Pick the statement** (`select_statement` in `selection.py`), among the parts whose bytes are a PDF, whatever type or name they carry (`Mail.pdfs`; previews list the same PDFs). The claiming parser opens the PDFs with its own passwords only (`pdf.py`, `passwords.py`): none, then one the owner chose (`PARSER_PDF_PASSWORDS__<INSTITUTION>`), then the bank's rule built from the owner's details. It takes the PDF whose name it knows (`TSB_Creditcard_Estatement_*.pdf`) and whose first page names its bank, so payment slips are ignored; a known name that none of its passwords opens fails the document for its password; otherwise any other PDF whose first page names the bank. A rule whose detail is not set fails naming the variable to set.
4. **Parse** (`parsers/<bank>.py`). Read the statement fields and every line, classify each line (purchase, foreign transaction fee, annual fee, refund, rebate, payment), and add up the new charges to compare with the amount the statement prints. A parser that `needs_ocr` fails with `ConfigurationError` while OCR is off. Once the backend stops waiting (its deadline passed or it cancelled), the parse stops before its next step or OCR call.

Lines are classified by one set of rules for every bank (`parsers/kinds.py`), by the bank's words where its layout says no more: 國外交易服務費/國外交易手續費/國外退貨手續費 a foreign transaction fee, 年費 an annual fee (its waiver negative), a negative line worded 扣繳/繳款/自動扣款/付款已收到 a payment, 回饋 (cash back) and 折抵 (points redeemed, 點數折抵…) a rebate, as `parser.proto` defines it; otherwise a refund or a purchase by sign. A deposit line is interest when its whole description is 利息, 利息存入, 存款息, or INTEREST. A parser classifies by layout first where its layout tells more (a payments section, a row with no card).

### Code

| Module | |
|---|---|
| `documents.py` | The upload as the bank's e-mail; which parts are PDFs (`pdf_data`); limits on untrusted MIME |
| `selection.py` | The e-mail's PDFs decrypted per parser (`PdfSet`), `claimants`, and `select_statement` (`Found`, `Locked`, `NotFound`) |
| `pipeline.py`, `reading.py` | `ParseDocument` and `OpenDocument`, both choosing with `select_statement` |
| `pdf.py`, `ocr.py` | What parsers read (`Document`, `Ocr`), and their implementations; know nothing of parsers |
| `text.py` | Tokens (`AMOUNT`, `ROC_DATE`, …) for parsers' patterns, each beside its reader (`amount`, `roc_date`, `month_day`, `day_in_period`, …), and `find` |
| `parsers/base.py` | `StatementParser` and `registry`, which checks every parser's metadata at import |
| `parsers/kinds.py`, `reconcile.py`, `points.py`, `layout.py` | Line classification, reconciliation, rewards points, and reading by position |
| `parsers/deposit.py` | What deposit parsers share: the book of `Accounts` (listed once each, reconciled into the statement) and reading a row's amounts by column |
| `errors.py` | Every failure and its gRPC status code |
| `settings.py` | Settings, and the only place that knows their environment variables' names |
| `convert.py`, `server.py` | Model to proto, and the gRPC service |


| Parser | Password, as the bank's e-mail states it | Notes |
|---|---|---|
| `esun.credit_card` | None stated (can be turned off at the bank) | Several cards per bill; rebates listed separately. e point (ADR 0019): the 點數明細 row, its headings an image, and each month's expiring points. From the October 2026 bill: Gregorian instead of Minguo dates, 本期回饋／費用明細, no 本期調整點數; both layouts are read |
| `esun.deposit` | National ID | Combined statement (綜合對帳單): every deposit account's balance, and the lines of those that had any. Withdrawal and deposit are told apart by column position (`Document.page_words`), since only one holds an amount; reconciled through every running balance. No opening balance is printed |
| `taishin.credit_card` | Last 2 characters of the national ID + birth month and day; up to the 113/11 bill, the national ID | Several Richart cards per bill; long descriptions wrap onto the lines around the row, never across a page break. Points (ADR 0019): a row per figure, a column per program (台新Point(信用卡), 大集大利點數 until 2022-06; 亞洲萬里通里數 where a figure is printed); used points print negative |
| `taishin.deposit` | National ID | Combined statement (綜合對帳單): NT$ and foreign currency summaries (a foreign account's currency sits half a row above it; its balance is the original currency's, not its NT$ worth), then tables of lines that carry the account number on every row and run onto the next page. An amount can print in both columns, as on a foreign currency interest line: the line is their difference. It words interest 存款息 and INTEREST |
| `fubon.deposit` | National ID (or one agreed with the bank) | Bank statement (銀行對帳單): 資產持有明細 lists each account's balance; 當月交易明細 opens each account's lines with the balance brought forward (承轉結餘), its number on that row only, which serves as the opening balance. Headings are centred over their columns, so words go to the nearest heading's centre. An account number past the amounts is the counterparty |
| `nextbank.deposit` | National ID | Combined statement (綜合對帳單): a section per account and currency, its balance in the heading; one with lines prints the balance brought forward (上期餘額) as the opening balance. Lines are dated by month and day only, placed within the statement's period, 收入 before 支出, which is printed negative. A long memo wraps onto rows above and below its line's. A transfer's memo names the other side's bank and account (玉山銀行-…), which becomes the counterparty with the bank's code where known. Pockets (活存口袋) are accounts of their own, by serial number; a pocket's lines raise a layout error until one has been seen. It words interest 利息存入 |
| `skis.securities` | National ID | Shin Kong Securities' monthly statement (證券月對帳單, ADR 0017); its daily trade reports (帳務回報通知) are not claimed. Trades are read by column position, and checked against the 合計 row column by column (價金, 手續費, 交易稅, 應收付金額). A buy paid in advance prints 0 to pay and its 預繳金 on the row below. Holdings, two to a row, are as of the day the statement prints, a few days into the next month, counting that day's trades. Names are Unicode-normalized (the text layer writes 一 as ⼀). Margin, short selling, and pledged shares raise a layout error until one has been seen |
| `ctbc.deposit` | National ID (can be changed or turned off at the bank) | Bank statement (對帳單), mid-month to mid-month. Its Chinese text has no Unicode mapping: digits, dates, and account numbers come from the text layer, everything in Chinese (section titles, account types, currencies by name, descriptions, the 上期結餘 and 本期結餘 rows) from OCR of the whole page, matched to text-layer rows by height. Each NT$ account's lines run from the balance brought forward to 本期結餘, which must equal its holding. Needs OCR; foreign currency lines raise a layout error until one has been seen |
| `cathay.credit_card` | National ID | Due date taken from the auto-debit notice, which already accounts for holidays. Payments are checked against 繳款小計; a mismatch is a warning, since payments are not part of the reconciled new charges. Points (ADR 0019): the 信用卡回饋總覽 row, its headings unmapped glyphs; this month's expiring points and the next twelve months' together |
| `cathay.deposit` | National ID | E-passbook (電子存摺) exported from the CUBE app (Cathay's monthly 綜合對帳單 e-mail has only a login link and no PDF). Page 1 is the cover with the account number, type, and currency; each inner page prints up to 22 passbook lines with remarks (備註) half a row above and the description (說明) and counterparty (`(812)0012345***678901…`) half a row below. The period runs from the first line's date to the last's, and the opening balance is the balance before the first line. It words interest 存款息 |
| `hsbc.credit_card` | National ID | Labels and descriptions are images. Rows come from the text layer, descriptions from OCR of the description column, matched by baseline. The new-charges subtotal and total due are the 4th and 5th unlabelled amounts on the transaction pages. Rows without a country or currency are classified by the shared words (interest and other charges count as purchases, as at the other banks); without any of them (OCR off or noisy), a payment in the payments section and a foreign transaction fee among the new charges. 滙豐旅遊積分 (ADR 0019): the box right of page 1's summary, its labels images too, read by position |

Adding a kind of statement: subclass `StatementParser` with its sender domains, subject terms, attachment name, first-page marker, and password rule (quote where the bank's e-mail states it), and `needs_ocr` if it cannot read without OCR. Classify lines with `parsers/kinds.py`, extending its word lists rather than writing a bank's own; a deposit statement builds its `Accounts` (`parsers/deposit.py`). Register it in `parsers/__init__.py` (`registry` refuses a parser with missing metadata or a name taken), write a synthetic fixture test, add its claim and attachment name to `test_claims.py`, run `task parser:generate` for the Gmail filter, and run `task parser:test-real` against real e-mails, which needs one for every parser.

Bump a parser's `version` whenever its output for the same e-mail can change, including through the shared code under `parsers/` or `text.py`.

## Errors

Each error carries its status code (`errors.py`); `server.py` aborts with it. What a library raises on a malformed upload is turned into one of them where the library is called; anything else is a bug (`UNKNOWN`).

| Exception | gRPC status | Backend retries? |
|---|---|---|
| `UnsupportedDocumentError` | `UNIMPLEMENTED` | No |
| `InvalidDocumentError` (also past a limit below) | `INVALID_ARGUMENT` | No |
| `PasswordRequiredError` | `FAILED_PRECONDITION` | No |
| `ConfigurationError` (the deployment lacks something, such as OCR) | `FAILED_PRECONDITION` | No |
| `LayoutError` (bank changed its layout) | `INTERNAL` | No |
| `AbandonedError` (the caller stopped waiting) | `CANCELLED` | Nobody reads it |

An unbalanced statement is not an error: it comes back `OK` with `reconciliation.balanced = false`.

Uploads are untrusted (ADR 0010), so the parser refuses, unread, an e-mail of more than 200 parts (`documents.MAX_PARTS`) or with forwards nested more than 5 deep (`MAX_NESTING`), and a PDF of more than 200 pages (`pdf.MAX_PAGES`). A preview inlines at most 8 MiB of the body's images (`reading.MAX_INLINED_BYTES`).

## Telemetry

With `OTEL_EXPORTER_OTLP_ENDPOINT` set (`telemetry.py`, ADR 0011):

- **Traces.** A span for every call but health checks, named after the method (`/finchie.parser.v1.ParserService/ParseDocument`), a child of the backend's call through the trace context it sends. It carries `finchie.document.id`, and for a parsed statement `finchie.parser.name`, `finchie.parser.version`, `finchie.statement.lines`, and `finchie.statement.balanced`. Under it, `select_statement` (decrypting and reading the PDFs), `parse`, and an `ocr` span per page strip, which includes waiting for the one OCR engine. A rejected document is an error with its status code; anything else that fails, a bug, records its exception.
- **Metrics.** The process's CPU time, memory, threads, and garbage collections.
- **Logs.** Every `logging` record, also on stdout, with the trace it was logged in.

## Configuration

Environment variables, prefix `PARSER_`, nested keys split by `__`; nothing else unless asked: `task parser:run` loads `../../.local/parser.env` into the environment, and `task parser:test-real` reads it too. Tests never see `PARSER_*` (`tests/conftest.py`).

| Variable | Default | |
|---|---|---|
| `PARSER_IDENTITY__NATIONAL_ID` | — | National ID; most banks' password rules use it |
| `PARSER_IDENTITY__BIRTH_DATE` | — | `YYYY-MM-DD`; Taishin's card bill uses its month and day |
| `PARSER_PDF_PASSWORDS__<INSTITUTION>` | — | Only for a bank whose password the owner changed, e.g. `PARSER_PDF_PASSWORDS__TAISHIN`; tried before that bank's rule, and only for that bank. One for an institution no parser has stops the server from starting |
| `PARSER_HOST` | `[::]` | Address to listen on |
| `PARSER_PORT` | `50051` | |
| `PARSER_WORKERS` | `4` | Threads for every call: `ParseDocument`, `OpenDocument`, and health checks. Keep it above the backend's `BACKEND_PARSE_CONCURRENCY` (2), or parses that take every thread starve previews and the container healthcheck, which gives up after 3 seconds |
| `PARSER_MAX_MESSAGE_MB` | `33` | gRPC message limit. Keep it 1 above the backend's `BACKEND_MAX_UPLOAD_MB`: a request is the whole file plus its other fields |
| `PARSER_TOKEN` | — | A secret every caller must send (`authorization: Bearer …`), the backend's `BACKEND_PARSER_TOKEN`; health checks and reflection need none. Unset, anyone who reaches the port is served (`auth.py`) |
| `PARSER_OCR` | `true` | Without OCR, HSBC descriptions are empty and a warning is returned, and CTBC statements fail with `FAILED_PRECONDITION` |
| `PARSER_LOG_LEVEL` | `INFO` | |
| `PARSER_VERSION` | `dev` | The release, reported as `service.version`. The image sets it; leave it alone |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | Where traces, metrics, and logs go over OTLP gRPC, e.g. `http://collector:4317`. Unset: nothing leaves the process, logs go to stdout only (ADR 0011) |
| `OTEL_SERVICE_NAME` | `finchie-parser` | |
| `OTEL_RESOURCE_ATTRIBUTES` | — | E.g. `service.namespace=finchie,deployment.environment.name=production`; the other standard `OTEL_*` variables apply too |

## Development

```sh
task parser:test        # synthetic fixtures
task parser:test-real   # every .eml in .local/statements must reconcile, with one for every parser
task parser:lint        # ruff, and mypy in strict mode
task parser:run         # gRPC on :50051, with health checking and reflection
task parser:generate    # the Gmail filter, from the parsers' claims
```

Code under `src/finchie/` is generated from `proto/` (`task proto`); don't edit it. `task parser:generate` writes the Gmail filter to [`../backend/internal/mailfilter/gmail-filters.xml`](../backend/internal/mailfilter/gmail-filters.xml), which the backend serves (ADR 0014); commit it with the parser change.

## Image

`Dockerfile`: Python 3.13 slim, runs as uid 1654, OCR models baked in at build time. The `VERSION` build argument (`release.yml` passes the tag's version) becomes `PARSER_VERSION`. The container healthcheck runs `python -m finchie_parser.healthcheck`. Expose the port only on the internal network.
