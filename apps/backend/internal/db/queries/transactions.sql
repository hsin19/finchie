-- Search filters: a NULL argument means "no filter". SearchTransactions and TransactionTotals must
-- apply the same ones. With @fold, a line with a parent (a linked fee or refund) is not a row of its
-- own: it is one of its parent's children, and the parent's amounts count it (ADR 0009).

-- name: SearchTransactions :many
SELECT t.id, t.statement_id, s.institution, t.transaction_date, t.posting_date, t.description, t.kind, t.amount,
       t.original_amount, t.original_currency, t.country, t.card_last4, t.parent_id, par.ref::jsonb AS parent, kids.list::jsonb AS children
FROM transactions t
JOIN statements s ON s.id = t.statement_id
LEFT JOIN LATERAL (SELECT jsonb_build_object('id', p.id, 'statementId', p.statement_id, 'kind', p.kind,
                                             'transactionDate', p.transaction_date, 'amount', p.amount::text,
                                             'originalAmount', p.original_amount::text, 'originalCurrency', p.original_currency) AS ref
                   FROM transactions p
                   WHERE p.id = t.parent_id) par ON true
LEFT JOIN LATERAL (SELECT sum(c.amount) AS amount,
                          jsonb_agg(jsonb_build_object('id', c.id, 'statementId', c.statement_id, 'kind', c.kind,
                                                       'transactionDate', c.transaction_date, 'amount', c.amount::text,
                                             'originalAmount', c.original_amount::text, 'originalCurrency', c.original_currency)
                                    ORDER BY c.transaction_date, c.id) AS list
                   FROM transactions c
                   WHERE @fold::boolean AND c.parent_id = t.id) kids ON true
WHERE NOT (@fold::boolean AND t.parent_id IS NOT NULL)
  -- Card lines only: deposit lines count money in as positive (ADR 0013) and get views of their own.
  AND s.kind = 'credit_card'
  AND (sqlc.narg(from_date)::date IS NULL OR t.transaction_date >= sqlc.narg(from_date)::date)
  AND (sqlc.narg(to_date)::date IS NULL OR t.transaction_date <= sqlc.narg(to_date)::date)
  AND (sqlc.narg(closing_from)::date IS NULL OR s.closing_date >= sqlc.narg(closing_from)::date)
  AND (sqlc.narg(closing_to)::date IS NULL OR s.closing_date <= sqlc.narg(closing_to)::date)
  AND (sqlc.narg(kinds)::text[] IS NULL OR t.kind = ANY (sqlc.narg(kinds)::text[]))
  AND (sqlc.narg(institution)::text IS NULL OR s.institution = sqlc.narg(institution)::text)
  AND (sqlc.narg(card_last4)::text IS NULL OR t.card_last4 = sqlc.narg(card_last4)::text)
  AND (NOT @overseas::boolean OR t.original_currency IS NOT NULL OR t.country <> 'TW')
  AND (sqlc.narg(query)::text IS NULL OR strpos(lower(t.description), lower(sqlc.narg(query)::text)) > 0)
-- Sort: newest (the default), oldest, largest, or smallest; amounts net of children and by size, so a
-- big payment counts as large. Ties go newest first, then in statement line order.
ORDER BY CASE WHEN @sort::text = 'largest' THEN abs(t.amount + coalesce(kids.amount, 0)) END DESC NULLS LAST,
         CASE WHEN @sort::text = 'smallest' THEN abs(t.amount + coalesce(kids.amount, 0)) END ASC NULLS LAST,
         CASE WHEN @sort::text = 'oldest' THEN t.transaction_date END ASC NULLS LAST,
         t.transaction_date DESC, t.statement_id DESC, t.line_no
LIMIT @row_limit OFFSET @row_offset;

-- name: TransactionTotals :many
-- The rows SearchTransactions lists, one group per (kind, card) with flags for whether the group
-- passes the kind and card filters, so the caller can build the full total and the card facet from a
-- single scan. Amounts are net of children.
SELECT t.kind,
       s.institution,
       t.card_last4,
       (sqlc.narg(kinds)::text[] IS NULL OR t.kind = ANY (sqlc.narg(kinds)::text[]))::boolean AS kind_match,
       ((sqlc.narg(institution)::text IS NULL OR s.institution = sqlc.narg(institution)::text)
           -- IS NOT DISTINCT FROM: rows without a card (payments) must give false, not NULL.
           AND (sqlc.narg(card_last4)::text IS NULL OR t.card_last4 IS NOT DISTINCT FROM sqlc.narg(card_last4)::text))::boolean AS card_match,
       count(*)::integer AS row_count,
       sum(t.amount + coalesce(kids.amount, 0))::numeric AS amount
FROM transactions t
JOIN statements s ON s.id = t.statement_id
LEFT JOIN LATERAL (SELECT sum(c.amount) AS amount
                   FROM transactions c
                   WHERE @fold::boolean AND c.parent_id = t.id) kids ON true
WHERE NOT (@fold::boolean AND t.parent_id IS NOT NULL)
  -- Card lines only: deposit lines count money in as positive (ADR 0013) and get views of their own.
  AND s.kind = 'credit_card'
  AND (sqlc.narg(from_date)::date IS NULL OR t.transaction_date >= sqlc.narg(from_date)::date)
  AND (sqlc.narg(to_date)::date IS NULL OR t.transaction_date <= sqlc.narg(to_date)::date)
  AND (sqlc.narg(closing_from)::date IS NULL OR s.closing_date >= sqlc.narg(closing_from)::date)
  AND (sqlc.narg(closing_to)::date IS NULL OR s.closing_date <= sqlc.narg(closing_to)::date)
  AND (NOT @overseas::boolean OR t.original_currency IS NOT NULL OR t.country <> 'TW')
  AND (sqlc.narg(query)::text IS NULL OR strpos(lower(t.description), lower(sqlc.narg(query)::text)) > 0)
GROUP BY t.kind, s.institution, t.card_last4
ORDER BY t.kind, s.institution, t.card_last4;

-- name: KindTotals :many
-- The kind facet: lines as printed, whether or not they have a parent; every filter but the kind one
-- applies.
SELECT t.kind,
       count(*)::integer AS row_count,
       sum(t.amount)::numeric AS amount
FROM transactions t
JOIN statements s ON s.id = t.statement_id
WHERE s.kind = 'credit_card'
  AND (sqlc.narg(from_date)::date IS NULL OR t.transaction_date >= sqlc.narg(from_date)::date)
  AND (sqlc.narg(to_date)::date IS NULL OR t.transaction_date <= sqlc.narg(to_date)::date)
  AND (sqlc.narg(closing_from)::date IS NULL OR s.closing_date >= sqlc.narg(closing_from)::date)
  AND (sqlc.narg(closing_to)::date IS NULL OR s.closing_date <= sqlc.narg(closing_to)::date)
  AND (sqlc.narg(institution)::text IS NULL OR s.institution = sqlc.narg(institution)::text)
  AND (sqlc.narg(card_last4)::text IS NULL OR t.card_last4 IS NOT DISTINCT FROM sqlc.narg(card_last4)::text)
  AND (NOT @overseas::boolean OR t.original_currency IS NOT NULL OR t.country <> 'TW')
  AND (sqlc.narg(query)::text IS NULL OR strpos(lower(t.description), lower(sqlc.narg(query)::text)) > 0)
GROUP BY t.kind
ORDER BY t.kind;

-- name: TransactionFamily :many
-- A line with its parent and its children, whichever statements they are on; the same columns as
-- ListTransactions.
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
WHERE t.id = @id
   OR t.id = (SELECT p.parent_id FROM transactions p WHERE p.id = @id)
   OR t.parent_id = @id
ORDER BY t.transaction_date, t.id;

-- Linking (internal/ingest): every link is worked out again from all the lines, under a lock so two
-- statements stored at once do not interleave.

-- name: LockLinks :exec
SELECT pg_advisory_xact_lock(4243711);

-- name: LinkLines :many
-- The lines a link can start or end at.
SELECT t.id, t.statement_id, s.institution, t.transaction_date, t.description, t.kind, t.amount,
       t.original_amount, t.original_currency, t.card_last4
FROM transactions t
JOIN statements s ON s.id = t.statement_id
WHERE t.kind IN ('purchase', 'foreign_transaction_fee', 'refund')
ORDER BY t.id;

-- name: ClearParents :exec
UPDATE transactions SET parent_id = NULL WHERE parent_id IS NOT NULL;

-- name: SetParents :exec
UPDATE transactions t SET parent_id = l.parent_id
FROM (SELECT unnest(@ids::bigint[]) AS id, unnest(@parent_ids::bigint[]) AS parent_id) l
WHERE t.id = l.id;

-- name: TransferLines :many
-- The lines a transfer link can join (ADR 0016): every deposit account's lines, and card payment lines.
SELECT t.id, t.account_id, a.kind AS account_kind, a.institution, a.number, a.full_number, t.statement_id,
       t.transaction_date, t.kind, t.amount, t.counterparty
FROM transactions t
JOIN accounts a ON a.id = t.account_id
WHERE a.kind = 'deposit' OR t.kind = 'payment'
ORDER BY t.id;

-- name: CardBills :many
SELECT id, institution, closing_date, due_date, total_due
FROM statements
WHERE kind = 'credit_card'
ORDER BY institution, closing_date;

-- name: ClearTransfers :exec
UPDATE transactions SET counterpart_id = NULL WHERE counterpart_id IS NOT NULL;

-- name: ClearPaid :exec
UPDATE statements SET paid_on = NULL, paid_by = NULL WHERE paid_on IS NOT NULL OR paid_by IS NOT NULL;

-- name: SetCounterparts :exec
UPDATE transactions t SET counterpart_id = l.counterpart_id
FROM (SELECT unnest(@ids::bigint[]) AS id, unnest(@counterpart_ids::bigint[]) AS counterpart_id) l
WHERE t.id = l.id;

-- name: SetPaid :exec
-- paid_by 0 is none: a bill that owes nothing is paid by no line.
UPDATE statements s SET paid_on = p.paid_on, paid_by = NULLIF(p.paid_by, 0)
FROM (SELECT unnest(@ids::bigint[]) AS id, unnest(@paid_on::date[]) AS paid_on,
             unnest(@paid_by::bigint[]) AS paid_by) p
WHERE s.id = p.id;
