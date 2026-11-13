-- Search filters: a NULL argument means "no filter". The ones every view of the lines shares are
-- line_matches (migration 00017). With @fold, a line with a parent (a linked fee or refund) is not a row of its
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
      AND (sqlc.narg(kinds)::text[] IS NULL OR t.kind = ANY (sqlc.narg(kinds)::text[]))
      AND (sqlc.narg(institution)::text IS NULL OR s.institution = sqlc.narg(institution)::text)
      AND (sqlc.narg(card_last4)::text IS NULL OR t.card_last4 = sqlc.narg(card_last4)::text)
      AND (sqlc.narg(account_id)::bigint IS NULL OR t.account_id = sqlc.narg(account_id)::bigint)
      AND line_matches(t.transaction_date, s.closing_date, t.original_currency, t.country, a.kind, t.counterpart_id, t.kind,
                       t.description, t.memo,
                       from_date => sqlc.narg(from_date)::date, to_date => sqlc.narg(to_date)::date,
                       closing_from => sqlc.narg(closing_from)::date, closing_to => sqlc.narg(closing_to)::date,
                       overseas => @overseas::boolean, of_account_kind => sqlc.narg(account_kind)::text,
                       hide_transfers => @hide_transfers::boolean, query => sqlc.narg(query)::text)
)
SELECT t.id, s.id AS statement_id, s.institution, t.transaction_date, t.posting_date, t.description, t.kind, t.amount,
       t.original_amount, t.original_currency, t.country, t.card_last4, t.parent_id, par.ref::jsonb AS parent, kids.list::jsonb AS children,
       -- Its account, and the amount seen from the owner (ADR 0013): money out negative, a card line's
       -- sign turned, net of the children folded into it.
       a.kind AS account_kind, t.account_id, coalesce(a.name, a.label) AS account_name,
       coalesce(a.full_number, a.number) AS account_number, u.symbol AS unit, t.memo, t.counterparty, t.balance,
       owner_flow(a.kind, t.amount + coalesce(kids.amount, 0))::numeric AS flow,
       -- A trade's money (ADR 0017).
       tr.category AS trade_category, tr.price AS trade_price, tr.amount AS trade_amount, tr.fee AS trade_fee,
       tr.tax AS trade_tax, tr.net AS trade_net, tr.prepaid AS trade_prepaid,
       -- Where its money went or came from (ADR 0016).
       c.id AS counterpart_id, c.statement_id AS counterpart_statement_id, c.transaction_date AS counterpart_date,
       ca.id AS counterpart_account_id, ca.kind AS counterpart_account_kind, ca.institution AS counterpart_institution,
       coalesce(ca.name, ca.label) AS counterpart_account_name, ca.number AS counterpart_account_number,
       ca.full_number AS counterpart_full_number
FROM matched m
JOIN transactions t ON t.id = m.id
JOIN statements s ON s.id = t.statement_id
JOIN accounts a ON a.id = t.account_id
JOIN units u ON u.id = t.unit_id
LEFT JOIN trades tr ON tr.transaction_id = t.id
LEFT JOIN transactions c ON c.id = t.counterpart_id
LEFT JOIN accounts ca ON ca.id = c.account_id
-- Keys are typed: Database Observability (ADR 0011) prepares this query with its constants as
-- parameters, and jsonb_build_object takes any type, so an untyped key leaves it unplannable.
LEFT JOIN LATERAL (SELECT jsonb_build_object('id'::text, p.id, 'statementId'::text, p.statement_id, 'kind'::text, p.kind,
                                             'transactionDate'::text, p.transaction_date, 'amount'::text, p.amount::text,
                                             'originalAmount'::text, p.original_amount::text, 'originalCurrency'::text, p.original_currency) AS ref
                   FROM transactions p
                   WHERE p.id = t.parent_id) par ON true
LEFT JOIN LATERAL (SELECT sum(c.amount) AS amount,
                          jsonb_agg(jsonb_build_object('id'::text, c.id, 'statementId'::text, c.statement_id, 'kind'::text, c.kind,
                                                       'transactionDate'::text, c.transaction_date, 'amount'::text, c.amount::text,
                                             'originalAmount'::text, c.original_amount::text, 'originalCurrency'::text, c.original_currency)
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
           coalesce(a.full_number, a.number) AS account_number, s.institution, t.card_last4, u.symbol AS unit,
           t.id, t.counterpart_id, (a.kind = 'deposit' AND t.amount < 0) AS outflow,
           (sqlc.narg(kinds)::text[] IS NULL OR t.kind = ANY (sqlc.narg(kinds)::text[])) AS kind_match,
           ((sqlc.narg(institution)::text IS NULL OR s.institution = sqlc.narg(institution)::text)
               -- IS NOT DISTINCT FROM: rows without a card (payments) must give false, not NULL.
               AND (sqlc.narg(card_last4)::text IS NULL OR t.card_last4 IS NOT DISTINCT FROM sqlc.narg(card_last4)::text)
               AND (sqlc.narg(account_id)::bigint IS NULL OR t.account_id = sqlc.narg(account_id)::bigint)) AS pick_match,
           t.amount + coalesce(kids.amount, 0) AS amount,
           owner_flow(a.kind, t.amount + coalesce(kids.amount, 0)) AS flow,
           (t.counterpart_id IS NOT NULL OR t.kind = 'payment') AS transfer,
           -- Shares or points, not money: neither spent nor received (ADR 0017, ADR 0019).
           a.kind IN ('securities', 'points') AS shares
    FROM transactions t
    JOIN statements s ON s.id = t.statement_id
    JOIN accounts a ON a.id = t.account_id
    JOIN units u ON u.id = t.unit_id
    LEFT JOIN LATERAL (SELECT sum(c.amount) AS amount
                       FROM transactions c
                       WHERE @fold::boolean AND c.parent_id = t.id) kids ON true
    WHERE NOT (@fold::boolean AND t.parent_id IS NOT NULL)
      AND line_matches(t.transaction_date, s.closing_date, t.original_currency, t.country, a.kind, t.counterpart_id, t.kind,
                       t.description, t.memo,
                       from_date => sqlc.narg(from_date)::date, to_date => sqlc.narg(to_date)::date,
                       closing_from => sqlc.narg(closing_from)::date, closing_to => sqlc.narg(closing_to)::date,
                       overseas => @overseas::boolean, of_account_kind => sqlc.narg(account_kind)::text,
                       hide_transfers => @hide_transfers::boolean, query => sqlc.narg(query)::text)
),
marked AS (
    SELECT l.kind, l.account_kind, l.account_id, l.account_name, l.account_number, l.institution, l.card_last4, l.unit,
           l.kind_match, l.pick_match, l.amount, l.flow, l.transfer, l.shares,
           (l.counterpart_id IS NOT NULL AND NOT l.outflow
               AND EXISTS (SELECT 1 FROM lines o WHERE o.id = l.counterpart_id AND o.kind_match AND o.pick_match)) AS folded
    FROM lines l
)
SELECT kind, account_kind, account_id, account_name, account_number, institution, card_last4, unit,
       kind_match::boolean AS kind_match, pick_match::boolean AS pick_match, folded::boolean AS folded,
       count(*)::integer AS row_count,
       sum(amount)::numeric AS amount,
       coalesce(sum(flow) FILTER (WHERE flow < 0 AND NOT transfer AND NOT shares), 0)::numeric AS spent,
       coalesce(sum(flow) FILTER (WHERE flow > 0 AND NOT transfer AND NOT shares), 0)::numeric AS received,
       count(*) FILTER (WHERE transfer AND NOT shares)::integer AS moved_count,
       coalesce(sum(abs(flow)) FILTER (WHERE transfer AND NOT shares), 0)::numeric AS moved
FROM marked
GROUP BY kind, account_kind, account_id, account_name, account_number, institution, card_last4, unit, kind_match, pick_match, folded
ORDER BY kind, institution, card_last4, account_id, unit;

-- name: KindTotals :many
-- The kind facet: lines as printed, whether or not they have a parent; every filter but the kind one
-- applies.
SELECT t.kind,
       count(*)::integer AS row_count,
       sum(t.amount)::numeric AS amount,
       sum(owner_flow(a.kind, t.amount))::numeric AS flow
FROM transactions t
JOIN statements s ON s.id = t.statement_id
JOIN accounts a ON a.id = t.account_id
WHERE (sqlc.narg(institution)::text IS NULL OR s.institution = sqlc.narg(institution)::text)
  AND (sqlc.narg(card_last4)::text IS NULL OR t.card_last4 IS NOT DISTINCT FROM sqlc.narg(card_last4)::text)
  AND (sqlc.narg(account_id)::bigint IS NULL OR t.account_id = sqlc.narg(account_id)::bigint)
  AND line_matches(t.transaction_date, s.closing_date, t.original_currency, t.country, a.kind, t.counterpart_id, t.kind,
      t.description, t.memo,
      from_date => sqlc.narg(from_date)::date, to_date => sqlc.narg(to_date)::date,
      closing_from => sqlc.narg(closing_from)::date, closing_to => sqlc.narg(closing_to)::date,
      overseas => @overseas::boolean, of_account_kind => sqlc.narg(account_kind)::text,
      hide_transfers => @hide_transfers::boolean, query => sqlc.narg(query)::text)
GROUP BY t.kind
ORDER BY t.kind;

-- name: TransactionFamily :many
-- A line with its parent and its children, whichever statements they are on; the same columns as
-- ListTransactions.
SELECT t.id, s.id AS statement_id, s.institution, t.transaction_date, t.posting_date, t.description, t.kind, t.amount,
       t.original_amount, t.original_currency, t.country, t.card_last4, t.parent_id, par.ref::jsonb AS parent, kids.list::jsonb AS children,
       -- Its account, and the amount seen from the owner (ADR 0013): money out negative, a card line's
       -- sign turned, net of the children folded into it.
       a.kind AS account_kind, t.account_id, coalesce(a.name, a.label) AS account_name,
       coalesce(a.full_number, a.number) AS account_number, u.symbol AS unit, t.memo, t.counterparty, t.balance,
       owner_flow(a.kind, t.amount + coalesce(kids.amount, 0))::numeric AS flow,
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
JOIN units u ON u.id = t.unit_id
LEFT JOIN trades tr ON tr.transaction_id = t.id
LEFT JOIN transactions c ON c.id = t.counterpart_id
LEFT JOIN accounts ca ON ca.id = c.account_id
-- Keys are typed: Database Observability (ADR 0011) prepares this query with its constants as
-- parameters, and jsonb_build_object takes any type, so an untyped key leaves it unplannable.
LEFT JOIN LATERAL (SELECT jsonb_build_object('id'::text, p.id, 'statementId'::text, p.statement_id, 'kind'::text, p.kind,
                                             'transactionDate'::text, p.transaction_date, 'amount'::text, p.amount::text,
                                             'originalAmount'::text, p.original_amount::text, 'originalCurrency'::text, p.original_currency) AS ref
                   FROM transactions p
                   WHERE p.id = t.parent_id) par ON true
LEFT JOIN LATERAL (SELECT NULL::numeric AS amount,
                          jsonb_agg(jsonb_build_object('id'::text, c.id, 'statementId'::text, c.statement_id, 'kind'::text, c.kind,
                                                       'transactionDate'::text, c.transaction_date, 'amount'::text, c.amount::text,
                                             'originalAmount'::text, c.original_amount::text, 'originalCurrency'::text, c.original_currency)
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
-- The lines of these kinds, which a parent link can start or end at.
SELECT t.id, s.id AS statement_id, s.institution, t.transaction_date, t.description, t.kind, t.amount,
       t.original_amount, t.original_currency, t.card_last4
FROM transactions t
JOIN statements s ON s.id = t.statement_id
WHERE t.kind = ANY (@kinds::text[])
ORDER BY t.id;

-- name: ClearParents :exec
UPDATE transactions SET parent_id = NULL WHERE parent_id IS NOT NULL;

-- name: SetParents :exec
UPDATE transactions t SET parent_id = l.parent_id
FROM (SELECT unnest(@ids::bigint[]) AS id, unnest(@parent_ids::bigint[]) AS parent_id) l
WHERE t.id = l.id;

-- name: TransferLines :many
-- The lines a transfer link can join (ADR 0016): every deposit account's lines a statement reports, card
-- payment lines, and the deposits and withdrawals an exchange reports (ADR 0018), whose statement is 0;
-- never the owner's fill of a gap, which is what moved over months, net.
SELECT t.id, t.account_id, a.kind AS account_kind, a.institution, a.number, a.full_number,
       coalesce(t.statement_id, 0)::bigint AS statement_id,
       t.transaction_date, t.kind, t.amount, u.symbol AS unit, t.memo, t.counterparty
FROM transactions t
JOIN accounts a ON a.id = t.account_id
JOIN units u ON u.id = t.unit_id
WHERE (t.statement_id IS NOT NULL AND (a.kind = 'deposit' OR t.kind = 'payment')) OR t.external_id IS NOT NULL
ORDER BY t.id;

-- name: CardBills :many
SELECT id, institution, closing_date, due_date, currency, total_due
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
