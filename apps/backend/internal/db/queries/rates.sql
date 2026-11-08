-- name: UpsertRates :exec
INSERT INTO exchange_rates (day, unit, per_twd)
SELECT @day::date, unnest(@units::text[]), unnest(@per_twd::numeric[])
ON CONFLICT (day, unit) DO UPDATE SET per_twd = EXCLUDED.per_twd;
