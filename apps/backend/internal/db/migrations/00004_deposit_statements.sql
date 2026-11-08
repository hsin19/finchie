-- +goose Up

-- Deposit statements beside card bills (ADR 0013). A deposit statement covers accounts each with
-- balances of its own, so the bill's columns are the bill's only.
ALTER TABLE statements
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'credit_card' CHECK (kind IN ('credit_card', 'deposit')),
    ADD COLUMN period_start DATE,
    ALTER COLUMN due_date DROP NOT NULL,
    ALTER COLUMN currency DROP NOT NULL,
    ALTER COLUMN total_due DROP NOT NULL,
    ALTER COLUMN computed_new_charges DROP NOT NULL,
    ALTER COLUMN stated_new_charges DROP NOT NULL,
    ALTER COLUMN stated_label DROP NOT NULL,
    ADD CONSTRAINT statements_card_bill_check CHECK (
        kind <> 'credit_card' OR (due_date IS NOT NULL AND currency IS NOT NULL AND total_due IS NOT NULL
            AND computed_new_charges IS NOT NULL AND stated_new_charges IS NOT NULL AND stated_label IS NOT NULL)),
    -- A bank's card bill and its deposit statement can close on the same day.
    DROP CONSTRAINT statements_institution_closing_date_key,
    ADD CONSTRAINT statements_institution_kind_closing_date_key UNIQUE (institution, kind, closing_date);

ALTER TABLE statements ALTER COLUMN kind DROP DEFAULT;

-- The source's own name for an account ("臺幣活存"), as last reported; the owner's is accounts.name.
ALTER TABLE accounts ADD COLUMN label TEXT;

ALTER TABLE transactions
    DROP CONSTRAINT transactions_kind_check,
    ADD CONSTRAINT transactions_kind_check CHECK (kind IN (
        'purchase', 'foreign_transaction_fee', 'annual_fee', 'refund', 'rebate', 'payment', -- card lines
        'interest', 'fee', 'other')),                                                      -- deposit lines
    ADD COLUMN memo         TEXT,
    ADD COLUMN counterparty TEXT,
    -- The running balance after the line, when the statement prints one.
    ADD COLUMN balance      NUMERIC;

-- +goose Down
DELETE FROM statements WHERE kind = 'deposit';
DELETE FROM transactions WHERE kind IN ('interest', 'fee', 'other');
ALTER TABLE transactions
    DROP COLUMN balance,
    DROP COLUMN counterparty,
    DROP COLUMN memo,
    DROP CONSTRAINT transactions_kind_check,
    ADD CONSTRAINT transactions_kind_check CHECK (kind IN (
        'purchase', 'foreign_transaction_fee', 'annual_fee', 'refund', 'rebate', 'payment'));
ALTER TABLE accounts DROP COLUMN label;
ALTER TABLE statements
    DROP CONSTRAINT statements_institution_kind_closing_date_key,
    ADD CONSTRAINT statements_institution_closing_date_key UNIQUE (institution, closing_date),
    DROP CONSTRAINT statements_card_bill_check,
    ALTER COLUMN due_date SET NOT NULL,
    ALTER COLUMN currency SET NOT NULL,
    ALTER COLUMN total_due SET NOT NULL,
    ALTER COLUMN computed_new_charges SET NOT NULL,
    ALTER COLUMN stated_new_charges SET NOT NULL,
    ALTER COLUMN stated_label SET NOT NULL,
    DROP COLUMN period_start,
    DROP COLUMN kind;
