-- +goose Up

-- price_at, as before, found by the index on (unit_id, as_of): the unit's newest price at the moment, then
-- its quote unit's. It was a CTE of every unit's prices up to the moment, which Postgres builds in full on
-- each call since two parts read it; net worth and the investments call it for every day, account, and unit,
-- so a year of days took seconds, and longer as prices piled up.
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

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION price_at(of_unit BIGINT, at TIMESTAMPTZ)
    RETURNS TABLE (as_of TIMESTAMPTZ, source TEXT, quote TEXT, amount NUMERIC, quote_amount NUMERIC,
                   unit_amount NUMERIC, twd_amount NUMERIC)
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    WITH valid AS (
        SELECT p.*
        FROM prices p
        WHERE (at IS NULL OR p.as_of <= at)
          AND (p.withdrawn_at IS NULL OR (at IS NOT NULL AND p.withdrawn_at > at))
    ), newest AS (
        SELECT DISTINCT ON (v.unit_id) v.unit_id, v.as_of, v.source, v.amount, v.quote_unit_id, v.quote_amount
        FROM valid v
        WHERE v.unit_id = of_unit
        ORDER BY v.unit_id, v.as_of DESC, v.source
    ), quoted AS (
        SELECT DISTINCT ON (v.unit_id) v.unit_id, v.amount, v.quote_amount, q.symbol
        FROM valid v
        JOIN units q ON q.id = v.quote_unit_id
        WHERE v.unit_id = (SELECT quote_unit_id FROM newest)
        ORDER BY v.unit_id, v.as_of DESC, v.source
    )
    SELECT n.as_of, n.source, q.symbol, n.amount, n.quote_amount,
           CASE WHEN q.symbol = 'TWD' THEN n.amount WHEN qq.symbol = 'TWD' THEN n.amount * qq.amount END,
           CASE WHEN q.symbol = 'TWD' THEN n.quote_amount WHEN qq.symbol = 'TWD' THEN n.quote_amount * qq.quote_amount END
    FROM newest n
    JOIN units q ON q.id = n.quote_unit_id
    LEFT JOIN quoted qq ON true
    UNION ALL
    SELECT NULL, NULL, u.symbol, 1, 1, 1, 1
    FROM units u
    WHERE u.id = of_unit AND u.kind = 'money' AND u.symbol = 'TWD'
$$;
-- +goose StatementEnd
