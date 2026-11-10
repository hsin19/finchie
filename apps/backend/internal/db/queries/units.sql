-- Units and prices (ADR 0020).

-- name: EnsureUnits :many
-- The units of a kind with these symbols, each created the first time a source reports it. The update
-- changes nothing: it makes a unit that exists, or one another transaction is creating, come back too.
INSERT INTO units (kind, symbol)
SELECT DISTINCT @kind::text, unnest(@symbols::text[])
ON CONFLICT (kind, symbol) DO UPDATE SET symbol = EXCLUDED.symbol
RETURNING id, symbol;

-- name: ListUnits :many
SELECT id, kind, symbol, code
FROM units
WHERE sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind)::text
ORDER BY kind, symbol;

-- name: GetUnit :one
SELECT id, kind, symbol FROM units WHERE id = @id;

-- name: Securities :many
-- The securities some account holds or held, by name: the closes worth keeping (ADR 0017).
SELECT symbol FROM units WHERE kind = 'security' ORDER BY symbol;

-- name: SetSecurityCodes :exec
UPDATE units u SET code = r.code, market = r.market
FROM (SELECT unnest(@symbols::text[]) AS symbol, unnest(@codes::text[]) AS code, unnest(@markets::text[]) AS market) r
WHERE u.kind = 'security' AND u.symbol = r.symbol;

-- name: UpsertPrices :exec
-- What a source gives for units of a kind that exist, each in a money unit: one price per unit, source,
-- and day in Taipei, a later one the same day replacing it. A unit priced in itself is left out, as is
-- one no account has held.
INSERT INTO prices (unit_id, as_of, amount, quote_unit_id, quote_amount, source)
SELECT u.id, r.as_of, r.amount, q.id, r.quote_amount, @source::text
FROM (SELECT unnest(@symbols::text[]) AS symbol, unnest(@as_of::timestamptz[]) AS as_of,
             unnest(@amounts::numeric[]) AS amount, unnest(@quotes::text[]) AS quote,
             unnest(@quote_amounts::numeric[]) AS quote_amount) r
JOIN units u ON u.kind = @kind::text AND u.symbol = r.symbol
JOIN units q ON q.kind = 'money' AND q.symbol = r.quote
WHERE u.id <> q.id
ON CONFLICT (unit_id, source, ((as_of AT TIME ZONE 'Asia/Taipei')::date))
DO UPDATE SET as_of = EXCLUDED.as_of, amount = EXCLUDED.amount, quote_unit_id = EXCLUDED.quote_unit_id,
              quote_amount = EXCLUDED.quote_amount, withdrawn_at = NULL;

-- name: ClearOwnerPrices :exec
-- Withdraws the owner's prices of a unit: kept, so what it was worth on a day stays known, but none counts
-- as its newest any more (newest_prices).
UPDATE prices SET withdrawn_at = now()
WHERE unit_id = @unit_id AND source = 'owner' AND withdrawn_at IS NULL;
