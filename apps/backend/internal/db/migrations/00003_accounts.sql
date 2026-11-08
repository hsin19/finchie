-- +goose Up

-- Anything that holds a value for the owner, or a debt (ADR 0013). Discovered from what its sources
-- report and never set up by hand; it outlives every report of it, so nothing deletes one.
CREATE TABLE accounts (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    institution TEXT        NOT NULL,
    kind        TEXT        NOT NULL CHECK (kind IN ('credit_card', 'deposit')),
    -- As the source prints it, normalized by its parser; empty for a card bill, one per institution.
    number      TEXT        NOT NULL DEFAULT '',
    -- The owner's name for it, if they ever give one; nothing needs it.
    name        TEXT,
    -- How its values lapse. Rolling and fixed rules come with the first points source.
    expiry      TEXT        NOT NULL DEFAULT 'none' CHECK (expiry IN ('none')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (institution, kind, number)
);

-- What an account held in one unit at the end of a day, and the statement that said so. Derived from
-- the statement like its lines, so deleted with it. The same day can have a balance from each of two
-- statements (a closing and the next one's opening): continuity compares them.
CREATE TABLE balances (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id   BIGINT  NOT NULL REFERENCES accounts (id),
    -- ISO 4217 for money, else a points or miles program's own code.
    unit         TEXT    NOT NULL,
    as_of        DATE    NOT NULL,
    -- Seen from the owner: money held is positive, money owed negative.
    amount       NUMERIC NOT NULL,
    statement_id BIGINT  NOT NULL REFERENCES statements (id) ON DELETE CASCADE,
    UNIQUE (statement_id, account_id, unit, as_of)
);

CREATE INDEX balances_account_idx ON balances (account_id, unit, as_of);

INSERT INTO accounts (institution, kind)
SELECT DISTINCT institution, 'credit_card' FROM statements;

ALTER TABLE transactions
    ADD COLUMN account_id BIGINT REFERENCES accounts (id),
    ADD COLUMN unit       TEXT,
    -- 'expired' once a value the line brought lapses; the line itself stays as reported.
    ADD COLUMN status     TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'expired'));

UPDATE transactions t
SET account_id = a.id, unit = s.currency
FROM statements s
JOIN accounts a ON a.institution = s.institution AND a.kind = 'credit_card' AND a.number = ''
WHERE s.id = t.statement_id;

ALTER TABLE transactions
    ALTER COLUMN account_id SET NOT NULL,
    ALTER COLUMN unit SET NOT NULL;

CREATE INDEX transactions_account_id_idx ON transactions (account_id);

INSERT INTO balances (account_id, unit, as_of, amount, statement_id)
SELECT a.id, s.currency, s.closing_date, -s.total_due, s.id
FROM statements s
JOIN accounts a ON a.institution = s.institution AND a.kind = 'credit_card' AND a.number = '';

-- +goose Down
ALTER TABLE transactions
    DROP COLUMN status,
    DROP COLUMN unit,
    DROP COLUMN account_id;
DROP TABLE balances;
DROP TABLE accounts;
