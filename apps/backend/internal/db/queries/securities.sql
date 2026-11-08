-- Brokerage accounts (ADR 0017).

-- name: InsertTrades :exec
-- A trade's money, for the lines of a statement just inserted, found by their line numbers.
INSERT INTO trades (transaction_id, category, price, amount, fee, tax, net, prepaid)
SELECT t.id, r.category, r.price, r.amount, r.fee, r.tax, r.net, r.prepaid
FROM (SELECT unnest(@line_nos::integer[]) AS line_no, unnest(@categories::text[]) AS category,
             unnest(@prices::numeric[]) AS price, unnest(@amounts::numeric[]) AS amount,
             unnest(@fees::numeric[]) AS fee, unnest(@taxes::numeric[]) AS tax, unnest(@nets::numeric[]) AS net,
             unnest(@prepaids::numeric[]) AS prepaid) r
JOIN transactions t ON t.statement_id = @statement_id AND t.line_no = r.line_no;

-- name: ClearEmptyHoldings :exec
-- The zero balances ImplyEmptyHoldings derives, to work out again: a statement lists no security it
-- holds none of, so every zero balance of a securities statement is derived.
DELETE FROM balances b
USING statements s
WHERE s.id = b.statement_id AND s.kind = 'securities' AND b.account_id = @account_id AND b.amount = 0;

-- name: ImplyEmptyHoldings :exec
-- A securities statement's holdings are the whole account: every security the account has held or
-- traded that a statement does not list is at 0 on its day.
WITH statements_of AS (
    SELECT DISTINCT s.id, s.closing_date
    FROM statements s
    WHERE s.kind = 'securities'
      AND (EXISTS (SELECT 1 FROM balances b WHERE b.statement_id = s.id AND b.account_id = @account_id)
           OR EXISTS (SELECT 1 FROM transactions t WHERE t.statement_id = s.id AND t.account_id = @account_id))
), units AS (
    SELECT unit FROM balances WHERE account_id = @account_id
    UNION
    SELECT unit FROM transactions WHERE account_id = @account_id
)
INSERT INTO balances (account_id, unit, as_of, amount, statement_id)
SELECT @account_id, u.unit, so.closing_date, 0, so.id
FROM statements_of so
CROSS JOIN units u
WHERE NOT EXISTS (SELECT 1 FROM balances b WHERE b.statement_id = so.id AND b.account_id = @account_id AND b.unit = u.unit);

-- name: HeldSecurities :many
-- The securities some account holds or held, by name: the quotes worth keeping.
SELECT DISTINCT b.unit
FROM balances b
JOIN accounts a ON a.id = b.account_id
WHERE a.kind = 'securities'
ORDER BY b.unit;

-- name: UpsertQuotes :exec
INSERT INTO quotes (day, name, code, market, close)
SELECT unnest(@days::date[]), unnest(@names::text[]), unnest(@codes::text[]), unnest(@markets::text[]),
       unnest(@closes::numeric[])
ON CONFLICT (day, name) DO UPDATE SET code = EXCLUDED.code, market = EXCLUDED.market, close = EXCLUDED.close;
