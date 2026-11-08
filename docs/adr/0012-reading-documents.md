# ADR 0012: Reading documents

## Decision

A document opens in the browser without its PDF password, so statements are read in the app instead of the mailbox. Opening a document opens its statement PDF, decrypted, in a new tab; the browser's PDF viewer saves the decrypted copy. An e-mail no parser recognized opens instead on a page of its own with its headers, body, and attachments, to see what it is; a statement's page links to its e-mail the same way.

| | |
|---|---|
| Who opens it | The parser (`OpenDocument`). It already reads e-mails, and it is the only service that holds the PDF passwords (ADR 0006). The backend sends it the stored original on every request and passes on what comes back |
| Statement PDF | The parser marks one file primary: the PDF whose name a bank parser knows as its statement, as parsing picks it, else the first PDF (since 2.6.0 parsing and previews share one selection: a known name counts only when the PDF's text carries that bank's marker, then any PDF recognized by its text; locked or unreadable PDFs no longer stop the search). `/preview/pdf` serves it, so a link needs no second step through the e-mail |
| Storage | Nothing decrypted is stored. The database keeps the document exactly as uploaded, and `/api/documents/{id}/content` still serves it (since 2.6.0 the decrypted responses also carry `Cache-Control: no-store`, so the browser's cache does not keep them either) |
| PDFs | Decrypted with the first password that opens them, as for parsing (since ADR 0014, only the passwords of the parser that claims the e-mail). One that none opens comes back as received, marked locked, and the viewer asks for its password |
| Other attachments | Download as `application/octet-stream` with `nosniff`, like originals: only a PDF is shown in place on the app's origin |
| E-mail body | HTML when the message has it, else plain text wrapped in a page. Images the HTML references by Content-ID are embedded as `data:` URIs. `/preview/body` serves it into an iframe, as UTF-8 whatever the HTML's own `<meta charset>` says |
| Sandbox | The body's response carries its own Content-Security-Policy, so it is sandboxed even when its URL is opened alone: `sandbox` (no scripts, forms, or plugins; an origin of its own), `default-src 'none'`, `img-src data: https:` with `upgrade-insecure-requests`, inline styles only, framed only by the app. The iframe repeats the sandbox. Links open in a new tab outside the sandbox, without a referrer |
| Remote images | Load, as mail clients show them by default: every bank's statement e-mail draws its logo and layout from the bank's servers, some still over http |

## Alternatives considered

- **Store a decrypted copy at parse time.** One database read per view instead of a call to the parser, but a second copy of every statement, without its password, kept for good.
- **Decrypt in the backend.** The backend would need the PDF passwords as well: two services holding them instead of one.
- **Read the e-mail in the backend with Go's `net/mail`.** The parser already reads MIME with Python's `email` package, which decodes charsets, encoded headers, and nested multiparts; a second reader in Go would have to agree with it.
- **Block remote images.** Every bank's e-mail would show broken boxes, and the sandbox already stops anything that could run. The cost is that the bank's servers see when the e-mail is viewed.
- **Link to the message in the mailbox.** No code, but it ties the app to one mail provider and does not cover a document uploaded by hand.
