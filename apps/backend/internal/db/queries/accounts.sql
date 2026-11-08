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
-- The accounts the owner has: a credit_card account only holds its bank's card lines (ADR 0013).
SELECT id, institution, kind, number, label, name, full_number
FROM accounts
WHERE kind <> 'credit_card'
ORDER BY institution, kind DESC, number;

-- name: GetAccount :one
SELECT id, institution, kind, number, label, name, full_number
FROM accounts
WHERE id = @id AND kind <> 'credit_card';

-- name: UpdateAccountDetails :one
-- The owner's details (ADR 0016); a set flag that is false leaves its column as it is.
UPDATE accounts
SET name        = CASE WHEN @set_name::boolean THEN sqlc.narg(name)::text ELSE name END,
    full_number = CASE WHEN @set_full_number::boolean THEN sqlc.narg(full_number)::text ELSE full_number END
WHERE id = @id AND kind <> 'credit_card'
RETURNING id;

-- name: LatestBalances :many
-- Each account's newest balance in each unit, with its worth in NT$: money at the newest rate of its
-- unit (ADR 0015), NT$ being worth itself; a security's shares at its newest close (ADR 0017). A unit
-- with neither is worth nothing. rate_day is the day of the rate or close used.
SELECT DISTINCT ON (b.account_id, b.unit) b.account_id, b.unit, b.as_of, b.amount, b.statement_id,
       (CASE WHEN a.kind = 'securities' THEN round(b.amount * quote.close, 2)
             WHEN b.unit = 'TWD' THEN b.amount
             ELSE round(b.amount / rate.per_twd, 2) END)::numeric AS worth,
       coalesce(quote.day, rate.day)::date AS rate_day,
       coalesce(quote.code, '') AS code, quote.close AS price
FROM balances b
JOIN accounts a ON a.id = b.account_id
LEFT JOIN LATERAL (SELECT r.day, r.per_twd
                   FROM exchange_rates r
                   WHERE a.kind <> 'securities' AND r.unit = b.unit
                   ORDER BY r.day DESC
                   LIMIT 1) rate ON true
LEFT JOIN LATERAL (SELECT q.day, q.code, q.close
                   FROM quotes q
                   WHERE a.kind = 'securities' AND q.name = b.unit
                   ORDER BY q.day DESC
                   LIMIT 1) quote ON true
ORDER BY b.account_id, b.unit, b.as_of DESC, b.id DESC;

-- name: AccountBalances :many
SELECT account_id, unit, as_of, amount, statement_id
FROM balances
WHERE account_id = @account_id
ORDER BY as_of DESC, unit, id DESC;

-- name: AccountGaps :many
-- Where an account's balances do not join up (ADR 0013): two consecutive balances in one unit whose
-- difference the lines dated between them do not account for, so money moved that no report shows.
-- Deposit and securities accounts (a security's shares, ADR 0017): a card bill's lines are dated by
-- purchase, which can fall before the previous bill closed, so dates cannot place them between two bills.
-- Itemized: some statement has listed a line of the account in the unit. A deposit account's unit none
-- has may be one whose statements print only its balance (CTBC's passbook savings, 活儲有摺): a move
-- there is one no source shows, not a gap. A securities statement lists every trade, so a security
-- never traded is itemized all the same: a stock dividend is a gap.
WITH ordered AS (
    SELECT b.account_id, b.unit, b.as_of, b.amount,
           (a.kind = 'securities' OR EXISTS (
               SELECT 1 FROM transactions t WHERE t.account_id = b.account_id AND t.unit = b.unit))::boolean AS itemized,
           lag(b.as_of) OVER w AS prev_as_of,
           lag(b.amount) OVER w AS prev_amount
    FROM balances b
    JOIN accounts a ON a.id = b.account_id
    WHERE a.kind IN ('deposit', 'securities')
      AND (sqlc.narg(account_id)::bigint IS NULL OR b.account_id = sqlc.narg(account_id)::bigint)
    WINDOW w AS (PARTITION BY b.account_id, b.unit ORDER BY b.as_of, b.id)
)
SELECT o.account_id, o.unit, o.prev_as_of::date AS from_date, o.as_of AS to_date,
       (o.amount - o.prev_amount - coalesce(seen.amount, 0))::numeric AS unseen, o.itemized
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
       t.amount, t.balance,
       -- A trade's money (ADR 0017).
       tr.category AS trade_category, tr.price AS trade_price, tr.amount AS trade_amount, tr.fee AS trade_fee,
       tr.tax AS trade_tax, tr.net AS trade_net, tr.prepaid AS trade_prepaid,
       -- Where its money went or came from (ADR 0016).
       c.id AS counterpart_id, c.statement_id AS counterpart_statement_id, c.transaction_date AS counterpart_date,
       ca.id AS counterpart_account_id, ca.kind AS counterpart_account_kind, ca.institution AS counterpart_institution,
       coalesce(ca.name, ca.label) AS counterpart_account_name, ca.number AS counterpart_account_number,
       ca.full_number AS counterpart_full_number
FROM transactions t
LEFT JOIN trades tr ON tr.transaction_id = t.id
LEFT JOIN transactions c ON c.id = t.counterpart_id
LEFT JOIN accounts ca ON ca.id = c.account_id
WHERE t.account_id = @account_id
ORDER BY t.transaction_date DESC, t.statement_id DESC, t.line_no DESC
LIMIT @row_limit OFFSET @row_offset;
