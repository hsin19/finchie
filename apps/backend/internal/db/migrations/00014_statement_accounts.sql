-- +goose Up

-- A statement is the accounts it reports over its period (ADR 0013), not its bank's only one of its kind
-- that period: two brokerage accounts' monthly statements, or two e-passbook exports of different accounts
-- on the same days, are two statements, never one the other's duplicate. `account_numbers` are the
-- numbers of the accounts it reports, sorted and joined by commas; a card bill's accounts, the bank's
-- cards and points, have none.
ALTER TABLE statements ADD COLUMN account_numbers TEXT NOT NULL DEFAULT '';

UPDATE statements s
SET account_numbers = coalesce((
    SELECT string_agg(DISTINCT a.number, ',' ORDER BY a.number)
    FROM accounts a
    WHERE a.number <> ''
      AND a.id IN (SELECT b.account_id FROM balances b WHERE b.statement_id = s.id
                   UNION
                   SELECT t.account_id FROM transactions t WHERE t.statement_id = s.id)), '');

ALTER TABLE statements
    ALTER COLUMN account_numbers DROP DEFAULT,
    DROP CONSTRAINT statements_institution_kind_period_key,
    ADD CONSTRAINT statements_identity_key
        UNIQUE NULLS NOT DISTINCT (institution, kind, account_numbers, period_start, closing_date);

-- +goose Down
-- Statements that only their accounts told apart cannot stay: the later document's goes.
DELETE FROM statements s
USING statements o
WHERE o.institution = s.institution AND o.kind = s.kind
  AND o.period_start IS NOT DISTINCT FROM s.period_start AND o.closing_date = s.closing_date
  AND o.document_id < s.document_id;

ALTER TABLE statements
    DROP CONSTRAINT statements_identity_key,
    ADD CONSTRAINT statements_institution_kind_period_key
        UNIQUE NULLS NOT DISTINCT (institution, kind, period_start, closing_date),
    DROP COLUMN account_numbers;
