-- Snapshots: what a source reports an account holds, with no statement, such as an exchange's API (ADR 0018).

-- name: ClearSnapshot :exec
-- A later snapshot on the same day replaces that day's.
DELETE FROM balances
WHERE account_id = @account_id AND as_of = @as_of::date AND statement_id IS NULL;

-- name: InsertSnapshot :exec
INSERT INTO balances (account_id, unit_id, as_of, amount)
SELECT @account_id, unnest(@unit_ids::bigint[]), @as_of::date, unnest(@amounts::numeric[]);

-- name: ImplySoldOut :exec
-- A snapshot is the whole account: a unit whose newest earlier balance is not 0 and that the snapshot does
-- not list is at 0 on its day.
INSERT INTO balances (account_id, unit_id, as_of, amount)
SELECT @account_id, prev.unit_id, @as_of::date, 0
FROM (SELECT DISTINCT ON (b.unit_id) b.unit_id, b.amount
      FROM balances b
      WHERE b.account_id = @account_id AND b.as_of < @as_of::date
      ORDER BY b.unit_id, b.as_of DESC, b.id DESC) prev
WHERE prev.amount <> 0
  AND NOT EXISTS (SELECT 1 FROM balances s
                  WHERE s.account_id = @account_id AND s.as_of = @as_of::date AND s.unit_id = prev.unit_id
                    AND s.statement_id IS NULL);

-- name: ClearValuation :exec
DELETE FROM valuations WHERE account_id = @account_id AND as_of = @as_of::date;

-- name: InsertValuation :exec
INSERT INTO valuations (account_id, as_of, unit_id, amount)
VALUES (@account_id, @as_of::date, @unit_id, @amount);
