-- +goose Up

-- Exchange accounts (ADR 0018): an exchange's API reports what the account holds, a snapshot that no
-- statement reported, and what the exchange says the whole account is worth.
ALTER TABLE accounts
    DROP CONSTRAINT accounts_kind_check,
    ADD CONSTRAINT accounts_kind_check CHECK (kind IN ('credit_card', 'deposit', 'securities', 'exchange'));

-- A balance without a statement is a snapshot, one per account, unit, and day: a later fetch on the same
-- day replaces that day's.
ALTER TABLE balances
    ALTER COLUMN statement_id DROP NOT NULL,
    DROP CONSTRAINT balances_statement_id_account_id_unit_as_of_key;
CREATE UNIQUE INDEX balances_statement_key ON balances (statement_id, account_id, unit, as_of)
    WHERE statement_id IS NOT NULL;
CREATE UNIQUE INDEX balances_snapshot_key ON balances (account_id, unit, as_of)
    WHERE statement_id IS NULL;

-- What a source says a whole account is worth on a day, in the unit it says it in. Any source may report
-- one; an account's worth falls back on it when some unit it holds has none (ADR 0018).
CREATE TABLE valuations (
    account_id BIGINT  NOT NULL REFERENCES accounts (id),
    as_of      DATE    NOT NULL,
    unit       TEXT    NOT NULL,
    amount     NUMERIC NOT NULL,
    PRIMARY KEY (account_id, as_of)
);

-- +goose Down
DROP TABLE valuations;

DELETE FROM balances WHERE statement_id IS NULL;
DROP INDEX balances_snapshot_key;
DROP INDEX balances_statement_key;
ALTER TABLE balances
    ALTER COLUMN statement_id SET NOT NULL,
    ADD CONSTRAINT balances_statement_id_account_id_unit_as_of_key UNIQUE (statement_id, account_id, unit, as_of);

DELETE FROM accounts WHERE kind = 'exchange';
ALTER TABLE accounts
    DROP CONSTRAINT accounts_kind_check,
    ADD CONSTRAINT accounts_kind_check CHECK (kind IN ('credit_card', 'deposit', 'securities'));
