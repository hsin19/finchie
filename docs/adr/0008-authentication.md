# ADR 0008: Authentication

## Decision

Finchie has one user. Signing in is Cloudflare Access's job, at the edge; the backend has no login of its own, but it refuses any request that did not come through Access.

| Layer | What it does |
|---|---|
| Cloudflare Access | Fronts the whole hostname. Signs in with **Google** (one click, no e-mailed code), allows only the owner's address, and keeps the session for a month. Requests without a session never reach the homelab |
| Tunnel | The homelab opens no ports: from outside, only `cloudflared` reaches the backend. Other containers on its Docker network can reach it too, which the backend check covers. The parser sits on an internal network that only the backend shares (and, since it opens statements with the owner's passwords, it can also ask callers for a token, `PARSER_TOKEN`, which the backend sends) |
| Backend | Verifies the JWT Access attaches to every request it lets through (`Cf-Access-Jwt-Assertion`): signature against the team's published keys, issuer, and the application's AUD tag. Anything else gets 401. `/healthz` is exempt: Docker and the monitoring probe call it from inside the network |

The backend check is configured with `BACKEND_ACCESS_TEAM_DOMAIN` and `BACKEND_ACCESS_AUD`. Both empty turns it off for local development (the backend logs a warning); setting only one is a startup error, since it would look like protection while checking nothing.

A machine can come through Access too, with a service token: its JWT carries no e-mail address, and the backend lets it upload documents and nothing else (the mailbox Worker, ADR 0010).

Without the backend check, one mistake in the Cloudflare configuration (a route added without its Access application) would publish the ledger, and nothing would say so.

## Alternatives considered

- **Access with its e-mailed one-time code.** Built in, no OAuth client to set up. Too slow for a page opened every day.
- **OAuth in the backend (Google, then GitHub).** Works without Cloudflare and is good practice, but makes the backend public, its login the only barrier, and adds sessions, cookies, and CSRF handling to maintain. Access gives the same one-click Google sign-in at the edge.
- **Access alone, no backend check.** One layer: the Cloudflare configuration would be the only thing standing between the internet and the data.
