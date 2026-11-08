-- name: UpsertAccount :one
-- The account a source reports, created the first time it appears (ADR 0013).
INSERT INTO accounts (institution, kind, number)
VALUES (@institution, @kind, @number)
ON CONFLICT (institution, kind, number) DO UPDATE SET number = EXCLUDED.number
RETURNING id;

-- name: InsertCardBillBalance :exec
-- A card bill's balance is what it says is owed, on its closing date.
INSERT INTO balances (account_id, unit, as_of, amount, statement_id)
SELECT @account_id, s.currency, s.closing_date, -s.total_due, s.id
FROM statements s
WHERE s.id = @statement_id;
