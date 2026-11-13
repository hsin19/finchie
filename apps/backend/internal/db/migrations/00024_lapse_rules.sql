-- +goose Up

-- How a points program's points lapse by its terms, as its parser reports it with each bill (ADR 0019): at
-- the end of the month lapse_months after a bill's closing month, or once a year, what the bills closing
-- up to month lapse_purge_month add, at the end of the month after it, lapse_purge_years later. None where
-- the terms are not known. Each bill reports it again; the one stored last stands.
ALTER TABLE units
    ADD COLUMN lapse_months smallint CHECK (lapse_months > 0),
    ADD COLUMN lapse_purge_month smallint CHECK (lapse_purge_month BETWEEN 1 AND 12),
    ADD COLUMN lapse_purge_years smallint CHECK (lapse_purge_years > 0),
    ADD CONSTRAINT units_lapse_check CHECK (
        (lapse_months IS NULL OR lapse_purge_month IS NULL) AND (lapse_purge_month IS NULL) = (lapse_purge_years IS NULL));

-- +goose Down
ALTER TABLE units
    DROP CONSTRAINT units_lapse_check,
    DROP COLUMN lapse_months,
    DROP COLUMN lapse_purge_month,
    DROP COLUMN lapse_purge_years;
