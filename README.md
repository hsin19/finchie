# Finchie

Finchie = Finance + Finch

A self-hosted personal finance platform. It turns bank and credit card statements into a ledger and, later, tracks investments, so you can see where your money goes and where you stand.

> **V2 is under construction on `main`.** V1 (a Python batch job plus a Go API on MongoDB) is frozen at tag [`v1.0.0`](https://github.com/hsin19/finchie/tree/v1.0.0) and no longer on `main`.

## Components

**Apps** (`apps/`, one folder each, with its own toolchain and README)

| Path | What it is | Status |
|---|---|---|
| [`apps/backend`](apps/backend/) | Go. The only public service: API, document ingestion, background jobs, and the web app (embedded). Owns the database. | ✅ Upload, parse, and read statements |
| [`apps/parser`](apps/parser/) | Python. Stateless parser behind gRPC: a raw statement goes in, structured data comes out. Internal only. | ✅ 4 banks |
| [`apps/mailbox`](apps/mailbox/) | Cloudflare Email Worker. Takes statements Gmail forwards to an address on Email Routing and uploads them to the backend. Deployed by hand | ✅ Automatic ingestion |
| [`apps/web`](apps/web/) | SvelteKit single-page app. Built into the backend binary today; an app of its own, so it could also be served on its own (say, by a proxy in front of the backend) | ✅ Overview, statements, transactions, accounts, upload |

**Shared**

| Path | What it is | Status |
|---|---|---|
| [`proto/`](proto/) | gRPC contracts, managed with buf. Generated code is committed inside each app. | ✅ |

Design decisions and their reasons are in [`docs/adr/`](docs/adr/): 0001 rewrite as V2, 0002 architecture, 0003 tech stack, 0004 money and dates, 0005 ingestion pipeline, 0006 test data, 0007 web app, 0008 authentication, 0009 transaction parents, 0010 mail ingestion, 0011 telemetry, 0012 reading documents, 0013 accounts, 0014 recognizing statements, 0015 exchange rates, 0016 transfer links, 0017 securities accounts.

## Why V2

V1 stalled on Gmail OAuth (7-day refresh tokens unless the app passes Google's verification), its parser returned wrong data without noticing, money was `float64`, and the deployment plan (Azure + GitHub Pages) no longer matched where things run. The scope also grew: Finchie now absorbs InTheGreenYet, an investment tracker on Notion that was too slow. Details in [ADR 0001](docs/adr/0001-rewrite-as-v2.md).

## Architecture

```
browser ── Cloudflare Access ── Tunnel ──▶ backend (Go) ── gRPC ──▶ parser (Python)
                                             │
                                             └── Postgres
```

- The backend serves the web app and the API from one origin; the app is a static build embedded in the binary.
- The backend stores every uploaded document as-is, then a background job asks the parser to read it. Parsed results can always be rebuilt from the stored originals.
- Every parsed statement is reconciled: the transactions must add up to the amount printed on the statement. Statements that don't are kept but flagged for review instead of being trusted.
- The parser never touches the database. Retries, back-off, and concurrency limits live in the backend's job queue.

## Supported statements

| Institution | Source | Notes |
|---|---|---|
| E.SUN Bank | PDF attached to the e-statement e-mail | |
| Taishin Bank | Password-protected PDF | One bill can list several Richart cards |
| Cathay United Bank | Password-protected PDF | |
| HSBC Taiwan | Password-protected PDF | Transaction descriptions are images, read with OCR |

Statements are taken as e-mails only ([ADR 0014](docs/adr/0014-recognizing-statements.md)). Let Gmail forward them to the mailbox, which uploads them as they arrive ([ADR 0010](docs/adr/0010-mail-ingestion.md)): the upload page offers the Gmail filter to import, generated from the parsers. Or upload the `.eml` by hand, as the bank sent it or forwarded. A statement opens in the browser as its PDF, decrypted, without the password ([ADR 0012](docs/adr/0012-reading-documents.md)).

## Getting started

Prerequisites: Go 1.27+, [uv](https://docs.astral.sh/uv/), Node 26 and pnpm, [Task](https://taskfile.dev/), Docker (for Postgres).

```sh
task --list              # everything you can run
task web:install         # web dependencies
task check               # what CI runs: proto checks, lint, tests, and the web build
docker compose up -d     # local Postgres
task parser:run          # gRPC on :50051
task backend:web         # build the web app into the backend (optional for API-only work)
task backend:run         # HTTP on :8080: the web app and /api
task web:dev             # or: Vite on :5173 with hot reload, /api proxied to :8080
curl -F file=@statement.eml localhost:8080/api/documents
```

Local configuration and secrets go in `.local/` (gitignored):

| File | Contents |
|---|---|
| `.local/backend.env` | `BACKEND_DATABASE_URL=postgres://finchie:finchie@localhost:5432/finchie?sslmode=disable` |
| `.local/parser.env` | `PARSER_IDENTITY__NATIONAL_ID=…` and `PARSER_IDENTITY__BIRTH_DATE=…`: the details the banks' PDF password rules use (ADR 0014) |
| `.local/statements/` | Real statement e-mails (`.eml`) for `task parser:test-real` |

`docker compose --profile app up --build` runs Postgres, the parser, and the backend (web app included) as containers (needs BuildKit, i.e. `docker buildx`). See each service's README for every variable it reads.

## CI and releases

GitHub Actions in [`.github/workflows/`](.github/workflows/README.md): `check.yml` runs on PRs, pushes to `main`, and releases. Pushing a `v*` tag (`git tag v2.0.0 && git push origin v2.0.0`) builds both images for `linux/amd64` and `linux/arm64`, smoke-checks each, and publishes `ghcr.io/hsin19/finchie-backend` and `ghcr.io/hsin19/finchie-parser` tagged `2.0.0`, `2.0` (moved only when this is the highest `2.0.x` release), and `sha-<short sha>`.

## Deployment

The homelab's Docker Compose runs the released images behind Cloudflare Tunnel and Access. Only the backend is reachable from outside; the parser stays on the internal network. Pin a version tag, never `latest`.

Sign-in is Cloudflare Access with Google, and the backend also verifies every request's Access token (ADR 0008): set `BACKEND_ACCESS_TEAM_DOMAIN` and `BACKEND_ACCESS_AUD` there. Set `BACKEND_MAILBOX_ADDRESS` for the Gmail filter download, and `PARSER_IDENTITY__*` for the parser's PDF passwords.
