-- +goose Up

-- Brokerage accounts (ADR 0017): a securities statement's lines are trades in shares, its balances the
-- holdings, each security a unit of its own. A securities statement closes on its holdings date.
ALTER TABLE statements
    DROP CONSTRAINT statements_kind_check,
    ADD CONSTRAINT statements_kind_check CHECK (kind IN ('credit_card', 'deposit', 'securities'));

ALTER TABLE accounts
    DROP CONSTRAINT accounts_kind_check,
    ADD CONSTRAINT accounts_kind_check CHECK (kind IN ('credit_card', 'deposit', 'securities'));

ALTER TABLE transactions
    DROP CONSTRAINT transactions_kind_check,
    ADD CONSTRAINT transactions_kind_check CHECK (kind IN (
        'purchase', 'foreign_transaction_fee', 'annual_fee', 'refund', 'rebate', 'payment', -- card lines
        'interest', 'fee', 'other',                                                        -- deposit lines
        'buy', 'sell'));                                                                   -- trades

-- A trade's money; its line holds the shares. Derived from the statement like the line, so deleted with it.
CREATE TABLE trades (
    transaction_id BIGINT  PRIMARY KEY REFERENCES transactions (id) ON DELETE CASCADE,
    -- The broker's category as printed (集買, 集賣, …).
    category       TEXT    NOT NULL,
    price          NUMERIC NOT NULL,
    -- 價金: the shares at the price.
    amount         NUMERIC NOT NULL,
    fee            NUMERIC NOT NULL,
    tax            NUMERIC NOT NULL,
    -- 應收付: received positive, paid negative; zero for a buy paid in advance.
    net            NUMERIC NOT NULL,
    -- 預繳金, for a buy paid in advance.
    prepaid        NUMERIC
);

-- Closing prices in NT$ of the securities some account holds or held, one per name and day, from the
-- exchanges' daily lists. A unit is the security's name as statements print it, so prices go by name.
CREATE TABLE quotes (
    day    DATE    NOT NULL,
    name   TEXT    NOT NULL,
    code   TEXT    NOT NULL,
    market TEXT    NOT NULL CHECK (market IN ('twse', 'tpex')),
    close  NUMERIC NOT NULL CHECK (close > 0),
    PRIMARY KEY (day, name)
);

-- +goose Down
DROP TABLE quotes;
DROP TABLE trades;
DELETE FROM statements WHERE kind = 'securities';
DELETE FROM balances b USING accounts a WHERE a.id = b.account_id AND a.kind = 'securities';
DELETE FROM accounts WHERE kind = 'securities';

ALTER TABLE transactions
    DROP CONSTRAINT transactions_kind_check,
    ADD CONSTRAINT transactions_kind_check CHECK (kind IN (
        'purchase', 'foreign_transaction_fee', 'annual_fee', 'refund', 'rebate', 'payment',
        'interest', 'fee', 'other'));

ALTER TABLE accounts
    DROP CONSTRAINT accounts_kind_check,
    ADD CONSTRAINT accounts_kind_check CHECK (kind IN ('credit_card', 'deposit'));

ALTER TABLE statements
    DROP CONSTRAINT statements_kind_check,
    ADD CONSTRAINT statements_kind_check CHECK (kind IN ('credit_card', 'deposit'));
