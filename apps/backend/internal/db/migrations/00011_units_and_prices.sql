-- +goose Up

-- What an amount is counted in (ADR 0020), found or created when a source first reports it, its kind by
-- what reports it, and never deleted. A unit is its kind and symbol: the same text in two kinds is two
-- units, so a security and a coin are never taken for each other.
CREATE TABLE units (
    id     BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind   TEXT NOT NULL CHECK (kind IN ('money', 'security', 'points')),
    -- Money: ISO 4217 or the ticker, upper case. A security: its name as statements print it (ADR 0017).
    -- Points: the program's name as its source prints it, or as the owner types it (ADR 0019).
    symbol TEXT NOT NULL,
    -- A security's, from the exchanges' lists.
    code   TEXT,
    market TEXT CHECK (market IN ('twse', 'tpex')),
    UNIQUE (kind, symbol)
);

-- NT$, which every worth is in.
INSERT INTO units (kind, symbol) VALUES ('money', 'TWD');

-- Every unit in use, its kind by the account it is in: card, deposit, and exchange accounts hold money,
-- securities accounts securities.
INSERT INTO units (kind, symbol)
SELECT DISTINCT CASE a.kind WHEN 'securities' THEN 'security' ELSE 'money' END, used.unit
FROM (SELECT account_id, unit FROM balances
      UNION SELECT account_id, unit FROM transactions
      UNION SELECT account_id, unit FROM valuations) used
JOIN accounts a ON a.id = used.account_id
ON CONFLICT (kind, symbol) DO NOTHING;

UPDATE units u SET code = q.code, market = q.market
FROM (SELECT DISTINCT ON (name) name, code, market FROM quotes ORDER BY name, day DESC) q
WHERE u.kind = 'security' AND u.symbol = q.name;

-- What a unit was worth, from any source: `amount` of the unit was worth `quote_amount` of the quote unit,
-- both as the source gives them (ADR 0004), at `as_of`: when the price held, not when it was stored.
CREATE TABLE prices (
    unit_id       BIGINT      NOT NULL REFERENCES units (id),
    as_of         TIMESTAMPTZ NOT NULL,
    amount        NUMERIC     NOT NULL CHECK (amount > 0),
    -- Always money.
    quote_unit_id BIGINT      NOT NULL REFERENCES units (id),
    quote_amount  NUMERIC     NOT NULL CHECK (quote_amount >= 0),
    -- Where it came from, for showing it and for clearing the owner's; never which price counts.
    source        TEXT        NOT NULL CHECK (source IN ('fawaz', 'twse', 'tpex', 'max', 'binance', 'bitget', 'owner')),
    CHECK (quote_unit_id <> unit_id)
);

-- One per unit, source, and day in Taipei: a later one the same day replaces it.
CREATE UNIQUE INDEX prices_daily_key ON prices (unit_id, source, ((as_of AT TIME ZONE 'Asia/Taipei')::date));
CREATE INDEX prices_newest_idx ON prices (unit_id, as_of DESC);

-- The feed's rates (ADR 0015) as of the start of their day, and the closes (ADR 0017) as of 13:30 on
-- theirs, for the units in use; the rest are dropped.
INSERT INTO prices (unit_id, as_of, amount, quote_unit_id, quote_amount, source)
SELECT u.id, r.day::timestamp AT TIME ZONE 'Asia/Taipei', r.per_twd, twd.id, 1, 'fawaz'
FROM exchange_rates r
JOIN units u ON u.kind = 'money' AND u.symbol = r.unit
JOIN units twd ON twd.kind = 'money' AND twd.symbol = 'TWD'
WHERE u.id <> twd.id;

INSERT INTO prices (unit_id, as_of, amount, quote_unit_id, quote_amount, source)
SELECT u.id, (q.day + time '13:30') AT TIME ZONE 'Asia/Taipei', 1, twd.id, q.close, q.market
FROM quotes q
JOIN units u ON u.kind = 'security' AND u.symbol = q.name
JOIN units twd ON twd.kind = 'money' AND twd.symbol = 'TWD';

DROP TABLE exchange_rates;
DROP TABLE quotes;

-- Each unit's newest price, whatever its source, and what it comes to in NT$: `unit_amount` of the unit
-- is worth `twd_amount`. A price in another unit goes through that unit's own newest price when that one
-- is in NT$: one step, never a chain; otherwise the NT$ columns are null. NT$ is worth itself, with no
-- price of its own.
CREATE VIEW newest_prices AS
WITH newest AS (
    SELECT DISTINCT ON (p.unit_id) p.unit_id, p.as_of, p.source, p.amount, p.quote_unit_id, p.quote_amount
    FROM prices p
    ORDER BY p.unit_id, p.as_of DESC, p.source
)
SELECT n.unit_id, n.as_of, n.source, q.symbol AS quote, n.amount, n.quote_amount,
       (CASE WHEN q.symbol = 'TWD' THEN n.amount WHEN qq.symbol = 'TWD' THEN n.amount * qn.amount END)::numeric AS unit_amount,
       (CASE WHEN q.symbol = 'TWD' THEN n.quote_amount WHEN qq.symbol = 'TWD' THEN n.quote_amount * qn.quote_amount END)::numeric AS twd_amount
FROM newest n
JOIN units q ON q.id = n.quote_unit_id
LEFT JOIN newest qn ON qn.unit_id = n.quote_unit_id
LEFT JOIN units qq ON qq.id = qn.quote_unit_id
UNION ALL
SELECT id, NULL, NULL, symbol, 1, 1, 1, 1
FROM units
WHERE kind = 'money' AND symbol = 'TWD';

-- Rows that hold an amount refer to its unit. Dropping a column drops the indexes on it.
ALTER TABLE balances ADD COLUMN unit_id BIGINT REFERENCES units (id);
UPDATE balances b SET unit_id = u.id
FROM accounts a, units u
WHERE a.id = b.account_id AND u.symbol = b.unit
  AND u.kind = CASE a.kind WHEN 'securities' THEN 'security' ELSE 'money' END;
ALTER TABLE balances ALTER COLUMN unit_id SET NOT NULL;
ALTER TABLE balances DROP COLUMN unit;
CREATE INDEX balances_account_idx ON balances (account_id, unit_id, as_of);
CREATE UNIQUE INDEX balances_statement_key ON balances (statement_id, account_id, unit_id, as_of)
    WHERE statement_id IS NOT NULL;
CREATE UNIQUE INDEX balances_snapshot_key ON balances (account_id, unit_id, as_of)
    WHERE statement_id IS NULL;

ALTER TABLE transactions ADD COLUMN unit_id BIGINT REFERENCES units (id);
UPDATE transactions t SET unit_id = u.id
FROM accounts a, units u
WHERE a.id = t.account_id AND u.symbol = t.unit
  AND u.kind = CASE a.kind WHEN 'securities' THEN 'security' ELSE 'money' END;
ALTER TABLE transactions ALTER COLUMN unit_id SET NOT NULL;
ALTER TABLE transactions DROP COLUMN unit;

ALTER TABLE valuations ADD COLUMN unit_id BIGINT REFERENCES units (id);
UPDATE valuations v SET unit_id = u.id
FROM units u
WHERE u.kind = 'money' AND u.symbol = v.unit;
ALTER TABLE valuations ALTER COLUMN unit_id SET NOT NULL;
ALTER TABLE valuations DROP COLUMN unit;

-- +goose Down
ALTER TABLE valuations ADD COLUMN unit TEXT;
UPDATE valuations v SET unit = u.symbol FROM units u WHERE u.id = v.unit_id;
ALTER TABLE valuations ALTER COLUMN unit SET NOT NULL;
ALTER TABLE valuations DROP COLUMN unit_id;

ALTER TABLE transactions ADD COLUMN unit TEXT;
UPDATE transactions t SET unit = u.symbol FROM units u WHERE u.id = t.unit_id;
ALTER TABLE transactions ALTER COLUMN unit SET NOT NULL;
ALTER TABLE transactions DROP COLUMN unit_id;

ALTER TABLE balances ADD COLUMN unit TEXT;
UPDATE balances b SET unit = u.symbol FROM units u WHERE u.id = b.unit_id;
ALTER TABLE balances ALTER COLUMN unit SET NOT NULL;
ALTER TABLE balances DROP COLUMN unit_id;
CREATE INDEX balances_account_idx ON balances (account_id, unit, as_of);
CREATE UNIQUE INDEX balances_statement_key ON balances (statement_id, account_id, unit, as_of)
    WHERE statement_id IS NOT NULL;
CREATE UNIQUE INDEX balances_snapshot_key ON balances (account_id, unit, as_of)
    WHERE statement_id IS NULL;

DROP VIEW newest_prices;

CREATE TABLE exchange_rates (
    day     DATE    NOT NULL,
    unit    TEXT    NOT NULL,
    per_twd NUMERIC NOT NULL CHECK (per_twd > 0),
    PRIMARY KEY (day, unit)
);
INSERT INTO exchange_rates (day, unit, per_twd)
SELECT (p.as_of AT TIME ZONE 'Asia/Taipei')::date, u.symbol, p.amount / p.quote_amount
FROM prices p
JOIN units u ON u.id = p.unit_id
WHERE p.source = 'fawaz';

CREATE TABLE quotes (
    day    DATE    NOT NULL,
    name   TEXT    NOT NULL,
    code   TEXT    NOT NULL,
    market TEXT    NOT NULL CHECK (market IN ('twse', 'tpex')),
    close  NUMERIC NOT NULL CHECK (close > 0),
    PRIMARY KEY (day, name)
);
INSERT INTO quotes (day, name, code, market, close)
SELECT (p.as_of AT TIME ZONE 'Asia/Taipei')::date, u.symbol, u.code, u.market, p.quote_amount / p.amount
FROM prices p
JOIN units u ON u.id = p.unit_id
WHERE p.source IN ('twse', 'tpex') AND u.code IS NOT NULL;

DROP TABLE prices;
DROP TABLE units;
