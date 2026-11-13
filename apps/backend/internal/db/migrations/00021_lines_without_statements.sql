-- +goose Up

-- A line no statement reports: the owner's. Its statement_id is NULL. Every query that reads lines as
-- statements report them (search, flows, a bill's lines, transfer candidates) joins the line's statement,
-- which leaves such a line out.
--
-- The first kind is a fill: the money that moved between two consecutive statement balances of an
-- account in a unit with no line to show it, once the reports that would show it are lost, as one line
-- for the whole period, the way a passbook prints one for what went unprinted.
ALTER TABLE transactions
    ALTER COLUMN statement_id DROP NOT NULL,
    DROP CONSTRAINT transactions_kind_check,
    ADD CONSTRAINT transactions_kind_check CHECK (kind IN (
        'purchase', 'foreign_transaction_fee', 'annual_fee', 'refund', 'rebate', 'payment', -- card lines
        'interest', 'fee', 'other',                                                        -- deposit lines
        'buy', 'sell',                                                                     -- trades
        'earn', 'adjust', 'redeem',                                                        -- points
        'fill')),                                                                          -- the owner's
    -- For now only a fill has no statement.
    ADD CONSTRAINT transactions_statement_check CHECK ((statement_id IS NULL) = (kind = 'fill'));

-- Each statement balance of an account in a unit with the one before it, and what moved between them.
CREATE VIEW balance_steps AS
SELECT b.account_id, b.unit_id, b.statement_id,
       lag(b.as_of) OVER w AS from_date, b.as_of AS to_date,
       b.amount - lag(b.amount) OVER w AS moved
FROM balances b
WHERE b.statement_id IS NOT NULL
WINDOW w AS (PARTITION BY b.account_id, b.unit_id ORDER BY b.as_of, b.id);

-- The lines that count: every line, but a fill only while the other lines between the two balances it
-- falls between still leave exactly its amount unseen. A statement found later claims its own days: the
-- fill counts for nothing, nothing is counted twice, and the days before and after it show as gaps
-- again rather than staying filled, so the owner sees each amount they accept and a balance misread
-- later never hides behind a fill. A fill set aside is kept, not deleted, so a statement that fails to
-- parse for a while does not take the owner's fill and memo with it. Queries that add up an account's
-- lines read this, not transactions. (t.* is expanded when the view is made: a column added to
-- transactions later needs the view made again.)
CREATE VIEW counted_lines AS
SELECT t.*
FROM transactions t
WHERE t.kind <> 'fill' OR EXISTS (
    SELECT 1
    FROM balance_steps s
    WHERE s.account_id = t.account_id AND s.unit_id = t.unit_id
      AND s.from_date < t.transaction_date AND t.transaction_date <= s.to_date
      AND s.moved - coalesce((SELECT sum(o.amount)
                              FROM transactions o
                              WHERE o.account_id = s.account_id AND o.unit_id = s.unit_id AND o.kind <> 'fill'
                                AND o.transaction_date > s.from_date AND o.transaction_date <= s.to_date), 0) = t.amount);

-- +goose Down
DROP VIEW counted_lines;
DROP VIEW balance_steps;
DELETE FROM transactions WHERE kind = 'fill';
ALTER TABLE transactions
    DROP CONSTRAINT transactions_statement_check,
    DROP CONSTRAINT transactions_kind_check,
    ADD CONSTRAINT transactions_kind_check CHECK (kind IN (
        'purchase', 'foreign_transaction_fee', 'annual_fee', 'refund', 'rebate', 'payment',
        'interest', 'fee', 'other',
        'buy', 'sell',
        'earn', 'adjust', 'redeem')),
    ALTER COLUMN statement_id SET NOT NULL;
