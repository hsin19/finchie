-- name: UpsertDocument :one
-- Returns the existing row when the same bytes were uploaded before; `created` tells which.
INSERT INTO documents (sha256, filename, content_type, content)
VALUES (@sha256, @filename, @content_type, @content)
ON CONFLICT (sha256) DO UPDATE SET sha256 = EXCLUDED.sha256
RETURNING id, (xmax = 0)::boolean AS created;

-- name: GetDocument :one
-- With what it produced: its statement's kind, and how many lines and accounts it reported.
SELECT d.id, d.sha256, d.filename, d.content_type, octet_length(d.content)::integer AS size_bytes, d.received_at,
       d.status, d.error, d.parser, d.parser_version, d.warnings, d.parsed_at,
       s.kind AS statement_kind,
       (SELECT count(*) FROM transactions t WHERE t.statement_id = s.id)::integer AS line_count,
       (SELECT count(DISTINCT b.account_id) FROM balances b WHERE b.statement_id = s.id)::integer AS account_count
FROM documents d
LEFT JOIN statements s ON s.document_id = d.id
WHERE d.id = @id;

-- name: ListDocuments :many
-- The same columns as GetDocument, newest first. `before` continues the list after that document;
-- states are as DocumentCounts names them.
SELECT d.id, d.sha256, d.filename, d.content_type, octet_length(d.content)::integer AS size_bytes, d.received_at,
       d.status, d.error, d.parser, d.parser_version, d.warnings, d.parsed_at,
       s.kind AS statement_kind,
       (SELECT count(*) FROM transactions t WHERE t.statement_id = s.id)::integer AS line_count,
       (SELECT count(DISTINCT b.account_id) FROM balances b WHERE b.statement_id = s.id)::integer AS account_count
FROM documents d
LEFT JOIN statements s ON s.document_id = d.id
WHERE (sqlc.narg(before)::bigint IS NULL
       OR (d.received_at, d.id) < (SELECT b.received_at, b.id FROM documents b WHERE b.id = sqlc.narg(before)::bigint))
  AND (sqlc.narg(states)::text[] IS NULL
       OR (CASE WHEN d.status = 'failed' AND d.error LIKE 'Unimplemented%' THEN 'skipped' ELSE d.status END)
          = ANY (sqlc.narg(states)::text[]))
  AND (sqlc.narg(query)::text IS NULL OR strpos(lower(d.filename), lower(sqlc.narg(query)::text)) > 0)
ORDER BY d.received_at DESC, d.id DESC
LIMIT @row_limit;

-- name: GetDocumentContent :one
SELECT filename, content_type, content
FROM documents
WHERE id = @id;

-- name: MarkDocumentPending :exec
UPDATE documents
SET status = 'pending', error = NULL
WHERE id = @id;

-- name: FinishDocument :exec
UPDATE documents
SET status         = @status,
    error          = sqlc.narg(error),
    parser         = sqlc.narg(parser),
    parser_version = sqlc.narg(parser_version),
    warnings       = @warnings,
    parsed_at      = now()
WHERE id = @id;

-- name: DocumentCounts :many
-- Documents by status as the web app shows it: a failure because no parser handles the file is
-- "skipped", since there is nothing to fix or retry.
-- With a query, only documents whose filename holds it.
SELECT (CASE WHEN status = 'failed' AND error LIKE 'Unimplemented%' THEN 'skipped' ELSE status END)::text AS status,
       count(*)::integer AS documents
FROM documents
WHERE sqlc.narg(query)::text IS NULL OR strpos(lower(filename), lower(sqlc.narg(query)::text)) > 0
GROUP BY 1;
