# ADR 0002: Architecture

## Decision

Two services and a web app, deployed on the homelab:

```
browser ── Cloudflare Access ── Tunnel ──▶ backend (Go) ── gRPC ──▶ parser (Python)
                                             │
                                             └── Postgres (shared homelab instance, own database and role)
```

- **backend** is the only public service. It serves the API and, later, the web app's static files (since ADR 0007); it ingests documents, runs background jobs, and owns the database and its schema.
- **parser** is internal (Compose network only) and stateless: a document goes in, structured data comes out. It has no database, no queue, and no storage.
- **web** is a TypeScript app built to static files and served by the backend.

## Why the parser is its own service

Not for scale. The boundary follows how the code changes: parsers change when a bank changes its statement layout, everything else changes when features are added. Splitting them lets each side use the language that suits it (PDF and OCR tooling is strongest in Python) behind one narrow contract. Taking over InTheGreenYet later touches the backend and the web app only.

## Why gRPC between them

The call is internal and service-to-service, and both sides get types generated from one proto file. Deadlines and status codes map directly onto retry decisions (ADR 0005). It is also a deliberate learning goal (ADR 0003).

## Why the queue lives in the backend

Parsing one statement takes under a second, or a few seconds with OCR, and volume is tens of documents a month. A background job in the backend calls the parser synchronously with a deadline; retries, back-off, and concurrency limits belong to the job queue. The parser never needs to know about queues. Revisit only if a single parse starts taking minutes, for example with a local LLM.

## Why not more services

There is one user and no independent scaling or team boundary to justify them. Each extra service costs a deployment, monitoring, and another contract. New ingestion paths are adapters that end in the backend's upload API:

1. Manual upload (first).
2. A local backfill tool that uploads old statements fetched over IMAP. (Never built: a Takeout export of the Gmail filter's label backfills instead, ADR 0014.)
3. Later: a Gmail filter forwards statements to a Cloudflare Email Worker, which uploads them. No Gmail credential lives on any server. (Built with ADR 0010.)

Investment providers (exchange APIs) become backend jobs. They need the homelab's residential IP anyway, so they no longer go through a relay.

## Consequences

- The backend is the only thing that needs authentication: Cloudflare Access in front of it.
- Local development needs Postgres (Docker) plus both services; the parser can be developed and tested on its own.
