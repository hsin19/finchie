# ADR 0010: Mail ingestion

## Decision

Statements arrive by themselves: a Gmail filter forwards each statement e-mail to an address on Cloudflare Email Routing, and an Email Worker, [`apps/mailbox`](../../apps/mailbox/), uploads it to the backend exactly as a person uploading the `.eml` would. This is the route ADR 0002 set out. No mailbox credential lives anywhere: Gmail pushes, nothing pulls.

```
bank ──▶ Gmail ──filter: sender is a bank, subject has 帳單──▶ finchie@<domain> (Email Routing)
     ──▶ mailbox Worker ──POST /api/documents, Access service token──▶ backend
```

| Part | Where | What it does |
|---|---|---|
| Gmail filter and forwarding address | Gmail, set up once by hand | Chooses what is a statement: the four banks' sender addresses and `帳單` in the subject (since ADR 0014, a filter generated from the parsers' sender domains, imported from the web app) |
| Email Routing | The deployment's Cloudflare configuration | `finchie@` runs the Worker; every other address is dropped. Email Routing rejects mail that fails the claimed sender's DMARC before the Worker runs; the banks' statements carry DKIM signatures that survive Gmail's forwarding |
| Worker | `apps/mailbox` | Takes only mail whose envelope sender is the owner's Gmail (`owner+caf_=…@gmail.com` when forwarded by a filter, the owner's own address when forwarded by hand) and uploads it whole. Mail from `google.com`, such as Gmail's forwarding confirmation, goes to the owner (only that confirmation now, a From header with just `forwarding-noreply@google.com`: anyone can get Google to mail the address, by sharing a Drive file with it for one); anything else is rejected at SMTP time and never stored |
| Service token | The deployment's Cloudflare configuration: a service token and a Service Auth policy on the Finchie Access application | The Worker's identity. Its JWT has no `email` claim, and the backend lets such a token `POST /api/documents` and nothing else (`internal/access`): leaked, it cannot read the ledger |
| Backend | unchanged pipeline | Dedupes, parses, reconciles, and links the upload like any other (ADRs 0005, 0009) |

**Failures are loud.** Rejecting a message (`setReject`) is a permanent SMTP error in Email Routing's session with Gmail, so the owner's Gmail gets a bounce: an upload the backend refused or could not take, or anything the Worker failed on, comes back as a bounce, and the original is still in Gmail to upload by hand. What Email Routing does when a Worker throws is not documented, so the Worker catches everything and rejects. What the backend takes but cannot use shows up in the app (a document marked skip or err, a statement marked chk), and a deployment can alert on err and chk: the backend reports `finchie.documents{status}`, document counts by status, with the rest of its telemetry (ADR 0011).

**Deployed by hand.** `task mailbox:deploy` from a machine logged in to wrangler. The Worker changes rarely, and deploying from CI would put a Cloudflare API token in this public repository. Its four settings are Worker secrets that whoever deploys it puts in (the service token stays with the deployment's Cloudflare configuration), so nothing personal is in this repository either. The Worker has to exist before Email Routing can route to it: deploy it, then add the route, then set its secrets.

**What the envelope check does not stop.** An SMTP envelope sender can be forged. Someone who knows the address could send mail with the owner's Gmail as envelope sender and a From domain of their own that passes DMARC; it would be uploaded. It only becomes data if it parses as one of the supported statements, it is visible as a document like any other, and the address is not published. Verifying Gmail's ARC seal in the Worker would close this; not worth it for now.

## Alternatives considered

- **Gmail API (OAuth).** What stalled V1: an unverified app's refresh tokens expire after seven days, and verifying an app with the `gmail.readonly` scope means a security assessment (ADR 0001).
- **IMAP polling with an app password.** Works, but an app password opens the whole mailbox and would live on a server.
- **Workers VPC to the backend, no public hostname.** Fits a service only Workers call. The backend already sits behind Access for people; a second, private way in would need its own authentication in the backend. A service token through the same front door keeps one.
- **The Worker checks that the original sender is a bank.** Another list to keep in step with the parsers when a bank is added; the Gmail filter already chooses, and a stray message costs one document marked skip.
