-- name: UpsertAccount :one
-- The account a source reports, created the first time it appears (ADR 0013); the source's name for
-- it is kept as last reported.
INSERT INTO accounts (institution, kind, number, label)
VALUES (@institution, @kind, @number, sqlc.narg(label))
ON CONFLICT (institution, kind, number) WHERE institution <> 'owner'
DO UPDATE SET label = coalesce(EXCLUDED.label, accounts.label)
RETURNING id;

-- name: InsertBalance :exec
-- A statement's balance; a snapshot's has none (InsertSnapshot).
INSERT INTO balances (account_id, unit_id, as_of, amount, statement_id)
VALUES (@account_id, @unit_id, @as_of, @amount, @statement_id::bigint);

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
-- Each account's newest balance in each unit, with its unit's newest price, whatever its source, and its
-- worth in NT$ at that price (ADR 0020). A unit without a price, or priced in a unit without one in NT$,
-- has no worth.
SELECT DISTINCT ON (b.account_id, b.unit_id) b.account_id, b.unit_id, u.symbol AS unit, u.code, b.as_of, b.amount,
       b.statement_id,
       round(b.amount * p.twd_amount / p.unit_amount, 2)::numeric AS worth,
       p.as_of AS priced_at, p.source AS price_source, p.quote AS price_quote,
       trim_scale(p.quote_amount / p.amount)::numeric AS price_quoted,
       trim_scale(p.twd_amount / p.unit_amount)::numeric AS price_twd
FROM balances b
JOIN units u ON u.id = b.unit_id
LEFT JOIN newest_prices p ON p.unit_id = b.unit_id
ORDER BY b.account_id, b.unit_id, b.as_of DESC, b.id DESC;

-- name: LatestValuations :many
-- Each account's newest valuation (ADR 0018), with its worth in NT$ at its unit's newest price; none for
-- a unit without one.
SELECT DISTINCT ON (v.account_id) v.account_id, v.as_of, u.symbol AS unit, v.amount,
       round(v.amount * p.twd_amount / p.unit_amount, 2)::numeric AS worth
FROM valuations v
JOIN units u ON u.id = v.unit_id
LEFT JOIN newest_prices p ON p.unit_id = v.unit_id
ORDER BY v.account_id, v.as_of DESC;

-- name: AccountBalances :many
SELECT b.account_id, u.symbol AS unit, b.as_of, b.amount, b.statement_id
FROM balances b
JOIN units u ON u.id = b.unit_id
WHERE b.account_id = @account_id
ORDER BY b.as_of DESC, u.symbol, b.id DESC;

-- name: AccountGaps :many
-- Where an account's balances do not join up (ADR 0013): two consecutive balances in one unit whose
-- difference the lines dated between them do not account for, so money moved that no report shows.
-- Deposit and securities accounts (a security's shares, ADR 0017): a card bill's lines are dated by
-- purchase, which can fall before the previous bill closed, so dates cannot place them between two bills.
-- Itemized: some statement has listed a line of the account in the unit. A deposit account's unit none
-- has may be one whose statements print only its balance (CTBC's passbook savings, 活儲有摺): a move
-- there is one no source shows, not a gap. A securities statement lists every trade, so a security
-- never traded is itemized all the same: a stock dividend is a gap. A points account's bills list every
-- movement but points that lapse, so a gap there is points lost (ADR 0019).
WITH ordered AS (
    SELECT b.account_id, b.unit_id, b.as_of, b.amount,
           (a.kind IN ('securities', 'points') OR EXISTS (
               SELECT 1 FROM transactions t WHERE t.account_id = b.account_id AND t.unit_id = b.unit_id))::boolean AS itemized,
           lag(b.as_of) OVER w AS prev_as_of,
           lag(b.amount) OVER w AS prev_amount
    FROM balances b
    JOIN accounts a ON a.id = b.account_id
    WHERE a.kind IN ('deposit', 'securities', 'points') AND b.statement_id IS NOT NULL
      AND (sqlc.narg(account_id)::bigint IS NULL OR b.account_id = sqlc.narg(account_id)::bigint)
    WINDOW w AS (PARTITION BY b.account_id, b.unit_id ORDER BY b.as_of, b.id)
)
SELECT o.account_id, u.symbol AS unit, o.prev_as_of::date AS from_date, o.as_of AS to_date,
       (o.amount - o.prev_amount - coalesce(seen.amount, 0))::numeric AS unseen, o.itemized
FROM ordered o
JOIN units u ON u.id = o.unit_id
LEFT JOIN LATERAL (SELECT sum(t.amount) AS amount
                   FROM transactions t
                   WHERE t.account_id = o.account_id AND t.unit_id = o.unit_id
                     AND t.transaction_date > o.prev_as_of AND t.transaction_date <= o.as_of) seen ON true
WHERE o.prev_as_of IS NOT NULL
  AND o.amount - o.prev_amount <> coalesce(seen.amount, 0)
ORDER BY o.account_id, u.symbol, o.as_of;

-- name: AccountLines :many
-- Newest first. Amounts as the source reports them: money in is positive on a deposit account, a
-- purchase is positive on a card bill.
SELECT t.id, t.statement_id, t.transaction_date, t.description, t.memo, t.counterparty, t.kind, u.symbol AS unit,
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
JOIN units u ON u.id = t.unit_id
LEFT JOIN trades tr ON tr.transaction_id = t.id
LEFT JOIN transactions c ON c.id = t.counterpart_id
LEFT JOIN accounts ca ON ca.id = c.account_id
WHERE t.account_id = @account_id
ORDER BY t.transaction_date DESC, t.statement_id DESC, t.line_no DESC
LIMIT @row_limit OFFSET @row_offset;
