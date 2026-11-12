-- name: UpsertDocument :one
-- Returns the existing row when the same bytes were uploaded before; `created` tells which.
INSERT INTO documents (sha256, filename, content_type, content)
VALUES (@sha256, @filename, @content_type, @content)
ON CONFLICT (sha256) DO UPDATE SET sha256 = EXCLUDED.sha256
RETURNING id, (xmax = 0)::boolean AS created;

-- name: GetDocument :one
-- With what it produced: its statement's kind, and how many lines and accounts it reported.
SELECT d.id, d.sha256, d.filename, d.content_type, octet_length(d.content)::integer AS size_bytes, d.received_at,
       d.status, d.failure, d.error, d.duplicate_of, d.parser, d.parser_version, d.warnings, d.parsed_at,
       s.kind AS statement_kind,
       (SELECT count(*) FROM transactions t WHERE t.statement_id = s.id)::integer AS line_count,
       (SELECT count(DISTINCT b.account_id) FROM balances b WHERE b.statement_id = s.id)::integer AS account_count
FROM documents d
LEFT JOIN statements s ON s.document_id = d.id
WHERE d.id = @id;

-- name: ListDocuments :many
-- The same columns as GetDocument, newest first. `before` continues the list after that document.
SELECT d.id, d.sha256, d.filename, d.content_type, octet_length(d.content)::integer AS size_bytes, d.received_at,
       d.status, d.failure, d.error, d.duplicate_of, d.parser, d.parser_version, d.warnings, d.parsed_at,
       s.kind AS statement_kind,
       (SELECT count(*) FROM transactions t WHERE t.statement_id = s.id)::integer AS line_count,
       (SELECT count(DISTINCT b.account_id) FROM balances b WHERE b.statement_id = s.id)::integer AS account_count
FROM documents d
LEFT JOIN statements s ON s.document_id = d.id
WHERE (sqlc.narg(before)::bigint IS NULL
       OR (d.received_at, d.id) < (SELECT b.received_at, b.id FROM documents b WHERE b.id = sqlc.narg(before)::bigint))
  AND (sqlc.narg(states)::text[] IS NULL OR d.status = ANY (sqlc.narg(states)::text[]))
  AND (sqlc.narg(ids)::bigint[] IS NULL OR d.id = ANY (sqlc.narg(ids)::bigint[]))
  AND (sqlc.narg(query)::text IS NULL OR strpos(lower(d.filename), lower(sqlc.narg(query)::text)) > 0)
ORDER BY d.received_at DESC, d.id DESC
LIMIT @row_limit;

-- name: GetDocumentContent :one
SELECT filename, content_type, content
FROM documents
WHERE id = @id;

-- name: LockDocument :one
-- Its status, and no parse or retry may change it until the transaction ends.
SELECT status
FROM documents
WHERE id = @id
FOR UPDATE;

-- name: MarkDuplicatesPending :many
-- The documents that say this one holds their statement, to be parsed again: one of them will hold it.
UPDATE documents
SET status = 'pending', failure = NULL, error = NULL, duplicate_of = NULL
WHERE duplicate_of = @id::bigint
RETURNING id;

-- name: DeleteDocument :exec
DELETE FROM documents
WHERE id = @id;

-- name: MarkDocumentPending :exec
UPDATE documents
SET status = 'pending', failure = NULL, error = NULL, duplicate_of = NULL
WHERE id = @id;

-- name: FinishDocument :exec
-- What became of parsing it (ADR 0005): a failure when it failed or was skipped, the document holding its
-- statement when a duplicate.
UPDATE documents
SET status         = @status,
    failure        = sqlc.narg(failure),
    error          = sqlc.narg(error),
    duplicate_of   = sqlc.narg(duplicate_of),
    parser_instance = sqlc.narg(parser_instance),
    parser         = sqlc.narg(parser),
    parser_version = sqlc.narg(parser_version),
    warnings       = @warnings,
    parsed_at      = now()
WHERE id = @id;

-- name: DocumentCounts :many
-- Documents by status; with a query, only documents whose filename holds it.
SELECT status, count(*)::integer AS documents
FROM documents
WHERE sqlc.narg(query)::text IS NULL OR strpos(lower(filename), lower(sqlc.narg(query)::text)) > 0
GROUP BY status;

-- name: StalledDocuments :many
-- Documents a parse job should be at work on and may not be: pending ones, whose job may have been lost
-- (a backend killed mid-job, a failure that could not be recorded), and ones that failed only because the
-- parser was out of reach.
SELECT id
FROM documents
WHERE status = 'pending' OR (status = 'failed' AND failure = 'unavailable')
ORDER BY id;

-- name: OutdatedDocuments :many
-- Documents that parsing again may change (ADR 0005): one a parser of another version than today's read
-- (`current` holds "name@version" of each), and one that failed, other than for an unreachable parser,
-- under another instance than `instance`, since its configuration may have changed.
SELECT id
FROM documents
WHERE (status IN ('parsed', 'needs_review', 'duplicate')
       AND coalesce(parser, '') || '@' || coalesce(parser_version, '') <> ALL (@current::text[]))
   OR (status IN ('failed', 'skipped') AND failure <> 'unavailable'
       AND parser_instance IS DISTINCT FROM @instance::text)
ORDER BY id;
