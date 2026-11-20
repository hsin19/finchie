-- name: LockNotifications :exec
-- Notices are worked out by one at a time: after a document is stored, after the owner changes something,
-- and every hour.
SELECT pg_advisory_xact_lock(4243712);

-- name: TroubledDocuments :many
-- The documents whose statement does not add up, or that failed to parse.
SELECT id, filename, status, failure
FROM documents
WHERE status IN ('needs_review', 'failed')
ORDER BY id;

-- name: RaiseNotifications :many
-- Raises each situation once, all in one statement: `notices` is a JSON array of them, keys distinct. One
-- still open keeps what the owner did with it and takes the newest facts; one that was resolved and is so
-- again is raised again, as news. A row comes back for each that changed, `raised` when it is news: new,
-- or so again.
INSERT INTO notifications (kind, key, account_id, document_id, facts)
SELECT n.kind, n.key, n.account_id, n.document_id, n.facts
FROM jsonb_to_recordset(@notices::jsonb) AS n(kind text, key text, account_id bigint, document_id bigint, facts jsonb)
ON CONFLICT (key) DO UPDATE SET
    facts       = EXCLUDED.facts,
    raised_at   = CASE WHEN notifications.resolved_at IS NULL THEN notifications.raised_at ELSE now() END,
    read_at     = CASE WHEN notifications.resolved_at IS NULL THEN notifications.read_at END,
    handled_at  = CASE WHEN notifications.resolved_at IS NULL THEN notifications.handled_at END,
    resolved_at = NULL
WHERE notifications.resolved_at IS NOT NULL OR notifications.facts IS DISTINCT FROM EXCLUDED.facts
RETURNING key, id, raised_at, (raised_at = now())::boolean AS raised;

-- name: ResolveNotifications :exec
-- Resolves the open situations that are no longer so: every one not among `keys`, except a document's
-- while it is being parsed again, which says nothing yet.
UPDATE notifications n
SET resolved_at = now()
WHERE n.resolved_at IS NULL
  AND n.key <> ALL (@keys::text[])
  AND NOT EXISTS (SELECT 1 FROM documents d WHERE d.id = n.document_id AND d.status = 'pending');

-- name: ListNotifications :many
-- Newest first, with the account each is about as it is named now; with `open`, only those not resolved.
SELECT n.id, n.kind, n.facts, n.raised_at, n.read_at, n.handled_at, n.resolved_at, n.document_id,
       a.id AS account_id, a.institution, a.kind AS account_kind, a.label, a.name
FROM notifications n
LEFT JOIN accounts a ON a.id = n.account_id
WHERE NOT @open::boolean OR n.resolved_at IS NULL
ORDER BY n.raised_at DESC, n.id DESC
LIMIT @row_limit;

-- name: CountUnreadNotifications :one
-- Open ones the owner has neither read nor handled.
SELECT count(*)::integer
FROM notifications
WHERE resolved_at IS NULL AND read_at IS NULL AND handled_at IS NULL;

-- name: ReadNotifications :exec
UPDATE notifications SET read_at = now() WHERE id = ANY (@ids::bigint[]) AND read_at IS NULL;

-- name: HandleNotifications :exec
-- Handling one reads it too; taking that back leaves it read.
UPDATE notifications
SET handled_at = CASE WHEN @handled::boolean THEN coalesce(handled_at, now()) END,
    read_at    = coalesce(read_at, now())
WHERE id = ANY (@ids::bigint[]);

-- name: AnyDocumentPending :one
-- Whether a document is being parsed: its ledger, and so what needs the owner, is still changing.
SELECT EXISTS (SELECT 1 FROM documents WHERE status = 'pending');
