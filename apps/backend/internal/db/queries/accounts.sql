-- name: UpsertAccount :one
-- The account a source reports, created the first time it appears (ADR 0013); the source's name for
-- it is kept as last reported.
INSERT INTO accounts (institution, kind, number, label)
VALUES (@institution, @kind, @number, sqlc.narg(label))
ON CONFLICT (institution, kind, number) DO UPDATE SET label = coalesce(EXCLUDED.label, accounts.label)
RETURNING id;

-- name: InsertBalance :exec
INSERT INTO balances (account_id, unit, as_of, amount, statement_id)
VALUES (@account_id, @unit, @as_of, @amount, @statement_id);
