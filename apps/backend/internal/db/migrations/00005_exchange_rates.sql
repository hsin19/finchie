-- +goose Up

-- Reference rates to NT$ for comparing balances in different units (ADR 0015), one per unit and day.
CREATE TABLE exchange_rates (
    day     DATE    NOT NULL,
    -- Upper case: ISO 4217 for money, else the feed's own code (USDT).
    unit    TEXT    NOT NULL,
    -- How much of the unit one NT$ buys, exactly as the feed prints it.
    per_twd NUMERIC NOT NULL CHECK (per_twd > 0),
    PRIMARY KEY (day, unit)
);

-- +goose Down
DROP TABLE exchange_rates;
