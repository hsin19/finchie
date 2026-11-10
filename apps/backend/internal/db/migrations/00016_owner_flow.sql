-- +goose Up

-- A line's amount as the owner sees it (ADR 0013): money out negative. A card bill prints a purchase
-- positive, as owed, so its lines turn sign; every other source prints money out negative already. One
-- definition, which every query that shows or adds up flows calls.
-- +goose StatementBegin
CREATE FUNCTION owner_flow(account_kind TEXT, amount NUMERIC) RETURNS NUMERIC
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    RETURN CASE WHEN account_kind = 'credit_card' THEN -amount ELSE amount END;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION owner_flow(TEXT, NUMERIC);
