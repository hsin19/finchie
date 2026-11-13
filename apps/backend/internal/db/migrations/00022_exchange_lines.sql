-- +goose Up

-- A line an exchange's API reports (ADR 0018): money the owner sent to an exchange or took out of it, a
-- fiat deposit or withdrawal, which no statement reports. It has no statement but the exchange's own id
-- for it, by which a later fetch finds it again. So a line has exactly one source: a statement, the
-- exchange's record, or the owner (a fill).
ALTER TABLE transactions
    ADD COLUMN external_id text,
    ADD CONSTRAINT transactions_external_id_key UNIQUE (account_id, external_id),
    DROP CONSTRAINT transactions_statement_check,
    ADD CONSTRAINT transactions_source_check CHECK (num_nonnulls(statement_id, external_id) + (kind = 'fill')::int = 1);

-- t.* was expanded when the view was made: made again, it has the new column.
CREATE OR REPLACE VIEW counted_lines AS
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
DELETE FROM transactions WHERE external_id IS NOT NULL;
ALTER TABLE transactions
    DROP CONSTRAINT transactions_source_check,
    ADD CONSTRAINT transactions_statement_check CHECK ((statement_id IS NULL) = (kind = 'fill')),
    DROP CONSTRAINT transactions_external_id_key,
    DROP COLUMN external_id;
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
