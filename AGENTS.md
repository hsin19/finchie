# AGENTS.md

Rules and conventions for coding agents working in this repository. The project overview is in [`README.md`](README.md); design decisions are in [`docs/adr/`](docs/adr/).

## Project at a glance

Self-hosted personal finance platform. V2 is being built on `main` (ADR 0001): a Go backend that owns the database and the API, a stateless Python parser behind gRPC (ADR 0002), and a SvelteKit web app embedded in the backend (ADR 0007).

| Path | Contents |
|---|---|
| `apps/backend/` | Go. API, document ingestion, River jobs, Postgres schema |
| `apps/parser/` | Python. gRPC server that turns statements into structured data |
| `apps/web/` | SvelteKit single-page app; its build is embedded in the backend |
| `apps/mailbox/` | Cloudflare Email Worker: uploads statements Gmail forwards; deployed by hand (`task mailbox:deploy`) |
| `proto/` | gRPC contracts (buf). Edit here, then regenerate |
| `docs/adr/` | Design decisions |

V1 lives only at tag `v1.0.0`; look there for its code (for example the IMAP fetcher), not on `main`.

## Product principle: no work for the owner

Finchie exists to take work off its owner, never to hand them any. V1 died of a chore (re-authorizing Gmail every seven days, ADR 0001). Check every proposal against this first.

- Data comes only from sources that keep arriving by themselves: statement e-mails Gmail forwards, read-only API keys that do not expire. Anything that needs periodic re-authorization is out.
- Configuration happens at most once. Accounts, cards, and the like are created from what statements print (ADR 0013), not set up by hand.
- Quiet unless action is needed: an unbalanced statement, money that moved between two statements unseen, a card bill not paid automatically. Nothing else notifies.
- Manual input is an optional supplement, never what a feature depends on. A feature that needs someone to keep rules up to date, such as card promotions, stays out unless its data can come by itself.

## Build and test

Run from the repository root; `task --list` shows everything.

```sh
task proto              # regenerate Go and Python code from proto/ (commit the output)
task backend:generate   # regenerate sqlc and oapi-codegen output (commit it)
task parser:test
task parser:lint
task backend:test       # integration tests start Postgres with testcontainers; skipped without Docker
task backend:lint
task web:generate       # regenerate the web app's API types after editing openapi.yaml (commit them)
task web:check          # dprint, ESLint, svelte-check, Vitest, build
task web:dev            # Vite on :5173, /api proxied to the backend on :8080
task backend:web        # build apps/web/ into the backend, so `task backend:run` serves the UI
task check              # everything CI runs (except the generated-code check, which needs a clean tree)
docker compose up -d    # local Postgres for `task backend:run`
.github/scripts/smoke.sh backend <image>   # the release smoke check, against a local image
```

- CI lives in `.github/workflows/` (layout in its README). `check.yml` is the only definition of the checks and calls Task targets; a new check goes into a Taskfile and into `check.yml`, nowhere else. Releases are `v*` tags.

- Generated code is committed; never edit it by hand:

  | Generated | From | Command |
  |---|---|---|
  | `apps/backend/internal/gen/`, `apps/parser/src/finchie/` | `proto/` | `task proto` |
  | `apps/backend/internal/db/dbq/` | `internal/db/queries/*.sql`, migrations | `task backend:generate` |
  | `apps/backend/internal/api/openapi.gen.go` | `apps/backend/api/openapi.yaml` | `task backend:generate` |
  | `apps/web/src/lib/api/schema.d.ts` | `apps/backend/api/openapi.yaml` | `task web:generate` |

- `buf breaking` guards the proto contract against incompatible changes once it exists on `main`: CI compares a push to `main` with the commit before it and a PR with `main`; locally `task proto:breaking AGAINST=<ref>` (default `main`).
- Real-statement regression: put `.eml` / `.pdf` files in `.local/statements/`, PDF passwords in `.local/parser.env`, and run `task parser:test-real`. It asserts that every statement reconciles. These files never leave `.local/` (ADR 0006).
- Integration tests and local Compose need a Docker daemon. On Colima, testcontainers needs `DOCKER_HOST` or `docker context use colima`; the backend Taskfile sets the socket override it also needs.
- Changed behavior needs a test. When fixing a bug, first make sure the new test fails without the fix.

## Conventions

- **English everywhere**: code, comments, docs, commit messages, log and error strings.
- Comments only for what the code cannot say: why, constraints, pitfalls. Don't restate the code.
- Money and dates follow ADR 0004: no floats for amounts, every amount has a currency, statement dates are dates without time zones.
- The parser stays stateless: no database, no files written outside temp space, no network calls. Anything stateful belongs in the backend.
- gRPC status codes carry the retry decision (ADR 0005). A parser that cannot handle a document returns `UNIMPLEMENTED`; a wrong password is `FAILED_PRECONDITION`.
- Each bank parser reconciles its own statements. Never "fix" a mismatch by adjusting numbers; report it.
- Python: type hints everywhere, ruff for lint and format, pytest. Test names describe behavior (`test_taishin_splits_wrapped_merchant_lines`).
- Go: `gofmt`, golangci-lint, errors wrapped with `%w`, `context.Context` first. SQL lives in `.sql` files for sqlc, not in Go strings.
- Schema changes are new goose migrations; never edit an applied one.
- Web (SvelteKit 3, Svelte 5 runes): see [`apps/web/README.md`](apps/web/README.md) for its pitfalls. Format with dprint (`task web:fmt`), not Prettier. Amounts go through `#lib/money.js`, never `Number` arithmetic. Every filter belongs in the URL.

## Documentation rules

- **After changing a file, check whether the `README.md` in each parent directory, up to the repository root, needs updating.**
- Design decisions go into an ADR: `docs/adr/NNNN-<slug>.md`, same format as the existing ones. Don't rewrite an accepted ADR; add later corrections or exceptions in parentheses next to the sentence they affect. Superseding one entirely means a new ADR, a pointer to it from the old one, and an updated ADR list in the root `README.md`.

## Commits

- Agents do not commit or push unless asked; the owner reviews and commits.
- Conventional Commits in English: `type(scope): summary`. Scopes: `backend`, `parser`, `mailbox`, `proto`, `web`, `docs`; omit for changes across components.

## Secrets and local state

None of these are under version control. Don't add them, and don't print their contents.

| Path / variable | Contents |
|---|---|
| `.local/` | Real statements, local env files, anything personal |
| `PARSER_PDF_PASSWORDS__<INSTITUTION>` | PDF passwords for the parser. Check whether one is set by name only |
| `BACKEND_DATABASE_URL` | Postgres connection string |
