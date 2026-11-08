# ADR 0014: Recognizing statements

## Decision

Finchie takes statements as e-mails only. Each kind of statement e-mail has one parser, which alone decides that an e-mail is its own, opens its PDF, and cleans its data. A PDF is opened only with the passwords of the parser that claims it, worked out from the owner's personal details by the bank's own rule.

### E-mails only

An upload that is not an e-mail is refused before it is stored, and the web app's upload takes `.eml` files only. A bare PDF says nothing about where it came from: recognizing one needs its file name, which anyone can choose, or its text, which is behind the password. Documents already stored as bare PDFs are not carried over; nothing stored so far is worth a migration, so the deployment starts again from an empty database and gets its statements again as e-mails.

### Forwarded e-mails are unwrapped first

Before any parser looks, a shared step reduces an e-mail the owner forwarded by hand to the one the bank sent. The forward's own sender is the owner; the bank's sender, subject, and date are inside it:

| Forwarded as | Where the original is |
|---|---|
| An attachment (`message/rfc822`) | The attached message, whole: headers, body, and attachments |
| Inline, as Gmail forwards | The header block after `---------- Forwarded message ---------`, its labels in Gmail's language (`寄件者：` or `From:`); the attachments are the forward's own |

Forwarded more than once, the innermost original counts. Previews (ADR 0012) go through the same step and show the bank's headers. An e-mail a Gmail filter forwards needs none of this: it arrives with the bank's headers (ADR 0010).

The header block is plain text and proves nothing by itself, no more than a file name did. A claimed statement still has to open with that bank's password, carry its marker, and reconcile, and only the owner can upload (ADRs 0008, 0010).

### A parser claims an e-mail by its sender and subject

Each parser declares the sender domains and subject of its e-mail; the subject also tells one bank's card bill from its deposit statement. Domains, not addresses: banks send from hosts other than the address they tell customers to expect (Next Bank asks for `eStatement@nextbank.com.tw` to be trusted and sends from `no_reply@eblsdr.nextbank.com.tw`), and change them without notice. The parser then picks its statement among the attachments: the PDFs whose names it knows first, then the others, each opened with its own passwords only and accepted when its text carries the parser's marker (e-mails also carry payment slips and notices).

| Outcome | Status (ADR 0005) |
|---|---|
| No parser claims it | `UNIMPLEMENTED`, as before |
| A parser claims it, but none of its passwords opens its statement | `FAILED_PRECONDITION`, naming what is missing: `esun.deposit: needs PARSER_IDENTITY__NATIONAL_ID` |

### Passwords come from personal details, by each bank's rule

Banks build statement passwords from details they already hold, and say how in the e-mail. The national ID with a capital letter is the rule E.SUN, Taishin, CTBC, Fubon, and Next Bank state for their deposit statements, Shin Kong Securities for its statements, and Cathay United for its card bill. The rule is public and lives in the parser's code. The details are secrets and live in the parser's environment only (ADR 0006).

| Variable | Contents |
|---|---|
| `PARSER_IDENTITY__NATIONAL_ID` | National ID (身分證字號) |
| `PARSER_PDF_PASSWORDS__<INSTITUTION>` | Only for a bank whose password the owner changed. Tried before the rule, which still opens the statements from before the change |

A detail such as a birth date or a mobile number is added when a parser first needs it. A new bank whose rule uses details already set needs no configuration. Each rule cites where the bank states it; a rule the bank's e-mail does not state is marked as such.

### The Gmail filter comes from the parsers

Which e-mails reach Finchie is decided by the Gmail filter that forwards them (ADR 0010); which ones Finchie can read, by its parsers' claims. Kept apart, they drift: a filter on `帳單 信用卡` forwards no deposit statement. So the filter is generated from the claims, in Gmail's filter export format, and committed like other generated code (`task parser:generate`, checked in CI). The backend embeds it and serves it at `/api/mail-filters.xml`, linked from the web app.

| Filter part | Value |
|---|---|
| Matches | From any domain a parser claims, with an attachment. Coarse on purpose: the parser's claim is the precise test |
| Label | `Finchie`, so that "apply to matching conversations" also labels the statements already in Gmail, and a Takeout export of that label backfills them |
| Forwards to | A placeholder in the repository; the download puts in the deployment's mailbox address (`BACKEND_MAILBOX_ADDRESS`), which is not published (ADR 0010) |

It is a filter of its own, next to whatever filters the owner keeps for themselves; a filter of theirs that forwarded statements before stops forwarding, or each statement arrives twice. Importing it (Gmail settings, Filters, Import filters) is the one step a new kind of statement asks of the owner. Gmail adds imported filters next to existing ones, so the previous Finchie filter is deleted there by hand.

## Alternatives considered

- **Try every configured password on every PDF** (until now). Needs no knowledge of which bank a PDF is from, but every bank's password is tried on every attachment, a new bank needs a variable even when its password is one already set, and a failure says "none of the configured passwords" instead of what is missing.
- **Keep bare PDFs, recognized by file name.** A second way in, with weaker recognition, for documents that can be uploaded as their e-mails instead.
- **Recognize forwards by file name only.** Works for the attachment, but leaves the parser without the sender and subject it claims by, and previews without the bank's headers.
- **Recognize by text, then pick the password.** The text is inside the encrypted PDF.
- **Keep the Gmail filter by hand, or as a hand-written file.** It drifts from the parsers.
- **Have the backend ask the parser for its claims (a `ListParsers` RPC).** Always current without a generated file, but another RPC, and the filter could not be read in the repository.
- **Create the filter through the Gmail API.** Needs OAuth with a Gmail scope, the chore V1 died of (ADR 0001).

## Consequences

- `StatementParser` gains sender and subject patterns and a password rule; `filename_pattern` stays, now only choosing among an e-mail's attachments. Unwrapping goes where e-mails are read (`documents.py`), used by both parsing and previews.
- Previews open PDFs with the passwords of the parser that claims the e-mail. A PDF in an e-mail nothing claims is shown as received, marked locked.
- Deployments set `PARSER_IDENTITY__NATIONAL_ID`. A `PARSER_PDF_PASSWORDS__<INSTITUTION>` that only held the national ID can go; one that holds a password the owner chose stays, now used for its bank only.
- The real-statement test (ADR 0006) takes `.eml` files only. The bare PDFs kept for it need their e-mails, fetched once from Gmail like the deployment's.
- The backend gains `BACKEND_MAILBOX_ADDRESS` and the filter download; the proto does not change.
- When this lands: notes in ADRs 0006 and 0012 next to their sentences on passwords and bare PDFs and in ADR 0010 next to its filter, and updates to the root README (uploads, variables), `AGENTS.md` (real-statement test, generated code), the parser's README (variables), and the mailbox's README (filter setup).
