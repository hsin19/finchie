# cli

Finchie's API as commands, for agents and scripts. The web app is the owner's way into the ledger; this is an AI agent's, for questions about the real ledger, one-off analysis, and later work on it such as sorting spending into categories. It builds its commands at every run from the contract the backend serves (`GET /api/openapi.json`, from [`apps/backend/api/openapi.yaml`](../backend/api/openapi.yaml)), so every operation the deployment offers is a command, with no release of the CLI in between.

## Running it

Nothing to install: [uv](https://docs.astral.sh/uv/) runs it straight from the repository, keeps it in its own cache, and puts nothing on the `PATH`. It needs no Python of the machine's; uv fetches one.

```sh
uvx --from "git+https://github.com/hsin19/finchie#subdirectory=apps/cli" finchie --help
```

uv looks for a new commit on `main` at every run and rebuilds only when there is one. From a checkout, `task cli:run -- <command>` runs the code in it instead.

## Settings

From environment variables, or a TOML file at `~/.config/finchie/config.toml` (`$XDG_CONFIG_HOME/finchie/config.toml`); the variables take precedence. The CLI writes nothing.

| Key | Variable | |
|---|---|---|
| `url` | `FINCHIE_URL` | The backend: the deployment's address, or `http://localhost:8080` |
| `client_id` | `FINCHIE_CLIENT_ID` | The agents' Access service token (below). Leave both out for a backend without Access, such as one running locally |
| `client_secret` | `FINCHIE_CLIENT_SECRET` | |

```toml
url = "https://finchie.example.com"
client_id = "….access"
client_secret = "…"
```

The file holds a secret that reads the whole ledger: `chmod 600` it. The CLI warns while others can read it.

### The agents' service token

The deployment sits behind Cloudflare Access (ADR 0008). Agents get a service token of their own, not the mailbox Worker's, and the backend lets that one read everything (`GET`) and change nothing; like any service token, it may also upload a document. Set up once:

1. Cloudflare Zero Trust, Access, Service credentials: create a service token (say `finchie-agents`) that does not expire, and keep its Client ID and Client Secret.
2. The Finchie Access application's Service Auth policy, the one the mailbox's token is in: add the new token to it.
3. The backend: set `BACKEND_ACCESS_AGENT_CLIENT_ID` to the Client ID.
4. The machine the agent runs on: put `url`, `client_id`, and `client_secret` in the config file.

Leaked, the token reads the ledger until it is revoked in Cloudflare; it cannot change or delete anything.

## Commands

Each operation of the contract is a command named after its `operationId` in kebab case (`searchTransactions` is `search-transactions`), its query parameters are options named the same way (`hideTransfers` is `--hide-transfers`), and its path parameters come first, in order (`get-document 42`). A JSON body is `--body`: the JSON itself, `@file`, or `-` for stdin. `uploadDocument` takes a multipart upload, which the CLI does not send.

```sh
finchie --help                                   # every command, with its method and summary
finchie search-transactions --help               # its options, and every field of what it returns
finchie get-net-worth
finchie search-transactions --from 2026-01-01 --hide-transfers --all > transactions.ndjson
finchie get-document-preview-pdf 42 > statement.pdf
```

- **Output** is JSON on stdout, indented on a terminal and compact otherwise; a file (a PDF, the original e-mail) comes as its bytes, never onto a terminal.
- **`--all`**, on a command that pages (`limit` with `offset`, or with `before`), fetches every page at the largest size the contract allows and prints the items one per line (NDJSON). A search's totals are not items: ask for them with `--limit 0`.
- **Errors**: the API's answer goes to stderr with exit status 1, as does a failure to reach it; a wrong command or option exits with 2.

## For agents

[`.agents/skills/finchie/SKILL.md`](../../.agents/skills/finchie/SKILL.md) is what an agent needs to know: how to run the CLI, how to find a command, and the ledger's pitfalls for analysis (decimals, `flow` against `amount`, transfers). `.agents/skills/` is where agents such as Codex read a project's skills from (Claude Code reads only `.claude/skills/`), and one of the places the [skills CLI](https://github.com/vercel-labs/skills) looks, so a machine that wants it installs it from GitHub, for every project with `-g`:

```sh
npx skills add hsin19/finchie --skill finchie -g
```

The tests check that every command and option it names is in the contract, so a renamed operation fails CI instead of leaving the skill stale.

Everything an agent reads goes to its model's provider. Computing from a file keeps most of it out of the conversation.

## Code

| Module | |
|---|---|
| `config.py` | Settings: the config file and `FINCHIE_*` |
| `client.py` | HTTP with the service token's headers; a redirect means Access refused it |
| `contract.py` | The contract's operations, parameters, bodies, and answers |
| `help.py` | `--help`, all of it from the contract |
| `main.py` | The command line: builds the options, calls, writes the answer, `--all` |

Tests run against the backend's `openapi.yaml` as well, so every command of the real contract builds and has its help, and against the skill.
