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

-- name: ListAccounts :many
SELECT id, institution, kind, number, label, name
FROM accounts
ORDER BY institution, kind DESC, number;

-- name: GetAccount :one
SELECT id, institution, kind, number, label, name
FROM accounts
WHERE id = @id;

-- name: LatestBalances :many
-- Each account's newest balance in each unit, with its worth in NT$ at the newest rate of the unit
-- (ADR 0015); NT$ is worth itself, a unit without a rate nothing.
SELECT DISTINCT ON (b.account_id, b.unit) b.account_id, b.unit, b.as_of, b.amount, b.statement_id,
       (CASE WHEN b.unit = 'TWD' THEN b.amount ELSE round(b.amount / rate.per_twd, 2) END)::numeric AS worth,
       rate.day AS rate_day
FROM balances b
LEFT JOIN LATERAL (SELECT r.day, r.per_twd
                   FROM exchange_rates r
                   WHERE r.unit = b.unit
                   ORDER BY r.day DESC
                   LIMIT 1) rate ON true
ORDER BY b.account_id, b.unit, b.as_of DESC, b.id DESC;

-- name: AccountBalances :many
SELECT account_id, unit, as_of, amount, statement_id
FROM balances
WHERE account_id = @account_id
ORDER BY as_of DESC, unit, id DESC;

-- name: AccountGaps :many
-- Where an account's balances do not join up (ADR 0013): two consecutive balances in one unit whose
-- difference the lines dated between them do not account for, so money moved that no report shows.
-- Deposit accounts only: a card bill's lines are dated by purchase, which can fall before the
-- previous bill closed, so dates cannot place them between two bills.
WITH ordered AS (
    SELECT b.account_id, b.unit, b.as_of, b.amount,
           lag(b.as_of) OVER w AS prev_as_of,
           lag(b.amount) OVER w AS prev_amount
    FROM balances b
    JOIN accounts a ON a.id = b.account_id
    WHERE a.kind = 'deposit'
      AND (sqlc.narg(account_id)::bigint IS NULL OR b.account_id = sqlc.narg(account_id)::bigint)
    WINDOW w AS (PARTITION BY b.account_id, b.unit ORDER BY b.as_of, b.id)
)
SELECT o.account_id, o.unit, o.prev_as_of::date AS from_date, o.as_of AS to_date,
       (o.amount - o.prev_amount - coalesce(seen.amount, 0))::numeric AS unseen
FROM ordered o
LEFT JOIN LATERAL (SELECT sum(t.amount) AS amount
                   FROM transactions t
                   WHERE t.account_id = o.account_id AND t.unit = o.unit
                     AND t.transaction_date > o.prev_as_of AND t.transaction_date <= o.as_of) seen ON true
WHERE o.prev_as_of IS NOT NULL
  AND o.amount - o.prev_amount <> coalesce(seen.amount, 0)
ORDER BY o.account_id, o.unit, o.as_of;

-- name: AccountLines :many
-- Newest first. Amounts as the source reports them: money in is positive on a deposit account, a
-- purchase is positive on a card bill.
SELECT t.id, t.statement_id, t.transaction_date, t.description, t.memo, t.counterparty, t.kind, t.unit,
       t.amount, t.balance
FROM transactions t
WHERE t.account_id = @account_id
ORDER BY t.transaction_date DESC, t.statement_id DESC, t.line_no DESC
LIMIT @row_limit OFFSET @row_offset;
