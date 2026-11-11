-- +goose Up

-- Placeholders no code ever wrote: how an account's values lapse is what its sources report in `expiries`
-- (ADR 0019), and a line has no status of its own.
ALTER TABLE accounts DROP COLUMN expiry;
ALTER TABLE transactions DROP COLUMN status;

-- +goose Down
ALTER TABLE transactions ADD COLUMN status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'expired'));
ALTER TABLE accounts ADD COLUMN expiry TEXT NOT NULL DEFAULT 'none' CHECK (expiry IN ('none'));
