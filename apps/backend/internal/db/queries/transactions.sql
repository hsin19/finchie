-- Search filters: a NULL argument means "no filter". SearchTransactions and TransactionTotals must
-- apply the same ones. With @fold, a line with a parent (a linked fee or refund) is not a row of its
-- own: it is one of its parent's children, and the parent's amounts count it (ADR 0009).
-- A link (ADR 0016) is one row too: the line its money went to (another account's inflow, a card bill's
-- payment line) is listed under the outflow it came from whenever that outflow matches as well. A view
-- without the outflow, such as another account or the month before, lists the line itself.

-- name: SearchTransactions :many
WITH matched AS (
    SELECT t.id, t.counterpart_id, (a.kind = 'deposit' AND t.amount < 0) AS outflow
    FROM transactions t
    JOIN statements s ON s.id = t.statement_id
    JOIN accounts a ON a.id = t.account_id
    WHERE NOT (@fold::boolean AND t.parent_id IS NOT NULL)
      AND (sqlc.narg(from_date)::date IS NULL OR t.transaction_date >= sqlc.narg(from_date)::date)
      AND (sqlc.narg(to_date)::date IS NULL OR t.transaction_date <= sqlc.narg(to_date)::date)
      AND (sqlc.narg(closing_from)::date IS NULL OR s.closing_date >= sqlc.narg(closing_from)::date)
      AND (sqlc.narg(closing_to)::date IS NULL OR s.closing_date <= sqlc.narg(closing_to)::date)
      AND (sqlc.narg(kinds)::text[] IS NULL OR t.kind = ANY (sqlc.narg(kinds)::text[]))
      AND (sqlc.narg(institution)::text IS NULL OR s.institution = sqlc.narg(institution)::text)
      AND (sqlc.narg(card_last4)::text IS NULL OR t.card_last4 = sqlc.narg(card_last4)::text)
      AND (sqlc.narg(account_id)::bigint IS NULL OR t.account_id = sqlc.narg(account_id)::bigint)
      AND (NOT @overseas::boolean OR t.original_currency IS NOT NULL OR t.country <> 'TW')
      AND (sqlc.narg(account_kind)::text IS NULL OR a.kind = sqlc.narg(account_kind)::text)
      -- A transfer between the owner's lines, or a card bill's payment line, is neither spent nor received.
      AND (NOT @hide_transfers::boolean OR (t.counterpart_id IS NULL AND t.kind <> 'payment'))
      AND (sqlc.narg(query)::text IS NULL
           OR strpos(lower(t.description || ' ' || coalesce(t.memo, '')), lower(sqlc.narg(query)::text)) > 0)
)
SELECT t.id, t.statement_id, s.institution, t.transaction_date, t.posting_date, t.description, t.kind, t.amount,
       t.original_amount, t.original_currency, t.country, t.card_last4, t.parent_id, par.ref::jsonb AS parent, kids.list::jsonb AS children,
       -- Its account, and the amount seen from the owner (ADR 0013): money out negative, a card line's
       -- sign turned, net of the children folded into it.
       a.kind AS account_kind, t.account_id, coalesce(a.name, a.label) AS account_name,
       coalesce(a.full_number, a.number) AS account_number, t.unit, t.memo, t.counterparty, t.balance,
       (CASE WHEN a.kind = 'credit_card' THEN -(t.amount + coalesce(kids.amount, 0)) ELSE t.amount END)::numeric AS flow,
       -- Where its money went or came from (ADR 0016).
       c.id AS counterpart_id, c.statement_id AS counterpart_statement_id, c.transaction_date AS counterpart_date,
       ca.id AS counterpart_account_id, ca.kind AS counterpart_account_kind, ca.institution AS counterpart_institution,
       coalesce(ca.name, ca.label) AS counterpart_account_name, ca.number AS counterpart_account_number,
       ca.full_number AS counterpart_full_number
FROM matched m
JOIN transactions t ON t.id = m.id
JOIN statements s ON s.id = t.statement_id
JOIN accounts a ON a.id = t.account_id
LEFT JOIN transactions c ON c.id = t.counterpart_id
LEFT JOIN accounts ca ON ca.id = c.account_id
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
WHERE NOT (m.counterpart_id IS NOT NULL AND NOT m.outflow AND m.counterpart_id IN (SELECT id FROM matched))
-- Sort: newest (the default), oldest, largest, or smallest; amounts net of children and by size, so a
-- big payment counts as large. Ties go newest first, then in statement line order.
ORDER BY CASE WHEN @sort::text = 'largest' THEN abs(t.amount + coalesce(kids.amount, 0)) END DESC NULLS LAST,
         CASE WHEN @sort::text = 'smallest' THEN abs(t.amount + coalesce(kids.amount, 0)) END ASC NULLS LAST,
         CASE WHEN @sort::text = 'oldest' THEN t.transaction_date END ASC NULLS LAST,
         t.transaction_date DESC, t.statement_id DESC, t.line_no
LIMIT @row_limit OFFSET @row_offset;

-- name: TransactionTotals :many
-- The rows SearchTransactions lists, one group per kind, account, card, and unit, with flags for whether
-- the group passes the kind filter and the account picker's (bank, card, account), and whether its
-- lines are folded under the outflows they came from; so the caller can build the totals and the
-- account facets from a single scan. Amounts are net of children; spent and received are seen from the
-- owner and leave out transfers and card payments, whose money only moved (ADR 0016).
WITH lines AS (
    SELECT t.kind, a.kind AS account_kind, t.account_id, coalesce(a.name, a.label) AS account_name,
           coalesce(a.full_number, a.number) AS account_number, s.institution, t.card_last4, t.unit,
           t.id, t.counterpart_id, (a.kind = 'deposit' AND t.amount < 0) AS outflow,
           (sqlc.narg(kinds)::text[] IS NULL OR t.kind = ANY (sqlc.narg(kinds)::text[])) AS kind_match,
           ((sqlc.narg(institution)::text IS NULL OR s.institution = sqlc.narg(institution)::text)
               -- IS NOT DISTINCT FROM: rows without a card (payments) must give false, not NULL.
               AND (sqlc.narg(card_last4)::text IS NULL OR t.card_last4 IS NOT DISTINCT FROM sqlc.narg(card_last4)::text)
               AND (sqlc.narg(account_id)::bigint IS NULL OR t.account_id = sqlc.narg(account_id)::bigint)) AS pick_match,
           t.amount + coalesce(kids.amount, 0) AS amount,
           CASE WHEN a.kind = 'credit_card' THEN -(t.amount + coalesce(kids.amount, 0)) ELSE t.amount END AS flow,
           (t.counterpart_id IS NOT NULL OR t.kind = 'payment') AS transfer
    FROM transactions t
    JOIN statements s ON s.id = t.statement_id
    JOIN accounts a ON a.id = t.account_id
    LEFT JOIN LATERAL (SELECT sum(c.amount) AS amount
                       FROM transactions c
                       WHERE @fold::boolean AND c.parent_id = t.id) kids ON true
    WHERE NOT (@fold::boolean AND t.parent_id IS NOT NULL)
      AND (sqlc.narg(from_date)::date IS NULL OR t.transaction_date >= sqlc.narg(from_date)::date)
      AND (sqlc.narg(to_date)::date IS NULL OR t.transaction_date <= sqlc.narg(to_date)::date)
      AND (sqlc.narg(closing_from)::date IS NULL OR s.closing_date >= sqlc.narg(closing_from)::date)
      AND (sqlc.narg(closing_to)::date IS NULL OR s.closing_date <= sqlc.narg(closing_to)::date)
      AND (NOT @overseas::boolean OR t.original_currency IS NOT NULL OR t.country <> 'TW')
      AND (sqlc.narg(account_kind)::text IS NULL OR a.kind = sqlc.narg(account_kind)::text)
      AND (NOT @hide_transfers::boolean OR (t.counterpart_id IS NULL AND t.kind <> 'payment'))
      AND (sqlc.narg(query)::text IS NULL
           OR strpos(lower(t.description || ' ' || coalesce(t.memo, '')), lower(sqlc.narg(query)::text)) > 0)
),
marked AS (
    SELECT l.kind, l.account_kind, l.account_id, l.account_name, l.account_number, l.institution, l.card_last4, l.unit,
           l.kind_match, l.pick_match, l.amount, l.flow, l.transfer,
           (l.counterpart_id IS NOT NULL AND NOT l.outflow
               AND EXISTS (SELECT 1 FROM lines o WHERE o.id = l.counterpart_id AND o.kind_match AND o.pick_match)) AS folded
    FROM lines l
)
SELECT kind, account_kind, account_id, account_name, account_number, institution, card_last4, unit,
       kind_match::boolean AS kind_match, pick_match::boolean AS pick_match, folded::boolean AS folded,
       count(*)::integer AS row_count,
       sum(amount)::numeric AS amount,
       coalesce(sum(flow) FILTER (WHERE flow < 0 AND NOT transfer), 0)::numeric AS spent,
       coalesce(sum(flow) FILTER (WHERE flow > 0 AND NOT transfer), 0)::numeric AS received,
       count(*) FILTER (WHERE transfer)::integer AS moved_count,
       coalesce(sum(abs(flow)) FILTER (WHERE transfer), 0)::numeric AS moved
FROM marked
GROUP BY kind, account_kind, account_id, account_name, account_number, institution, card_last4, unit, kind_match, pick_match, folded
ORDER BY kind, institution, card_last4, account_id, unit;

-- name: KindTotals :many
-- The kind facet: lines as printed, whether or not they have a parent; every filter but the kind one
-- applies.
SELECT t.kind,
       count(*)::integer AS row_count,
       sum(t.amount)::numeric AS amount,
       sum(CASE WHEN a.kind = 'credit_card' THEN -t.amount ELSE t.amount END)::numeric AS flow
FROM transactions t
JOIN statements s ON s.id = t.statement_id
JOIN accounts a ON a.id = t.account_id
WHERE (sqlc.narg(from_date)::date IS NULL OR t.transaction_date >= sqlc.narg(from_date)::date)
  AND (sqlc.narg(to_date)::date IS NULL OR t.transaction_date <= sqlc.narg(to_date)::date)
  AND (sqlc.narg(closing_from)::date IS NULL OR s.closing_date >= sqlc.narg(closing_from)::date)
  AND (sqlc.narg(closing_to)::date IS NULL OR s.closing_date <= sqlc.narg(closing_to)::date)
  AND (sqlc.narg(institution)::text IS NULL OR s.institution = sqlc.narg(institution)::text)
  AND (sqlc.narg(card_last4)::text IS NULL OR t.card_last4 IS NOT DISTINCT FROM sqlc.narg(card_last4)::text)
  AND (sqlc.narg(account_id)::bigint IS NULL OR t.account_id = sqlc.narg(account_id)::bigint)
  AND (NOT @overseas::boolean OR t.original_currency IS NOT NULL OR t.country <> 'TW')
  AND (sqlc.narg(account_kind)::text IS NULL OR a.kind = sqlc.narg(account_kind)::text)
  -- A transfer between the owner's lines, or a card bill's payment line, is neither spent nor received.
  AND (NOT @hide_transfers::boolean OR (t.counterpart_id IS NULL AND t.kind <> 'payment'))
  AND (sqlc.narg(query)::text IS NULL
       OR strpos(lower(t.description || ' ' || coalesce(t.memo, '')), lower(sqlc.narg(query)::text)) > 0)
GROUP BY t.kind
ORDER BY t.kind;

-- name: TransactionFamily :many
-- A line with its parent and its children, whichever statements they are on; the same columns as
-- ListTransactions.
SELECT t.id, t.statement_id, s.institution, t.transaction_date, t.posting_date, t.description, t.kind, t.amount,
       t.original_amount, t.original_currency, t.country, t.card_last4, t.parent_id, par.ref::jsonb AS parent, kids.list::jsonb AS children,
       -- Its account, and the amount seen from the owner (ADR 0013): money out negative, a card line's
       -- sign turned, net of the children folded into it.
       a.kind AS account_kind, t.account_id, coalesce(a.name, a.label) AS account_name,
       coalesce(a.full_number, a.number) AS account_number, t.unit, t.memo, t.counterparty, t.balance,
       (CASE WHEN a.kind = 'credit_card' THEN -(t.amount + coalesce(kids.amount, 0)) ELSE t.amount END)::numeric AS flow,
       -- Where its money went or came from (ADR 0016).
       c.id AS counterpart_id, c.statement_id AS counterpart_statement_id, c.transaction_date AS counterpart_date,
       ca.id AS counterpart_account_id, ca.kind AS counterpart_account_kind, ca.institution AS counterpart_institution,
       coalesce(ca.name, ca.label) AS counterpart_account_name, ca.number AS counterpart_account_number,
       ca.full_number AS counterpart_full_number
FROM transactions t
JOIN statements s ON s.id = t.statement_id
JOIN accounts a ON a.id = t.account_id
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
