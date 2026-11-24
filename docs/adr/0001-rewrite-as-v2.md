# ADR 0001: Rewrite as V2

## Decision

Rebuild Finchie as V2 on `main` in the same repository. V1 is frozen at tag `v1.0.0`, and its services (`services/ledger-svc`, `services/statement-fetcher`) are removed from `main`: nothing in them is worth carrying as code, and the tag keeps them available for reference. No V1 code is migrated.

## Background

V1 was a Python batch job (`statement-fetcher`) that fetched Gmail, parsed statements, and posted JSON to a Go REST API (`ledger-svc`) on MongoDB, with Azure Functions, Cosmos DB, GitHub Pages, and scheduled GitHub Actions as the deployment plan. It never ran unattended:

| Problem | Detail |
|---|---|
| Gmail OAuth | An OAuth app in *Testing* status gets refresh tokens that expire after 7 days. Publishing an app that requests the restricted `gmail.readonly` scope requires Google's verification (privacy policy, demo video, review). The IMAP fetcher avoids this but needs an App Password, which grants full mailbox access to whatever host runs it. |
| Silent parser failures | The Taishin parser was written against a 2025 statement. On 2026 statements, which list several Richart cards per bill, it found 1 card and 17 of 50 transactions (2 of 55 and 12 of 47 on the next two months) and still reported success. Nothing compared the result with the statement total. |
| Money as `float64` | Both services stored amounts as floating point. |
| Deployment target | Services now run on a self-hosted homelab (Postgres, Cloudflare Tunnel and Access) that already hosts other projects. The Azure plan was never built. |
| Scope | Finchie becomes a personal finance platform that also takes over InTheGreenYet, an investment tracker whose Notion backend was too slow to use. |
| Coupling | Fetching, parsing, and storing were one job. A parser bug meant re-fetching mail; there was no stored original to re-parse. |

## What carries over

- The name, the repository, and its history.
- The overall shape: Python for documents, Go for the API, TypeScript for the web app.
- The parser's registry of per-bank processors that each answer "can I handle this?" and "extract it".
- ROC (Minguo) year handling.
- The IMAP approach, for a future local tool that backfills old statements by uploading them to V2 (V1's fetcher is at the tag). (The tool was never built: a Takeout export of the Gmail filter's label backfills instead, ADR 0014.)
- Tool choices: uv and ruff for Python, golangci-lint for Go, Task as the command runner.

## Consequences

- V1's CI workflow (tests for `statement-fetcher`) goes with it; V2 gets its own.
- V2 releases are tagged `v2.x.y`.
