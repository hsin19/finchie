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
-- The same columns as GetDocument.
SELECT d.id, d.sha256, d.filename, d.content_type, octet_length(d.content)::integer AS size_bytes, d.received_at,
       d.status, d.error, d.parser, d.parser_version, d.warnings, d.parsed_at,
       s.kind AS statement_kind,
       (SELECT count(*) FROM transactions t WHERE t.statement_id = s.id)::integer AS line_count,
       (SELECT count(DISTINCT b.account_id) FROM balances b WHERE b.statement_id = s.id)::integer AS account_count
FROM documents d
LEFT JOIN statements s ON s.document_id = d.id
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
SELECT (CASE WHEN status = 'failed' AND error LIKE 'Unimplemented%' THEN 'skipped' ELSE status END)::text AS status,
       count(*)::integer AS documents
FROM documents
GROUP BY 1;
