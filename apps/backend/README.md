# backend

The only public service (ADR 0002). It stores uploaded documents, parses them in background jobs through the parser service, keeps the results in Postgres, and serves them over HTTP together with the web app (ADR 0007).

## Code layout

Dependencies point inward, toward `domain`; nothing in it knows the database, HTTP, the parser, or jobs.

| Package | |
|---|---|
| `internal/domain` | The model: kinds of accounts, units, lines, documents, and statements, and the rules that read them, such as an account's continuity and worth (`Assess`), positions' cost (`Positions`), how each kind of unit is spelled (`Symbol`), and what makes two statements one (`AccountNumbers`). Pure, unit-tested |
| `internal/ledger` | Writes that keep the domain's invariants, in the caller's transaction, whatever the source: units, whole-account snapshots, prices, links (`Relink`), and the owner's accounts, details, and holdings. Every source and the API write through it, so each rule has one implementation |
| `internal/ingest` | The parse pipeline: the `parse_document` job, converting the parser's response (contract v2: a statement is a report of accounts, ADR 0021), storing a statement, and `retry_documents` |
| `internal/exchanges`, `internal/rates`, `internal/quotes` | Sources that keep arriving by themselves: each reads its API and hands the ledger what it read |
| `internal/api` | HTTP: reads requests, calls the ledger or reads the database, and shows what the domain says. No rules of its own |
| `internal/jobs` | Queues and the schedule sources are read on |
| `internal/telemetry` | Telemetry, and gauges that report what the domain says (documents by status, gapped accounts) |
| `internal/parsertest` | Builders of parser responses for tests in any package |
| `internal/numeric`, `internal/taipei` | Exact decimals to and from NUMERIC (ADR 0004); Taipei's dates |
| `internal/db` | Migrations, and sqlc's queries in `dbq`, whose kind columns carry the domain's types |

## API

Defined spec-first in [`api/openapi.yaml`](api/openapi.yaml); `oapi-codegen` generates the Echo v5 handlers and types into `internal/api/openapi.gen.go`, and `internal/api/server.go` implements them.

| Method | Path | |
|---|---|---|
| `POST` | `/api/documents` | Multipart field `file`: a statement e-mail (`.eml`), as the bank sent it or forwarded. `201` when new, `200` when the same bytes were uploaded before, `415` for anything that is not an e-mail, a bare PDF included ([ADR 0014](../../docs/adr/0014-recognizing-statements.md)) |
| `GET` | `/api/documents` | Newest first, a page at a time: `items`, up to `?limit=` (500), after `?before=`, the last document of the page before; `?state=` (repeatable, a status), `?q=`, text in the filename, and `?id=` (repeatable: documents already shown, fetched again wherever they fall) filter. `byState` counts every match of `q` by state |
| `GET` | `/api/documents/{id}` | Status, parser, warnings; a failure's `failure` (what has to change) and `error` (its details); a duplicate's `duplicateOf` |
| `GET` | `/api/documents/{id}/content` | The original file. Only PDFs open in the browser (documents stored as bare PDFs before ADR 0014); anything else downloads as `application/octet-stream`, with `nosniff`. The stored content type comes from the bytes, never from what the browser declared, because the file is served from the web app's origin |
| `GET` | `/api/documents/{id}/preview` | The document opened for reading by the parser: the bank's sender, recipient, subject, and date (unwrapped from a forward), and its files. Nothing decrypted is stored ([ADR 0012](../../docs/adr/0012-reading-documents.md)) |
| `GET` | `/api/documents/{id}/preview/pdf` | The statement PDF, decrypted, in the browser: the file the parser marks primary (the PDF whose name a bank parser knows, else the first PDF). `404` without a PDF. `Cache-Control: no-store`, so the decrypted copy stays out of the browser's cache too |
| `GET` | `/api/documents/{id}/preview/body` | The e-mail's body as a page for an iframe, sandboxed by its own Content-Security-Policy: no scripts, only `data:` and https images, links to a new tab. `Cache-Control: no-store` |
| `GET` | `/api/documents/{id}/preview/files/{index}` | One file, a PDF decrypted. Only PDFs open in the browser; anything else downloads as `application/octet-stream` with `nosniff`. `Cache-Control: no-store` |
| `POST` | `/api/documents/{id}/reparse` | Parse the stored original again. The document's statement is replaced and keeps its id; its lines get new ones |
| `DELETE` | `/api/documents/{id}` | Delete a document and its original, only one that holds nothing of the ledger: it failed, was skipped, or is a duplicate (`deletable`; else 409). Documents that are duplicates of it are parsed again, so one of them holds the statement |
| `GET` | `/api/accounts` | Every account the owner has, assets and liabilities (ADR 0013, ADR 0022; `side`: a bank's card account and a loan are liabilities, their balances what is owed, as printed, their worth negative), with its newest balance in each unit (`unitId`, and the `unit`'s symbol) and that balance's `worth` in NT$ at its unit's newest `price`, whatever the source: a rate, a close, an exchange's market, or the owner (ADR 0020; `asOf`, `source`, one unit in its `quote` unit and in NT$, `twd`, through the quote unit's own newest price; none for NT$). The account's `worth` adds them up, none if a unit has no worth. A securities account's units are its securities, by name: each holding's `worth` is its shares at the security's newest close, with its `code`, and one it no longer holds is not listed (ADR 0017). An exchange account's units are its coins, and one it no longer holds is not listed either (ADR 0018); a points account's are its programs, by the name its bills print, and its `expiries` what its newest report says will lapse by when, none of 0 or gone by (ADR 0019). An account whose sources say it holds nothing is worth 0. Where a unit has no worth, the account's `worth` is its `valuation` of the day of its newest balance, what the exchange says the whole is worth, at the newest price of its unit; `worthFrom` says which (`holdings`, `valuation`). `continuity` is `checked` for deposit, securities, and points accounts whose balances statements report (a points gap is points that lapsed, ADR 0019; snapshots alone cannot be checked), `unknown` for a source that reports balances only; and its `gaps`: two consecutive balances in one unit whose difference the lines dated between them do not account for, with the `unseen` amount (none for a deposit account's unit no statement has listed a line of: when such a unit's balance moved, the account's `continuity` is `unknown`). Worked out on every read, never stored |
| `GET` | `/api/accounts/{id}` | One account, plus `history`, every balance its sources reported (a snapshot's has no `statementId`, ADR 0018); a securities account also its `positions` (ADR 0017): every security it holds or has traded, with its `cost` by average cost and its `gain` and `realized` gains, known only from a day the account held none of it with no gap since; `fills`, the owner's fills of its gaps that count |
| `PATCH` | `/api/accounts/{id}` | The owner's details (ADR 0016): `name`, which no other account goes by (`409`; cleared, the account goes by its own `label` again), and `fullNumber`, which must fit the masked number digit for digit where the source prints one, else is anything. A field left out stays, an empty one clears it; links are worked out again after. `checkAgainOn` (YYYY-MM-DD, after today in Taipei; empty clears it) is the day to look again whether the account is current, shown on it as `checkAgainOn`: an account with no lines gets no statement, so its newest balance can be old though nothing is wrong. A statement that brings a newer balance than the account had clears it, however far off; parsing one again does not. `204` |
| `POST` | `/api/accounts` | An account the owner keeps by hand (ADR 0019), of any `kind` but `credit_card`, with its `name`, which becomes its own `label`, as a bank's account has the bank's (`409` when another account goes by it). Institution `owner`; it holds nothing until its holdings are set. `201` with its `id` |
| `POST` | `/api/accounts/{id}/fills` | Fill one of the account's `gaps` (its `unit`, `from`, `to`) with what it lacks, once its reports are lost: a line of the account with no statement (kind `fill`), on the last day no report covers. It joins up, and net worth counts it from that day; search, totals, and transfers never see it. It counts while those balances are consecutive and lack exactly that (`counted_lines`, migration 00021); `409` when there is no such gap |
| `PATCH` | `/api/accounts/{id}/fills/{fillId}` | `memo`, what the owner writes about a fill, such as why its reports are lost; empty clears it. A fill made again for the same gap keeps it |
| `DELETE` | `/api/accounts/{id}/fills/{fillId}` | Take a fill back; the gap shows again |
| `PUT` | `/api/accounts/{id}/holdings` | What an account the owner opened holds today, the whole of it: `holdings`, each a `unit` of the kind the account holds (money upper-cased, securities by name, points; found or created, ADR 0020), its `amount`, and its `expiries`, what lapses by when. A snapshot (Taipei's date): a later one the same day replaces it, a unit no longer listed is at 0 (as is one listed at 0, except a deposit account's, which still has its currency), and the expiries replace all the account had. `400` for an account a source reports, a unit listed twice however spelled, or an amount below 0 (what a loan owes is its principal, positive; ADR 0022) or what lapses not above 0. `204` |
| `GET` | `/api/units` | The units amounts are counted in (ADR 0020), `?kind=` `money`, `security`, or `points` |
| `PUT` | `/api/units/{id}/price` | The owner's price of a unit as of now: one is worth `value` (0 or more) of `quote`, a money unit some account holds, NT$ when left out. It counts while it is the unit's newest; a later one the same day replaces it. NT$ is worth itself and takes none (`400`). `204` |
| `DELETE` | `/api/units/{id}/price` | Withdraws every price the owner gave the unit: kept on record, but no longer its newest. `204` |
| `GET` | `/api/accounts/{id}/lines` | Its lines, newest first, `?limit=` up to 500 and `offset`, each linked line with its `counterpart`: the other account, or the card bill it paid (ADR 0016). Amounts as the source reports them: money in is positive on a deposit account, a purchase on a card bill, shares bought on a securities account, whose lines carry their `trade` (price, amount, fee, tax, net paid or received, prepaid). The owner's fills of its gaps that count are lines too (`fill`, no `statementId`): one for the whole period no statement covers, on its last day, with the fill's memo and the balance it leaves. An exchange's lines are its deposits and withdrawals (ADR 0018), with no `statementId` either, nor has a `counterpart` that is one of them |
| `GET` | `/api/net-worth` | On a day (`?on=`, today in Taipei by default; ADR 0023): every account's worth then, `assets`, `liabilities` (negative), `net`, and `byKind`, with how many accounts have none (`unvalued`). An account's worth on a day is its newest balance on or before it, carried forward by the lines dated since for the kinds whose statements list every move, at each unit's newest price at the end of that day. `completeAsOf` is the newest day every account had reported by (accounts the owner keeps, and ones holding nothing, left out); `behind` names the accounts that have not reported since the day asked for. Worked out on every read (`domain.Net`) |
| `GET` | `/api/net-worth/history` | The same for each month end (`step=month`, the default) or day (`step=day`) from `from` to `to`, at most 1000 points, each `complete` when exact |
| `GET` | `/api/settings` | What the deployment decides that the web app shows or obeys: `maxUploadBytes`, `sourceIntervalHours` (how often rates, closes, and exchanges are read), `baseCurrency` |
| `GET` | `/api/mail-filters.xml` | The Gmail filter that forwards statement e-mails to `BACKEND_MAILBOX_ADDRESS`, generated from the parsers' claims (ADR 0014). `404` while that is not set |
| `GET` | `/api/statements` | Card bills, newest closing date first, with each statement's `checks` (ADR 0021) and whether all of them balance, its transaction count, and `paidOn`, once a deposit account or a later bill shows it paid (ADR 0016). Deposit statements are stored but get views of their own later (ADR 0013); this and the two endpoints after it show card bills and their lines only |
| `GET` | `/api/statements/{id}` | One statement |
| `GET` | `/api/statements/{id}/transactions` | In statement order |
| `GET` | `/api/transactions` | Every account's lines, card bills' and deposit accounts' alike. Filters: `from`, `to` (transaction date), `closingFrom`, `closingTo` (the statement's closing date), `kind` (repeatable), `accountKind`, `institution`, `card`, `account` (a deposit account), `overseas`, `hideTransfers`, `q` (description or memo); `sort` (`newest`, `oldest`, `largest`, `smallest`); `limit` 0–500 and `offset`. Each line has its amount as printed and its `flow`, seen from the owner (money out negative; a card line's sign turned, ADR 0013), and its `counterpart` when linked (ADR 0016). A link is one row: the line its money went to is folded under its outflow whenever both match (ADR 0016). Returns the page plus, for the whole match, `total`, `sum` (as printed, which adds up within one `accountKind` only), `flows` (per unit: spent and received, and apart from them what moved between the owner's own lines), and `byKind` / `byCard` / `byAccount` facets: the kind facet ignores the kind filter, the card and account facets the bank, card, and account filters. While purchases are included, fees and refunds linked to a purchase fold into it as its `children` and amounts are net of them ([ADR 0009](../../docs/adr/0009-transaction-parents.md)) |
| `GET` | `/api/transactions/{id}` | One transaction with its parent and children, whichever statements they are on |
| `GET` | `/healthz` | `200` when the database answers |
| `GET` | anything else | The web app: a file from the embedded build, or its `200.html` shell for client-side routes. Unknown `/api/` paths stay `404` |

Amounts are decimal strings; statement dates are `YYYY-MM-DD` (ADR 0004).

## Ingestion

1. Upload: the document is stored with its SHA-256 and a `parse_document` River job is inserted **in the same transaction**, so every stored document has a job.
2. The job calls the parser over gRPC (`BACKEND_PARSE_TIMEOUT`) and, in one transaction, replaces whatever the document produced before. Each account the statement reports is found or created (ADR 0013): a deposit statement covers an account per number; a bank's card lines go into its `credit_card` account, a liability whose balance on each closing date is what that bill says is owed (ADR 0022). A deposit account's closing balance per currency goes into `balances`, and its opening balance, where printed, on the day before the period; when deposit statements of an account share days (on-demand exports such as the e-passbook), only the one closing later keeps that account's lines and balances on them (of two closing the same day, the later document), whatever order they are stored in. Every check the statement carries (ADR 0021: a bill's new charges, each deposit account's balance per currency, each securities total, each points program) goes into `statement_checks`; one that does not balance makes the statement `needs_review`. A securities statement (ADR 0017) closes on its holdings date: its trades become lines in shares, one unit per security, with their money in `trades`; its holdings become balances, and since they are the whole account, every security the account has held or traded that a statement does not list is worked out again as 0 on that statement's day. A card bill's points programs (ADR 0019) become balances of the bank's `points` account on the closing date, one unit per program, its period's movements lines on that date (`earn`, `adjust`, `redeem`), and what will lapse `expiries`. Accounts are never deleted with a document. Then every link is worked out again from all the lines: parents (ADR 0009), transfers between the owner's lines and card bills' payments (ADR 0016).
3. The document ends up as:

| Status | Meaning |
|---|---|
| `pending` | Queued, being parsed, or waiting to retry |
| `parsed` | Stored; transactions add up to the statement |
| `needs_review` | Stored, but the transactions do not add up |
| `duplicate` | Another document, `duplicate_of`, already holds this statement: the same institution, kind, accounts, period start, and closing date |
| `failed` | It could not be parsed; `failure` says what has to change (`locked`, `unreadable`, `layout`, `unavailable`, `invalid_response`, `other`) and `error` the details. A failed document holds no statement |
| `skipped` | No parser handles it (`failure` `unsupported`): nothing to fix or retry |

Prices go into `prices`, one per unit, source, and day in Taipei, a later one the same day replacing it; the newest counts, whatever its source (ADR 0020). `fetch_exchange_rates` runs every 6 hours and at start (ADR 0015): it reads the day's rates to NT$ from Fawaz Ahmed's currency API on jsDelivr, else its Cloudflare Pages mirror, and keeps those of the money units some account holds or held, as of the start of the feed's day, so the backend needs outbound access to `cdn.jsdelivr.net` and `currency-api.pages.dev`; without it, only NT$ balances and what the exchanges and the owner price have a worth. `fetch_quotes` runs on the same schedule (ADR 0017): it reads the day's closes from TWSE's and TPEx's open APIs (`openapi.twse.com.tw`, `www.tpex.org.tw`) and keeps those of the securities some account holds or held, by name, as of 13:30, and their codes and markets on their units.

`fetch_exchange` runs on the same schedule, one job per exchange whose keys are set (ADR 0018; `internal/exchanges`): it reads what the account holds at MAX, Binance, or Bitget (`max-api.maicoin.com`, `api.binance.com`, `api.bitget.com`) and the exchange's valuation of it, and stores them in one transaction as that day's snapshot (Taipei date): balances without a statement, every coin it held before and no longer lists at 0, the valuation in `valuations`, and the price of each coin it holds at the exchange's own markets, in NT$ (MAX) or USDT, as of the fetch; and the deposits and withdrawals of money a bank holds (MAX's NT$, Binance's fiat orders) as the account's lines, without a statement, found again by the exchange's id for each (`external_id`), linking again when one is new or changed (ADR 0016). A later fetch the same day replaces the day's. A wallet whose coins, at the exchange's own prices, do not come to the value the exchange gives it goes in by that value in USDT. One call failing stores nothing for that exchange, and River retries it.

Only `UNAVAILABLE`, `DEADLINE_EXCEEDED`, and `RESOURCE_EXHAUSTED` from the parser are retried (6 attempts with River's back-off); everything else it returns fails at once (ADR 0005). Database errors are retried too. A document has one parse job at a time. `retry_documents` runs every hour and at start: it queues again every document that failed only because the parser stayed out of reach (`unavailable`), every `pending` one whose job was lost, every one a parser of another version than today's read, and every one that failed under an earlier parser instance (`DescribeParsers`; passwords, details, and OCR are read when the parser starts), so none waits for anyone to press re-parse.

Parse jobs run on River's default queue, `BACKEND_PARSE_CONCURRENCY` at a time; the sources' jobs and `retry_documents` on a `sources` queue of their own, so neither holds the other up.

On `SIGTERM` the backend stops taking jobs and gives a parse in progress 30 seconds to finish; one still running then is cancelled and retried after the restart. Give the container longer than that to stop (Compose `stop_grace_period`; Docker's default is 10 seconds).

## Database

- Schema: goose migrations in `internal/db/migrations/`, embedded and applied at startup together with River's own migrations. Add a new file for every change; never edit an applied one.
- Queries: plain SQL in `internal/db/queries/`, compiled by sqlc into `internal/db/dbq/`.

## Configuration

| Variable | Default | |
|---|---|---|
| `BACKEND_DATABASE_URL` | — | Required. Postgres connection string |
| `BACKEND_HTTP_ADDR` | `:8080` | |
| `BACKEND_PARSER_ADDR` | `localhost:50051` | Parser gRPC address (plaintext, internal network only) |
| `BACKEND_PARSER_TOKEN` | — | Sent with every call to the parser; set it to the parser's `PARSER_TOKEN`, and the parser serves no one else. Set once, at deployment |
| `BACKEND_PARSE_TIMEOUT` | `120s` | Deadline for one parse |
| `BACKEND_PARSE_CONCURRENCY` | `2` | Documents parsed at once (other jobs have a queue of their own); keep it below the parser's workers, so previews and health checks always get a thread |
| `BACKEND_MAX_UPLOAD_MB` | `32` | |
| `BACKEND_LOG_FORMAT` | `json` | `json` or `text` |
| `BACKEND_ACCESS_TEAM_DOMAIN` | — | Cloudflare Access team domain, `https://<team>.cloudflareaccess.com`. With `BACKEND_ACCESS_AUD`, every request but `/healthz` needs a valid Access token (ADR 0008); a service token's (no e-mail address) may only `POST /api/documents` (ADR 0010). Leave both empty locally |
| `BACKEND_ACCESS_AUD` | — | The Finchie Access application's AUD tag (not a secret) |
| `BACKEND_MAILBOX_ADDRESS` | — | The address the mailbox Worker receives statements at; the Gmail filter download forwards to it. The deployment's, never in the repository (ADR 0010) |
| `BACKEND_MAX_ACCESS_KEY`, `BACKEND_MAX_SECRET_KEY` | — | MAX's read-only API key (ADR 0018). Each exchange is fetched once all of its keys are set; some but not all stops the backend at start |
| `BACKEND_BINANCE_API_KEY`, `BACKEND_BINANCE_API_SECRET` | — | Binance's read-only API key, without IP restriction |
| `BACKEND_BITGET_API_KEY`, `BACKEND_BITGET_API_SECRET`, `BACKEND_BITGET_PASSPHRASE` | — | Bitget's read-only API key |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | Where traces, metrics, and logs go over OTLP gRPC, e.g. `http://collector:4317`. Unset: nothing leaves the process, logs go to stdout only (ADR 0011) |
| `OTEL_SERVICE_NAME` | `finchie-backend` | |
| `OTEL_RESOURCE_ATTRIBUTES` | — | E.g. `service.namespace=finchie,deployment.environment.name=production`; the other standard `OTEL_*` variables apply too |

`task backend:run` reads `../../.local/backend.env`.

## Development

```sh
task backend:generate   # after editing api/openapi.yaml or SQL; commit the output
task backend:lint
task backend:test       # integration tests start Postgres 18 with testcontainers
task backend:run
```

Integration tests need a Docker daemon and are skipped without one. Testcontainers uses `DOCKER_HOST` or the current Docker context; with Colima, `docker context use colima` (or `DOCKER_HOST=unix://$HOME/.colima/default/docker.sock`).

Generated code (`internal/gen/`, `internal/db/dbq/`, `internal/api/openapi.gen.go`) is committed; don't edit it. After changing `api/openapi.yaml`, also run `task web:generate` for the web app's types.

The web app is embedded from `internal/web/dist/` (gitignored apart from `.gitkeep`). `task backend:web` builds `apps/web/` and copies it there; without it, the binary still serves the API and answers app routes with a hint to build the UI.

## Image

`Dockerfile`: builds `apps/web/` in a Node stage, embeds it, then a static binary on distroless, uid 1654. The build context is the repository root (`docker build -f apps/backend/Dockerfile .`, or `task backend:image`); `Dockerfile.dockerignore` limits it to `apps/web/` and `apps/backend/`. The container healthcheck runs `/backend healthcheck`.
