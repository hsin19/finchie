-- +goose Up

-- The search filters every view of the lines applies alike (SearchTransactions, TransactionTotals,
-- KindTotals): one definition, so the list, its totals, and its facets never disagree. A NULL filter
-- matches everything. The kind filter and the account picker's are each query's own: the totals turn
-- them into flags, and each facet leaves out its own.
-- +goose StatementBegin
CREATE FUNCTION line_matches(
    transaction_date DATE, closing_date DATE, original_currency TEXT, country TEXT, account_kind TEXT,
    counterpart_id BIGINT, kind TEXT, description TEXT, memo TEXT,
    from_date DATE, to_date DATE, closing_from DATE, closing_to DATE, overseas BOOLEAN,
    of_account_kind TEXT, hide_transfers BOOLEAN, query TEXT
) RETURNS BOOLEAN
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    RETURN (from_date IS NULL OR transaction_date >= from_date)
        AND (to_date IS NULL OR transaction_date <= to_date)
        AND (closing_from IS NULL OR closing_date >= closing_from)
        AND (closing_to IS NULL OR closing_date <= closing_to)
        AND (NOT overseas OR original_currency IS NOT NULL OR country <> 'TW')
        AND (of_account_kind IS NULL OR account_kind = of_account_kind)
        -- A transfer between the owner's lines, or a card bill's payment line, is neither spent nor
        -- received (ADR 0016).
        AND (NOT hide_transfers OR (counterpart_id IS NULL AND kind <> 'payment'))
        AND (query IS NULL OR strpos(lower(description || ' ' || coalesce(memo, '')), lower(query)) > 0);
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION line_matches(DATE, DATE, TEXT, TEXT, TEXT, BIGINT, TEXT, TEXT, TEXT, DATE, DATE, DATE, DATE, BOOLEAN, TEXT, BOOLEAN, TEXT);
