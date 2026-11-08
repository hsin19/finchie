-- name: DeleteStatementsForDocument :exec
DELETE FROM statements
WHERE document_id = @document_id;

-- name: FindStatementByKey :one
SELECT id, document_id
FROM statements
WHERE institution = @institution AND closing_date = @closing_date;

-- name: InsertStatement :one
INSERT INTO statements (document_id, institution, closing_date, due_date, currency, total_due,
                        computed_new_charges, stated_new_charges, stated_label, balanced)
VALUES (@document_id, @institution, @closing_date, @due_date, @currency, @total_due,
        @computed_new_charges, @stated_new_charges, @stated_label, @balanced)
RETURNING id;

-- name: InsertTransactions :copyfrom
INSERT INTO transactions (statement_id, account_id, unit, line_no, transaction_date, posting_date, description, kind,
                          amount, original_amount, original_currency, country, card_last4)
VALUES (@statement_id, @account_id, @unit, @line_no, @transaction_date, @posting_date, @description, @kind,
        @amount, @original_amount, @original_currency, @country, @card_last4);

-- name: ListStatements :many
SELECT s.id, s.document_id, s.institution, s.closing_date, s.due_date, s.currency, s.total_due,
       s.computed_new_charges, s.stated_new_charges, s.stated_label, s.balanced,
       (SELECT count(*) FROM transactions t WHERE t.statement_id = s.id)::integer AS transaction_count
FROM statements s
ORDER BY s.closing_date DESC, s.id DESC
LIMIT @row_limit;

-- name: GetStatement :one
SELECT s.id, s.document_id, s.institution, s.closing_date, s.due_date, s.currency, s.total_due,
       s.computed_new_charges, s.stated_new_charges, s.stated_label, s.balanced,
       (SELECT count(*) FROM transactions t WHERE t.statement_id = s.id)::integer AS transaction_count
FROM statements s
WHERE s.id = @id;

-- name: StatementExists :one
SELECT EXISTS (SELECT 1 FROM statements WHERE id = @id);

-- name: ListTransactions :many
-- The lines as printed, each with its parent (parent_id) and its children, which may be on other
-- statements.
SELECT t.id, t.statement_id, s.institution, t.transaction_date, t.posting_date, t.description, t.kind, t.amount,
       t.original_amount, t.original_currency, t.country, t.card_last4, t.parent_id, par.ref::jsonb AS parent, kids.list::jsonb AS children
FROM transactions t
JOIN statements s ON s.id = t.statement_id
LEFT JOIN LATERAL (SELECT jsonb_build_object('id', p.id, 'statementId', p.statement_id, 'kind', p.kind,
                                             'transactionDate', p.transaction_date, 'amount', p.amount::text,
                                             'originalAmount', p.original_amount::text, 'originalCurrency', p.original_currency) AS ref
                   FROM transactions p
                   WHERE p.id = t.parent_id) par ON true
LEFT JOIN LATERAL (SELECT jsonb_agg(jsonb_build_object('id', c.id, 'statementId', c.statement_id, 'kind', c.kind,
                                                       'transactionDate', c.transaction_date, 'amount', c.amount::text,
                                             'originalAmount', c.original_amount::text, 'originalCurrency', c.original_currency)
                                    ORDER BY c.transaction_date, c.id) AS list
                   FROM transactions c
                   WHERE c.parent_id = t.id) kids ON true
WHERE t.statement_id = @statement_id
ORDER BY t.line_no;
