# backend

The only public service (ADR 0002). It stores uploaded documents, parses them in background jobs through the parser service, keeps the results in Postgres, and serves them over HTTP together with the web app (ADR 0007).

## API

Defined spec-first in [`api/openapi.yaml`](api/openapi.yaml); `oapi-codegen` generates the Echo v5 handlers and types into `internal/api/openapi.gen.go`, and `internal/api/server.go` implements them.

| Method | Path | |
|---|---|---|
| `POST` | `/api/documents` | Multipart field `file`: a statement e-mail (`.eml`), as the bank sent it or forwarded. `201` when new, `200` when the same bytes were uploaded before, `415` for anything that is not an e-mail, a bare PDF included ([ADR 0014](../../docs/adr/0014-recognizing-statements.md)) |
| `GET` | `/api/documents` | Newest first, `?limit=` up to 500 |
| `GET` | `/api/documents/{id}` | Status, parser, warnings, error |
| `GET` | `/api/documents/{id}/content` | The original file. Only PDFs open in the browser (documents stored as bare PDFs before ADR 0014); anything else downloads as `application/octet-stream`, with `nosniff`. The stored content type comes from the bytes, never from what the browser declared, because the file is served from the web app's origin |
| `GET` | `/api/documents/{id}/preview` | The document opened for reading by the parser: the bank's sender, recipient, subject, and date (unwrapped from a forward), and its files. Nothing decrypted is stored ([ADR 0012](../../docs/adr/0012-reading-documents.md)) |
| `GET` | `/api/documents/{id}/preview/pdf` | The statement PDF, decrypted, in the browser: the file the parser marks primary (the PDF whose name a bank parser knows, else the first PDF). `404` without a PDF. `Cache-Control: no-store`, so the decrypted copy stays out of the browser's cache too |
| `GET` | `/api/documents/{id}/preview/body` | The e-mail's body as a page for an iframe, sandboxed by its own Content-Security-Policy: no scripts, only `data:` and https images, links to a new tab. `Cache-Control: no-store` |
| `GET` | `/api/documents/{id}/preview/files/{index}` | One file, a PDF decrypted. Only PDFs open in the browser; anything else downloads as `application/octet-stream` with `nosniff`. `Cache-Control: no-store` |
| `POST` | `/api/documents/{id}/reparse` | Parse the stored original again. The document's statement is replaced, so it gets a new id |
| `GET` | `/api/accounts` | Every account the owner has (ADR 0013; a bank's card bill is none), with its newest balance in each unit and that balance's `worth` in NT$ at the newest rate of its unit (ADR 0015; the account's `worth` adds them up, none if a unit has no rate), `continuity` (`checked` for deposit accounts; `unknown` for a source that reports balances only), and its `gaps`: two consecutive balances in one unit whose difference the lines dated between them do not account for, with the `unseen` amount. Worked out on every read, never stored |
| `GET` | `/api/accounts/{id}` | One account, plus `history`, every balance its statements reported |
| `PATCH` | `/api/accounts/{id}` | The owner's details (ADR 0016): `name`, and `fullNumber`, which must fit the masked number digit for digit. A field left out stays, an empty one clears it; links are worked out again after. `204` |
| `GET` | `/api/accounts/{id}/lines` | Its lines, newest first, `?limit=` up to 500 and `offset`, each linked line with its `counterpart`: the other account, or the card bill it paid (ADR 0016). Amounts as the source reports them: money in is positive on a deposit account, a purchase on a card bill |
| `GET` | `/api/mail-filters.xml` | The Gmail filter that forwards statement e-mails to `BACKEND_MAILBOX_ADDRESS`, generated from the parsers' sender domains (ADR 0014). `404` while that is not set |
| `GET` | `/api/statements` | Card bills, newest closing date first, with each statement's transaction count and `paidOn`, once a deposit account or a later bill shows it paid (ADR 0016). Deposit statements are stored but get views of their own later (ADR 0013); this and the two endpoints after it show card bills and their lines only |
| `GET` | `/api/statements/{id}` | One statement |
| `GET` | `/api/statements/{id}/transactions` | In statement order |
| `GET` | `/api/transactions` | Every account's lines, card bills' and deposit accounts' alike. Filters: `from`, `to` (transaction date), `closingFrom`, `closingTo` (the statement's closing date), `kind` (repeatable), `accountKind`, `institution`, `card`, `account` (a deposit account), `overseas`, `hideTransfers`, `q` (description or memo); `sort` (`newest`, `oldest`, `largest`, `smallest`); `limit` 0–500 and `offset`. Each line has its amount as printed and its `flow`, seen from the owner (money out negative; a card line's sign turned, ADR 0013), and its `counterpart` when linked (ADR 0016). A link is one row: the line its money went to is folded under its outflow whenever both match (ADR 0016). Returns the page plus, for the whole match, `total`, `sum` (as printed, which adds up within one `accountKind` only), `flows` (per unit: spent and received, and apart from them what moved between the owner's own lines), and `byKind` / `byCard` / `byAccount` facets: the kind facet ignores the kind filter, the card and account facets the bank, card, and account filters. While purchases are included, fees and refunds linked to a purchase fold into it as its `children` and amounts are net of them ([ADR 0009](../../docs/adr/0009-transaction-parents.md)) |
| `GET` | `/api/transactions/{id}` | One transaction with its parent and children, whichever statements they are on |
| `GET` | `/healthz` | `200` when the database answers |
| `GET` | anything else | The web app: a file from the embedded build, or its `200.html` shell for client-side routes. Unknown `/api/` paths stay `404` |

Amounts are decimal strings; statement dates are `YYYY-MM-DD` (ADR 0004).

## Ingestion

1. Upload: the document is stored with its SHA-256 and a `parse_document` River job is inserted **in the same transaction**, so every stored document has a job.
2. The job calls the parser over gRPC (`BACKEND_PARSE_TIMEOUT`) and, in one transaction, replaces whatever the document produced before. Each account the statement reports is found or created (ADR 0013): a deposit statement covers an account per number; a bank's card lines go into its `credit_card` account, which holds no balance and is not listed, since what a bill says is owed stays on the bill. A deposit account's closing balance per currency goes into `balances`, and its opening balance, where printed, on the day before the period. A deposit account whose lines do not add up makes the statement `needs_review` and is named in the document's warnings. Accounts are never deleted with a document. Then every link is worked out again from all the lines: parents (ADR 0009), transfers between the owner's lines and card bills' payments (ADR 0016).
3. The document ends up as:

| Status | Meaning |
|---|---|
| `pending` | Queued, being parsed, or waiting to retry |
| `parsed` | Stored; transactions add up to the statement |
| `needs_review` | Stored, but the transactions do not add up |
| `duplicate` | Another document already holds this statement (same institution, kind, and closing date) |
| `failed` | The parser rejected it (`error` says why), or every attempt failed: the parser stayed unreachable, or the database refused the result |

`fetch_exchange_rates` runs every 6 hours and at start (ADR 0015): it reads the day's rates to NT$ from Fawaz Ahmed's currency API on jsDelivr, else its Cloudflare Pages mirror, into `exchange_rates`, so the backend needs outbound access to `cdn.jsdelivr.net` and `currency-api.pages.dev`; without it, only NT$ balances have a worth.

Only `UNAVAILABLE`, `DEADLINE_EXCEEDED`, and `RESOURCE_EXHAUSTED` from the parser are retried (6 attempts with River's back-off); everything else it returns fails at once (ADR 0005). Database errors are retried too.

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
| `BACKEND_PARSE_TIMEOUT` | `120s` | Deadline for one parse |
| `BACKEND_PARSE_CONCURRENCY` | `2` | Documents parsed at once; keep it below the parser's workers, so previews and health checks always get a thread |
| `BACKEND_MAX_UPLOAD_MB` | `32` | |
| `BACKEND_LOG_FORMAT` | `json` | `json` or `text` |
| `BACKEND_ACCESS_TEAM_DOMAIN` | — | Cloudflare Access team domain, `https://<team>.cloudflareaccess.com`. With `BACKEND_ACCESS_AUD`, every request but `/healthz` needs a valid Access token (ADR 0008); a service token's (no e-mail address) may only `POST /api/documents` (ADR 0010). Leave both empty locally |
| `BACKEND_ACCESS_AUD` | — | The Finchie Access application's AUD tag (not a secret) |
| `BACKEND_MAILBOX_ADDRESS` | — | The address the mailbox Worker receives statements at; the Gmail filter download forwards to it. The deployment's, never in the repository (ADR 0010) |
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
