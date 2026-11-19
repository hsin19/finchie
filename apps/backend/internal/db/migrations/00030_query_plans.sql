-- +goose Up

-- line_matches was IMMUTABLE, but concat_ws is only STABLE, so Postgres never inlined it: every search,
-- total, and facet called it once for each line in scope, and a date filter could not use the date index.
ALTER FUNCTION line_matches(DATE, DATE, TEXT, TEXT, TEXT, BIGINT, TEXT, TEXT, TEXT, TEXT, TEXT, DATE, DATE, DATE, DATE, BOOLEAN, TEXT, BOOLEAN, TEXT) STABLE;

-- An account's lines in one unit over a span of days, as gaps, balances by day, and fills look them up.
-- It serves what the index on account_id alone did, as does the unique key on (account_id, external_id).
CREATE INDEX transactions_account_unit_date_idx ON transactions (account_id, unit_id, transaction_date);
DROP INDEX transactions_account_id_idx;

-- price_at as in 00029, but with the moment as a bound of the index on (unit_id, as_of): `at IS NULL OR
-- as_of <= at` bounds nothing, so each call read every price the unit ever had, one more each day.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION price_at(of_unit BIGINT, at TIMESTAMPTZ)
    RETURNS TABLE (as_of TIMESTAMPTZ, source TEXT, quote TEXT, amount NUMERIC, quote_amount NUMERIC,
                   unit_amount NUMERIC, twd_amount NUMERIC)
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT n.as_of, n.source, q.symbol, n.amount, n.quote_amount,
           CASE WHEN q.symbol = 'TWD' THEN n.amount WHEN qq.symbol = 'TWD' THEN n.amount * qq.amount END,
           CASE WHEN q.symbol = 'TWD' THEN n.quote_amount WHEN qq.symbol = 'TWD' THEN n.quote_amount * qq.quote_amount END
    FROM (
        SELECT p.as_of, p.source, p.amount, p.quote_unit_id, p.quote_amount
        FROM prices p
        WHERE p.unit_id = of_unit
          AND p.as_of <= coalesce(at, 'infinity'::timestamptz)
          AND (p.withdrawn_at IS NULL OR (at IS NOT NULL AND p.withdrawn_at > at))
        ORDER BY p.as_of DESC, p.source
        LIMIT 1
    ) n
    JOIN units q ON q.id = n.quote_unit_id
    LEFT JOIN LATERAL (
        SELECT p.amount, p.quote_amount, u.symbol
        FROM prices p
        JOIN units u ON u.id = p.quote_unit_id
        WHERE p.unit_id = n.quote_unit_id
          AND p.as_of <= coalesce(at, 'infinity'::timestamptz)
          AND (p.withdrawn_at IS NULL OR (at IS NOT NULL AND p.withdrawn_at > at))
        ORDER BY p.as_of DESC, p.source
        LIMIT 1
    ) qq ON true
    UNION ALL
    SELECT NULL, NULL, u.symbol, 1, 1, 1, 1
    FROM units u
    WHERE u.id = of_unit AND u.kind = 'money' AND u.symbol = 'TWD'
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION price_at(of_unit BIGINT, at TIMESTAMPTZ)
    RETURNS TABLE (as_of TIMESTAMPTZ, source TEXT, quote TEXT, amount NUMERIC, quote_amount NUMERIC,
                   unit_amount NUMERIC, twd_amount NUMERIC)
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT n.as_of, n.source, q.symbol, n.amount, n.quote_amount,
           CASE WHEN q.symbol = 'TWD' THEN n.amount WHEN qq.symbol = 'TWD' THEN n.amount * qq.amount END,
           CASE WHEN q.symbol = 'TWD' THEN n.quote_amount WHEN qq.symbol = 'TWD' THEN n.quote_amount * qq.quote_amount END
    FROM (
        SELECT p.as_of, p.source, p.amount, p.quote_unit_id, p.quote_amount
        FROM prices p
        WHERE p.unit_id = of_unit
          AND (at IS NULL OR p.as_of <= at)
          AND (p.withdrawn_at IS NULL OR (at IS NOT NULL AND p.withdrawn_at > at))
        ORDER BY p.as_of DESC, p.source
        LIMIT 1
    ) n
    JOIN units q ON q.id = n.quote_unit_id
    LEFT JOIN LATERAL (
        SELECT p.amount, p.quote_amount, u.symbol
        FROM prices p
        JOIN units u ON u.id = p.quote_unit_id
        WHERE p.unit_id = n.quote_unit_id
          AND (at IS NULL OR p.as_of <= at)
          AND (p.withdrawn_at IS NULL OR (at IS NOT NULL AND p.withdrawn_at > at))
        ORDER BY p.as_of DESC, p.source
        LIMIT 1
    ) qq ON true
    UNION ALL
    SELECT NULL, NULL, u.symbol, 1, 1, 1, 1
    FROM units u
    WHERE u.id = of_unit AND u.kind = 'money' AND u.symbol = 'TWD'
$$;
-- +goose StatementEnd
CREATE INDEX transactions_account_id_idx ON transactions (account_id);
DROP INDEX transactions_account_unit_date_idx;
ALTER FUNCTION line_matches(DATE, DATE, TEXT, TEXT, TEXT, BIGINT, TEXT, TEXT, TEXT, TEXT, TEXT, DATE, DATE, DATE, DATE, BOOLEAN, TEXT, BOOLEAN, TEXT) IMMUTABLE;
