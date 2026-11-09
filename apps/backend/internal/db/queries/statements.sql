-- name: DeleteStatementsForDocument :exec
DELETE FROM statements
WHERE document_id = @document_id;

-- name: FindStatementByKey :one
SELECT id, document_id
FROM statements
WHERE institution = @institution
  AND kind = @kind
  AND period_start IS NOT DISTINCT FROM @period_start
  AND closing_date = @closing_date;

-- name: OverlappingDepositStatements :many
-- The other deposit statements whose periods share days with this one's and that report the account in
-- the currency: on-demand exports such as Cathay's e-passbook (ADR 0013). Statements that only adjoin,
-- as monthly ones do, share no day.
SELECT o.id, o.document_id, o.period_start, o.closing_date
FROM statements s
JOIN statements o ON o.id <> s.id
    AND o.kind = 'deposit'
    AND o.period_start <= s.closing_date
    AND o.closing_date >= s.period_start
WHERE s.id = @statement_id
  AND (EXISTS (SELECT 1 FROM transactions t WHERE t.statement_id = o.id AND t.account_id = @account_id AND t.unit = @unit)
    OR EXISTS (SELECT 1 FROM balances b WHERE b.statement_id = o.id AND b.account_id = @account_id AND b.unit = @unit));

-- name: TrimStatementLines :exec
-- A statement's lines of an account in a currency on the days another statement keeps.
DELETE FROM transactions
WHERE statement_id = @statement_id
  AND account_id = @account_id
  AND unit = @unit
  AND transaction_date BETWEEN @from_date AND @to_date;

-- name: TrimStatementBalances :exec
-- A statement's balances of an account in a currency on the days another statement keeps, and on a day
-- before them that statement has a balance for too: the opening balance two exports starting the same
-- day both have.
DELETE FROM balances b
WHERE b.statement_id = @statement_id::bigint
  AND b.account_id = @account_id
  AND b.unit = @unit
  AND b.as_of <= @to_date
  AND (b.as_of >= @from_date OR EXISTS (
      SELECT 1 FROM balances k
      WHERE k.statement_id = @keeper_id::bigint AND k.account_id = b.account_id AND k.unit = b.unit AND k.as_of = b.as_of));

-- name: InsertStatement :one
-- The bill's columns are null for a deposit statement (ADR 0013).
INSERT INTO statements (document_id, institution, kind, period_start, closing_date, due_date, currency, total_due,
                        computed_new_charges, stated_new_charges, stated_label, balanced)
VALUES (@document_id, @institution, @kind, @period_start, @closing_date, @due_date, @currency, @total_due,
        @computed_new_charges, @stated_new_charges, @stated_label, @balanced)
RETURNING id;

-- name: InsertTransactions :copyfrom
INSERT INTO transactions (statement_id, account_id, unit, line_no, transaction_date, posting_date, description, kind,
                          amount, original_amount, original_currency, country, card_last4, memo, counterparty, balance)
VALUES (@statement_id, @account_id, @unit, @line_no, @transaction_date, @posting_date, @description, @kind,
        @amount, @original_amount, @original_currency, @country, @card_last4, @memo, @counterparty, @balance);

-- name: ListStatements :many
SELECT s.id, s.document_id, s.institution, s.closing_date, s.due_date, s.currency, s.total_due,
       s.computed_new_charges, s.stated_new_charges, s.stated_label, s.balanced, s.paid_on,
       (SELECT count(*) FROM transactions t WHERE t.statement_id = s.id)::integer AS transaction_count
FROM statements s
-- Card bills only, until deposit statements get views of their own (ADR 0013).
WHERE s.kind = 'credit_card'
ORDER BY s.closing_date DESC, s.id DESC
LIMIT @row_limit;

-- name: GetStatement :one
SELECT s.id, s.document_id, s.institution, s.closing_date, s.due_date, s.currency, s.total_due,
       s.computed_new_charges, s.stated_new_charges, s.stated_label, s.balanced, s.paid_on,
       (SELECT count(*) FROM transactions t WHERE t.statement_id = s.id)::integer AS transaction_count
FROM statements s
WHERE s.id = @id AND s.kind = 'credit_card';

-- name: StatementExists :one
SELECT EXISTS (SELECT 1 FROM statements WHERE id = @id AND kind = 'credit_card');

-- name: ListTransactions :many
-- The lines as printed, each with its parent (parent_id) and its children, which may be on other
-- statements.
SELECT t.id, t.statement_id, s.institution, t.transaction_date, t.posting_date, t.description, t.kind, t.amount,
       t.original_amount, t.original_currency, t.country, t.card_last4, t.parent_id, par.ref::jsonb AS parent, kids.list::jsonb AS children,
       -- Its account, and the amount seen from the owner (ADR 0013): money out negative, a card line's
       -- sign turned, net of the children folded into it.
       a.kind AS account_kind, t.account_id, coalesce(a.name, a.label) AS account_name,
       coalesce(a.full_number, a.number) AS account_number, t.unit, t.memo, t.counterparty, t.balance,
       (CASE WHEN a.kind = 'credit_card' THEN -(t.amount + coalesce(kids.amount, 0)) ELSE t.amount END)::numeric AS flow,
       -- A trade's money (ADR 0017).
       tr.category AS trade_category, tr.price AS trade_price, tr.amount AS trade_amount, tr.fee AS trade_fee,
       tr.tax AS trade_tax, tr.net AS trade_net, tr.prepaid AS trade_prepaid,
       -- Where its money went or came from (ADR 0016).
       c.id AS counterpart_id, c.statement_id AS counterpart_statement_id, c.transaction_date AS counterpart_date,
       ca.id AS counterpart_account_id, ca.kind AS counterpart_account_kind, ca.institution AS counterpart_institution,
       coalesce(ca.name, ca.label) AS counterpart_account_name, ca.number AS counterpart_account_number,
       ca.full_number AS counterpart_full_number
FROM transactions t
JOIN statements s ON s.id = t.statement_id
JOIN accounts a ON a.id = t.account_id
LEFT JOIN trades tr ON tr.transaction_id = t.id
LEFT JOIN transactions c ON c.id = t.counterpart_id
LEFT JOIN accounts ca ON ca.id = c.account_id
LEFT JOIN LATERAL (SELECT jsonb_build_object('id', p.id, 'statementId', p.statement_id, 'kind', p.kind,
                                             'transactionDate', p.transaction_date, 'amount', p.amount::text,
                                             'originalAmount', p.original_amount::text, 'originalCurrency', p.original_currency) AS ref
                   FROM transactions p
                   WHERE p.id = t.parent_id) par ON true
LEFT JOIN LATERAL (SELECT NULL::numeric AS amount,
                          jsonb_agg(jsonb_build_object('id', c.id, 'statementId', c.statement_id, 'kind', c.kind,
                                                       'transactionDate', c.transaction_date, 'amount', c.amount::text,
                                             'originalAmount', c.original_amount::text, 'originalCurrency', c.original_currency)
                                    ORDER BY c.transaction_date, c.id) AS list
                   FROM transactions c
                   WHERE c.parent_id = t.id) kids ON true
WHERE t.statement_id = @statement_id
ORDER BY t.line_no;
