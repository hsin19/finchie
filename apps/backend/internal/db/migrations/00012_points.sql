-- +goose Up

-- Points (ADR 0019): a bank's card rewards, one account per bank, each program a unit of its own; and
-- accounts the owner keeps by hand, of institution 'owner'.
ALTER TABLE accounts
    DROP CONSTRAINT accounts_kind_check,
    ADD CONSTRAINT accounts_kind_check CHECK (kind IN ('credit_card', 'deposit', 'securities', 'exchange', 'points'));

-- A source's accounts stay one per institution, kind, and number, so that each report finds its own; the
-- owner finds theirs by id, and keeps as many as they like.
ALTER TABLE accounts DROP CONSTRAINT accounts_institution_kind_number_key;
CREATE UNIQUE INDEX accounts_source_key ON accounts (institution, kind, number) WHERE institution <> 'owner';

-- No two accounts go by one name the owner gave: the name they gave an account, else its own where they
-- opened it (its label, as a bank's account has the bank's). Names given twice before this keep the
-- first; the others get their id.
UPDATE accounts a SET name = a.name || ' (' || a.id || ')'
WHERE a.name IS NOT NULL AND EXISTS (SELECT 1 FROM accounts b WHERE b.name = a.name AND b.id < a.id);
CREATE UNIQUE INDEX accounts_name_key ON accounts ((coalesce(name, label)))
    WHERE name IS NOT NULL OR institution = 'owner';

-- A bill's points lines: a period's movements as it totals them, one per column with a figure.
ALTER TABLE transactions
    DROP CONSTRAINT transactions_kind_check,
    ADD CONSTRAINT transactions_kind_check CHECK (kind IN (
        'purchase', 'foreign_transaction_fee', 'annual_fee', 'refund', 'rebate', 'payment', -- card lines
        'interest', 'fee', 'other',                                                        -- deposit lines
        'buy', 'sell',                                                                     -- trades
        'earn', 'adjust', 'redeem'));                                                      -- points

-- What a source says will lapse: an amount of a unit, by a date, as printed. A statement's go with it;
-- the owner's have none.
CREATE TABLE expiries (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id   BIGINT  NOT NULL REFERENCES accounts (id),
    unit_id      BIGINT  NOT NULL REFERENCES units (id),
    amount       NUMERIC NOT NULL,
    lapses_by    DATE    NOT NULL,
    statement_id BIGINT  REFERENCES statements (id) ON DELETE CASCADE
);

CREATE INDEX expiries_account_idx ON expiries (account_id, unit_id);

-- The owner's word that a point of these card programs is worth NT$1, as of long before any bill, so that
-- any later price takes over (ADR 0020). The symbols are the ones the bills print.
INSERT INTO units (kind, symbol)
VALUES ('points', '小樹點(信用卡)'), ('points', 'e point'), ('points', '台新Point(信用卡)')
ON CONFLICT (kind, symbol) DO NOTHING;

INSERT INTO prices (unit_id, as_of, amount, quote_unit_id, quote_amount, source)
SELECT u.id, '2000-01-01 00:00+08', 1, twd.id, 1, 'owner'
FROM units u
JOIN units twd ON twd.kind = 'money' AND twd.symbol = 'TWD'
WHERE u.kind = 'points' AND u.symbol IN ('小樹點(信用卡)', 'e point', '台新Point(信用卡)');

-- +goose Down
DROP TABLE expiries;

DELETE FROM transactions t USING accounts a WHERE a.id = t.account_id AND a.kind = 'points';
ALTER TABLE transactions
    DROP CONSTRAINT transactions_kind_check,
    ADD CONSTRAINT transactions_kind_check CHECK (kind IN (
        'purchase', 'foreign_transaction_fee', 'annual_fee', 'refund', 'rebate', 'payment',
        'interest', 'fee', 'other',
        'buy', 'sell'));

DELETE FROM balances b USING accounts a WHERE a.id = b.account_id AND a.kind = 'points';
DELETE FROM accounts WHERE kind = 'points';
DROP INDEX accounts_name_key;
DROP INDEX accounts_source_key;
ALTER TABLE accounts ADD CONSTRAINT accounts_institution_kind_number_key UNIQUE (institution, kind, number);
DELETE FROM prices p USING units u WHERE u.id = p.unit_id AND u.kind = 'points';
DELETE FROM units WHERE kind = 'points';
ALTER TABLE accounts
    DROP CONSTRAINT accounts_kind_check,
    ADD CONSTRAINT accounts_kind_check CHECK (kind IN ('credit_card', 'deposit', 'securities', 'exchange'));
