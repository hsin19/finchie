-- +goose Up

-- A card bill reports what is owed on one day, not a balance that carries on (ADR 0013): its bank's
-- credit_card account only holds the card lines.
DELETE FROM balances b
USING accounts a
WHERE a.id = b.account_id AND a.kind = 'credit_card';

-- +goose Down
INSERT INTO balances (account_id, unit, as_of, amount, statement_id)
SELECT a.id, s.currency, s.closing_date, -s.total_due, s.id
FROM statements s
JOIN accounts a ON a.institution = s.institution AND a.kind = 'credit_card' AND a.number = ''
WHERE s.kind = 'credit_card';
