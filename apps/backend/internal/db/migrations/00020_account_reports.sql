-- +goose Up

-- Every figure a statement prints that its lines can be checked against (ADR 0021), in place of a card
-- bill's single reconciliation in its own columns and a deposit statement's in its document's warnings.
CREATE TABLE statement_checks (
    statement_id BIGINT  NOT NULL REFERENCES statements (id) ON DELETE CASCADE,
    position     INTEGER NOT NULL,
    -- The statement's own label for the figure.
    name         TEXT    NOT NULL,
    -- The unit both are in, as the statement prints it; empty when it prints none.
    unit         TEXT    NOT NULL DEFAULT '',
    computed     NUMERIC NOT NULL,
    stated       NUMERIC NOT NULL,
    balanced     BOOLEAN NOT NULL,
    PRIMARY KEY (statement_id, position)
);

INSERT INTO statement_checks (statement_id, position, name, unit, computed, stated, balanced)
SELECT id, 0, stated_label, currency, computed_new_charges, stated_new_charges, balanced
FROM statements
WHERE kind = 'credit_card';

ALTER TABLE statements
    DROP CONSTRAINT statements_card_bill_check,
    DROP COLUMN computed_new_charges,
    DROP COLUMN stated_new_charges,
    DROP COLUMN stated_label,
    ADD CONSTRAINT statements_card_bill_check
        CHECK (kind <> 'credit_card' OR (due_date IS NOT NULL AND currency IS NOT NULL AND total_due IS NOT NULL));

-- A loan is an account (ADR 0022), kept by hand until its statements are parsed.
ALTER TABLE accounts
    DROP CONSTRAINT accounts_kind_check,
    ADD CONSTRAINT accounts_kind_check
        CHECK (kind IN ('credit_card', 'deposit', 'securities', 'exchange', 'points', 'loan'));

-- A card account is a liability with a balance (ADR 0022): what each bill says is owed, on its closing
-- date, as printed.
INSERT INTO balances (account_id, unit_id, as_of, amount, statement_id)
SELECT a.id, u.id, s.closing_date, s.total_due, s.id
FROM statements s
JOIN accounts a ON a.institution = s.institution AND a.kind = 'credit_card'
JOIN units u ON u.kind = 'money' AND u.symbol = s.currency
WHERE s.kind = 'credit_card'
ON CONFLICT DO NOTHING;

-- A unit's newest price at a moment, from any source (ADR 0020, ADR 0023): `unit_amount` of the unit is
-- worth `twd_amount` of NT$, through its quote unit's own newest price at that moment when that one is in
-- NT$: one step, never a chain; otherwise the NT$ columns are null. A price the owner withdrew counts
-- before it was withdrawn. NT$ is worth itself, with no price of its own. Without a moment, the newest.
-- +goose StatementBegin
CREATE FUNCTION price_at(of_unit BIGINT, at TIMESTAMPTZ)
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

CREATE OR REPLACE VIEW newest_prices AS
SELECT u.id AS unit_id, p.as_of::timestamptz AS as_of, p.source::text AS source, p.quote::text AS quote,
       p.amount::numeric AS amount, p.quote_amount::numeric AS quote_amount, p.unit_amount::numeric AS unit_amount,
       p.twd_amount::numeric AS twd_amount
FROM units u
CROSS JOIN LATERAL price_at(u.id, NULL) AS p(as_of, source, quote, amount, quote_amount, unit_amount, twd_amount);

-- +goose Down
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
DROP FUNCTION price_at(BIGINT, TIMESTAMPTZ);

DELETE FROM balances b USING accounts a WHERE a.id = b.account_id AND a.kind = 'credit_card';
DELETE FROM accounts WHERE kind = 'loan';
ALTER TABLE accounts
    DROP CONSTRAINT accounts_kind_check,
    ADD CONSTRAINT accounts_kind_check CHECK (kind IN ('credit_card', 'deposit', 'securities', 'exchange', 'points'));

ALTER TABLE statements
    DROP CONSTRAINT statements_card_bill_check,
    ADD COLUMN computed_new_charges NUMERIC,
    ADD COLUMN stated_new_charges NUMERIC,
    ADD COLUMN stated_label TEXT;
UPDATE statements s SET computed_new_charges = c.computed, stated_new_charges = c.stated, stated_label = c.name
FROM statement_checks c WHERE c.statement_id = s.id AND c.position = 0 AND s.kind = 'credit_card';
ALTER TABLE statements ADD CONSTRAINT statements_card_bill_check CHECK (
    kind <> 'credit_card' OR (due_date IS NOT NULL AND currency IS NOT NULL AND total_due IS NOT NULL
        AND computed_new_charges IS NOT NULL AND stated_new_charges IS NOT NULL AND stated_label IS NOT NULL));
DROP TABLE statement_checks;
