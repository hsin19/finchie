# mailbox

A Cloudflare Email Worker that takes statements Gmail forwards to an address on Email Routing and
uploads each e-mail to the backend, exactly as a person uploading the `.eml` would. Design and
reasons: [ADR 0010](../../docs/adr/0010-mail-ingestion.md).

```
bank ──▶ Gmail ──filter, forward──▶ finchie@<domain> (Email Routing) ──▶ this Worker
     ──POST /api/documents, Access service token──▶ backend (dedupe, parse, reconcile as usual)
```

The Gmail filter comes from the backend: the web app's documents page links `/api/mail-filters.xml`, generated from the parsers' claims (each one's sender domains and subject terms, so a bank's other e-mails stay out) and forwarding to `BACKEND_MAILBOX_ADDRESS` ([ADR 0014](../../docs/adr/0014-recognizing-statements.md)). Import it in Gmail (Settings, Filters and Blocked Addresses, Import filters) with "apply to matching conversations" if the old statements should get its label too, and import it again after a new kind of statement is supported, deleting the previous one. A filter of the owner's own that already forwards statements has to stop forwarding, or each arrives twice.

| Mail | What happens |
|---|---|
| Forwarded by the owner's Gmail (envelope sender `owner+caf_=…@gmail.com`, or the owner's own address) | Uploaded. Anything the backend cannot parse shows up as a document marked skip or err |
| Gmail's "confirm forwarding" message (one From address, `forwarding-noreply@google.com`) | Forwarded to the owner, who confirms the new forwarding address with it. Email Routing checks DMARC first, so the From header can be trusted |
| Anything else, other mail from `google.com` included (anyone can share a Drive file with the address) | Rejected at SMTP time; nothing is stored |
| Upload fails (backend down, token refused) or anything else goes wrong | Rejected, so the bounce reaches the owner's Gmail: upload that one by hand |

## Settings

All four are Worker secrets, so nothing personal is in this repository: whoever deploys the Worker
puts them in (`wrangler secret put`, or Cloudflare's API). The route (an Email Routing address that
runs this Worker) and the service token belong to the deployment's Cloudflare configuration too.

| Secret | What it is |
|---|---|
| `FINCHIE_URL` | The backend's public URL |
| `OWNER_EMAIL` | The Gmail address that forwards statements |
| `ACCESS_CLIENT_ID`, `ACCESS_CLIENT_SECRET` | The Access service token; the backend lets it upload and nothing else |

Until all four are set the Worker rejects every message.

`OWNER_EMAIL` has to be a verified destination address in Email Routing before the finchie address is
added as a forwarding address in Gmail: `message.forward()` only reaches verified destinations, so
until then Gmail's confirmation bounces instead of reaching the owner.

## Commands

```bash
task mailbox:install
task mailbox:check     # dprint, tsc, tests, and a bundle (wrangler deploy --dry-run)
task mailbox:deploy    # by hand; `pnpm exec wrangler login` once first
```

Deploys are by hand: the Worker changes rarely, and a CI deploy would need a Cloudflare API token in
this repository.
