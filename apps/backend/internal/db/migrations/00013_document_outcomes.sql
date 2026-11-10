-- +goose Up

-- What became of parsing a document is stored as such (ADR 0005), never read back out of its error's
-- words: a document no parser handles is skipped, a failure says what has to change before parsing it
-- again can succeed, and a duplicate names the document that holds its statement. `error` keeps only the
-- details, for the owner to read.
ALTER TABLE documents
    DROP CONSTRAINT documents_status_check,
    ADD CONSTRAINT documents_status_check
        CHECK (status IN ('pending', 'parsed', 'needs_review', 'duplicate', 'failed', 'skipped')),
    ADD COLUMN failure TEXT
        CHECK (failure IN ('unsupported', 'locked', 'unreadable', 'layout', 'unavailable', 'invalid_response', 'other')),
    ADD COLUMN duplicate_of BIGINT REFERENCES documents (id) ON DELETE SET NULL;

-- Failures were recorded as "<gRPC code>: <message>" (or the backend's own reason).
UPDATE documents
SET failure = CASE
        WHEN error LIKE 'Unimplemented:%' THEN 'unsupported'
        WHEN error LIKE 'FailedPrecondition:%' THEN 'locked'
        WHEN error LIKE 'InvalidArgument:%' THEN 'unreadable'
        WHEN error LIKE 'Internal:%' THEN 'layout'
        WHEN error ~ '^(Unavailable|DeadlineExceeded|ResourceExhausted|Canceled):' THEN 'unavailable'
        WHEN error LIKE 'invalid parser response%' THEN 'invalid_response'
        ELSE 'other'
    END,
    status = CASE WHEN error LIKE 'Unimplemented:%' THEN 'skipped' ELSE status END,
    error = regexp_replace(error,
        '^(Unimplemented|FailedPrecondition|InvalidArgument|Internal|Unavailable|DeadlineExceeded|ResourceExhausted|Canceled): ', '')
WHERE status = 'failed';

-- Duplicates were recorded as "same statement as document <id>".
UPDATE documents d
SET duplicate_of = o.id, error = NULL
FROM documents o
WHERE d.status = 'duplicate' AND o.id = substring(d.error FROM 'document (\d+)$')::bigint;

ALTER TABLE documents
    ADD CONSTRAINT documents_failure_status_check CHECK ((status IN ('failed', 'skipped')) = (failure IS NOT NULL)),
    ADD CONSTRAINT documents_skipped_check CHECK ((status = 'skipped') = (failure IS NOT DISTINCT FROM 'unsupported')),
    ADD CONSTRAINT documents_duplicate_of_check CHECK (duplicate_of IS NULL OR status = 'duplicate');

-- +goose Down
UPDATE documents d
SET error = 'same statement as document ' || d.duplicate_of
WHERE d.status = 'duplicate' AND d.duplicate_of IS NOT NULL;

UPDATE documents
SET error = CASE failure
        WHEN 'unsupported' THEN 'Unimplemented: '
        WHEN 'locked' THEN 'FailedPrecondition: '
        WHEN 'unreadable' THEN 'InvalidArgument: '
        WHEN 'layout' THEN 'Internal: '
        WHEN 'unavailable' THEN 'Unavailable: '
        ELSE ''
    END || coalesce(error, ''),
    status = 'failed'
WHERE status IN ('failed', 'skipped');

ALTER TABLE documents
    DROP CONSTRAINT documents_duplicate_of_check,
    DROP CONSTRAINT documents_skipped_check,
    DROP CONSTRAINT documents_failure_status_check,
    DROP COLUMN duplicate_of,
    DROP COLUMN failure,
    DROP CONSTRAINT documents_status_check,
    ADD CONSTRAINT documents_status_check
        CHECK (status IN ('pending', 'parsed', 'needs_review', 'duplicate', 'failed'));
