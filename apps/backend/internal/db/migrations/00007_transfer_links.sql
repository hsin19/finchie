-- +goose Up

-- The owner's full number for an account, shown in place of the masked one and used to fit
-- counterparties (ADR 0016). It must fit the masked number; the account is still found by that.
ALTER TABLE accounts ADD COLUMN full_number TEXT;

-- Money that moved between two of the owner's lines, both pointing at each other. Derived, like
-- parent_id: worked out again from all the lines whenever a statement is stored.
ALTER TABLE transactions ADD COLUMN counterpart_id BIGINT REFERENCES transactions (id) ON DELETE SET NULL;
CREATE INDEX transactions_counterpart_id_idx ON transactions (counterpart_id) WHERE counterpart_id IS NOT NULL;

-- When a card bill was paid, and the line that showed it (a deposit outflow, else a later bill's payment
-- line); null for a bill that owes nothing, which is paid on its closing date. Derived as above.
ALTER TABLE statements
    ADD COLUMN paid_on DATE,
    ADD COLUMN paid_by BIGINT REFERENCES transactions (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE statements DROP COLUMN paid_by, DROP COLUMN paid_on;
ALTER TABLE transactions DROP COLUMN counterpart_id;
ALTER TABLE accounts DROP COLUMN full_number;
