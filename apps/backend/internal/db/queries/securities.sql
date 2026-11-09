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
), held AS (
    SELECT unit_id FROM balances WHERE account_id = @account_id
    UNION
    SELECT unit_id FROM transactions WHERE account_id = @account_id
)
INSERT INTO balances (account_id, unit_id, as_of, amount, statement_id)
SELECT @account_id, h.unit_id, so.closing_date, 0, so.id
FROM statements_of so
CROSS JOIN held h
WHERE NOT EXISTS (SELECT 1 FROM balances b WHERE b.statement_id = so.id AND b.account_id = @account_id AND b.unit_id = h.unit_id);

-- name: AccountTrades :many
-- A securities account's trades in the order they happened, for working out each position's cost
-- (ADR 0017).
SELECT u.symbol AS unit, t.transaction_date, t.kind, t.amount AS shares, tr.amount, tr.fee, tr.tax
FROM transactions t
JOIN units u ON u.id = t.unit_id
JOIN trades tr ON tr.transaction_id = t.id
WHERE t.account_id = @account_id
ORDER BY t.transaction_date, t.statement_id, t.line_no;
