-- +goose Up

-- Withdrawing the owner's price of a unit keeps it, marked withdrawn, instead of deleting it: what a unit
-- was worth on a day stays known, for working out what the owner had on any day since. A withdrawn price
-- no longer counts as the unit's newest; a price the owner gives again the same day counts once more.
ALTER TABLE prices ADD COLUMN withdrawn_at TIMESTAMPTZ;

CREATE OR REPLACE VIEW newest_prices AS
WITH newest AS (
    SELECT DISTINCT ON (p.unit_id) p.unit_id, p.as_of, p.source, p.amount, p.quote_unit_id, p.quote_amount
    FROM prices p
    WHERE p.withdrawn_at IS NULL
    ORDER BY p.unit_id, p.as_of DESC, p.source
)
SELECT n.unit_id, n.as_of, n.source, q.symbol AS quote, n.amount, n.quote_amount,
       (CASE WHEN q.symbol = 'TWD' THEN n.amount WHEN qq.symbol = 'TWD' THEN n.amount * qn.amount END)::numeric AS unit_amount,
       (CASE WHEN q.symbol = 'TWD' THEN n.quote_amount WHEN qq.symbol = 'TWD' THEN n.quote_amount * qn.quote_amount END)::numeric AS twd_amount
FROM newest n
JOIN units q ON q.id = n.quote_unit_id
LEFT JOIN newest qn ON qn.unit_id = n.quote_unit_id
LEFT JOIN units qq ON qq.id = qn.quote_unit_id
UNION ALL
SELECT id, NULL, NULL, symbol, 1, 1, 1, 1
FROM units
WHERE kind = 'money' AND symbol = 'TWD';

-- +goose Down
DELETE FROM prices WHERE withdrawn_at IS NOT NULL;

CREATE OR REPLACE VIEW newest_prices AS
WITH newest AS (
    SELECT DISTINCT ON (p.unit_id) p.unit_id, p.as_of, p.source, p.amount, p.quote_unit_id, p.quote_amount
    FROM prices p
    ORDER BY p.unit_id, p.as_of DESC, p.source
)
SELECT n.unit_id, n.as_of, n.source, q.symbol AS quote, n.amount, n.quote_amount,
       (CASE WHEN q.symbol = 'TWD' THEN n.amount WHEN qq.symbol = 'TWD' THEN n.amount * qn.amount END)::numeric AS unit_amount,
       (CASE WHEN q.symbol = 'TWD' THEN n.quote_amount WHEN qq.symbol = 'TWD' THEN n.quote_amount * qn.quote_amount END)::numeric AS twd_amount
FROM newest n
JOIN units q ON q.id = n.quote_unit_id
LEFT JOIN newest qn ON qn.unit_id = n.quote_unit_id
LEFT JOIN units qq ON qq.id = qn.quote_unit_id
UNION ALL
SELECT id, NULL, NULL, symbol, 1, 1, 1, 1
FROM units
WHERE kind = 'money' AND symbol = 'TWD';

ALTER TABLE prices DROP COLUMN withdrawn_at;
