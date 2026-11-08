-- +goose Up

-- Two deposit exports for the same bank can close on the same day while starting on different days
-- (for example, an e-passbook exported for two date ranges after the last transaction).
ALTER TABLE statements
    DROP CONSTRAINT statements_institution_kind_closing_date_key,
    ADD CONSTRAINT statements_institution_kind_period_key
        UNIQUE NULLS NOT DISTINCT (institution, kind, period_start, closing_date);

-- +goose Down
ALTER TABLE statements
    DROP CONSTRAINT statements_institution_kind_period_key,
    ADD CONSTRAINT statements_institution_kind_closing_date_key UNIQUE (institution, kind, closing_date);
