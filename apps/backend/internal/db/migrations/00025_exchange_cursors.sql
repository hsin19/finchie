-- +goose Up

-- How far each exchange's deposits and withdrawals have been read (ADR 0018): a fetch reads the span from
-- here a month on, up to now, and moves it on past what has settled. None yet means two years back.
CREATE TABLE exchange_cursors (
    institution text PRIMARY KEY,
    read_from   timestamptz NOT NULL
);

-- +goose Down
DROP TABLE exchange_cursors;
