-- Points (ADR 0019).

-- name: InsertExpiry :exec
-- What a statement says will lapse; deleted with it.
INSERT INTO expiries (account_id, unit_id, amount, lapses_by, statement_id)
VALUES (@account_id, @unit_id, @amount, @lapses_by, @statement_id::bigint);

-- name: LatestExpiries :many
-- What will lapse of each account's units, by their newest report: the newest statement that reports the
-- unit at all, so one that prints nothing lapsing says nothing will; and the owner's for an account they
-- keep (ADR 0019). Amounts of 0 and dates gone by in Taipei are left out.
WITH reports AS (
    SELECT DISTINCT ON (b.account_id, b.unit_id) b.account_id, b.unit_id, b.statement_id
    FROM balances b
    JOIN statements s ON s.id = b.statement_id
    ORDER BY b.account_id, b.unit_id, s.closing_date DESC, s.id DESC
), reported AS (
    SELECT e.account_id, e.unit_id, e.amount, e.lapses_by
    FROM expiries e
    JOIN reports r ON r.account_id = e.account_id AND r.unit_id = e.unit_id AND r.statement_id = e.statement_id
    UNION ALL
    SELECT e.account_id, e.unit_id, e.amount, e.lapses_by
    FROM expiries e
    WHERE e.statement_id IS NULL
)
SELECT r.account_id, u.symbol AS unit, r.amount, r.lapses_by
FROM reported r
JOIN units u ON u.id = r.unit_id
WHERE r.amount > 0 AND r.lapses_by >= (now() AT TIME ZONE 'Asia/Taipei')::date
ORDER BY r.account_id, r.lapses_by, u.symbol;

-- Accounts the owner keeps (ADR 0019).

-- name: CreateOwnerAccount :one
-- The name the owner opens it with is its own, its label, as a bank's account has the bank's: a name they
-- give it later goes over it, and clearing that brings it back.
INSERT INTO accounts (institution, kind, label)
VALUES ('owner', @kind, @label)
RETURNING id;

-- name: GetAccountSource :one
SELECT institution, kind FROM accounts WHERE id = @id AND kind <> 'credit_card';

-- name: ClearOwnerExpiries :exec
DELETE FROM expiries WHERE account_id = @account_id AND statement_id IS NULL;

-- name: InsertOwnerExpiry :exec
INSERT INTO expiries (account_id, unit_id, amount, lapses_by)
VALUES (@account_id, @unit_id, @amount, @lapses_by);
