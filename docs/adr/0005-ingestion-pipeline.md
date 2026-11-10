# ADR 0005: Ingestion pipeline

## Decision

### Originals are the source of truth

Every uploaded file is stored as-is in Postgres (`documents`, `BYTEA`) with its SHA-256, which is unique. Uploading the same bytes again returns the existing document. Everything derived from a document (statements, transactions) can be deleted and rebuilt by parsing it again. A statement is a few hundred KB to about 1.5 MB and arrives about once a month per bank, so the database stays small, and one `pg_dump` backs up everything.

### Flow

1. `POST` upload → insert the document (or find it by hash) → enqueue a `parse_document` job.
2. The job calls `ParseDocument` on the parser with a 120-second deadline.
3. In one database transaction, the job replaces everything previously derived from that document and records the result, the parser name, and the parser version.

Re-parsing (after a parser fix) runs the same job again.

### Reconciliation

The parser adds up the transactions that make up a statement's new charges and compares the sum with the amount printed on the statement. Which lines count, and which printed amount to compare with, is bank-specific and decided by each bank's parser. The result travels with the parsed data. An unbalanced statement is stored but marked `needs_review` and is not treated as correct.

### Duplicates

The same statement can arrive as an e-mail and as a bare PDF, which hash differently. A statement is identified by institution and closing date (later also by kind and period start, and by the accounts it reports: ADR 0013); a second document for the same statement is marked `duplicate`, names the document that holds the statement (`duplicate_of`), and its data is not stored again.

### Errors and retries

The gRPC status decides whether retrying can help:

| Status | Meaning | Job outcome |
|---|---|---|
| `OK` | Parsed (balanced or not) | Store; `parsed` or `needs_review` |
| `UNIMPLEMENTED` | No parser recognizes the document | `failed`, no retry (`skipped` since migration 00013: nothing to fix or retry) |
| `INVALID_ARGUMENT`, `FAILED_PRECONDITION` | Corrupt file, wrong or missing PDF password | `failed`, no retry |
| `UNAVAILABLE`, `DEADLINE_EXCEEDED`, `RESOURCE_EXHAUSTED` | Parser down or overloaded | Retry with back-off |
| anything else | Parser bug | `failed`, no retry: a bug does not fix itself |

(Since migration 00013 the outcome is stored as such, never read back out of the error's words: `documents.failure` says what has to change before parsing again can succeed (`unsupported`, `locked`, `unreadable`, `layout`, `unavailable`, `invalid_response`, `other`), `error` only the details. A failed document holds no statement: whatever an earlier parse of it derived is deleted, so nothing a parser can no longer read stays counted. A document that failed only because the parser stayed out of reach through every retry, or that stays `pending` with no job, is queued again every hour by `retry_documents`, so neither waits for the owner. So is one whose parser has a newer version than the one that read it (`DescribeParsers`), and one that failed under an earlier parser instance: passwords, the owner's details, and OCR are set when the parser starts, and a new parser comes with a new instance.)

## Background

V1 parsed during fetching and kept nothing but the output. When the Taishin parser broke, the only fix was to fetch the mail again, and nothing noticed it had broken (ADR 0001).
