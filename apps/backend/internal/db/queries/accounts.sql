-- name: UpsertAccount :one
-- The account a source reports, created the first time it appears (ADR 0013); the source's name for
-- it is kept as last reported.
INSERT INTO accounts (institution, kind, number, label)
VALUES (@institution, @kind, @number, sqlc.narg(label))
ON CONFLICT (institution, kind, number) WHERE institution <> 'owner'
DO UPDATE SET label = coalesce(EXCLUDED.label, accounts.label)
RETURNING id;

-- name: InsertBalance :exec
-- A statement's balance; a snapshot's has none (InsertSnapshot).
INSERT INTO balances (account_id, unit_id, as_of, amount, statement_id)
VALUES (@account_id, @unit_id, @as_of, @amount, @statement_id::bigint);

-- name: ListAccounts :many
-- The owner's accounts, assets and liabilities alike (ADR 0022). Of one account, or with none, of all.
SELECT id, institution, kind, number, label, name, full_number, check_again_on
FROM accounts
WHERE sqlc.narg(id)::bigint IS NULL OR id = sqlc.narg(id)::bigint
ORDER BY institution, kind DESC, number;

-- name: GetAccount :one
SELECT id, institution, kind, number, label, name, full_number
FROM accounts
WHERE id = @id;

-- name: UpdateAccountDetails :one
-- The owner's details (ADR 0016); a set flag that is false leaves its column as it is, and none clears it.
UPDATE accounts
SET name           = CASE WHEN @set_name::boolean THEN sqlc.narg(name)::text ELSE name END,
    full_number    = CASE WHEN @set_full_number::boolean THEN sqlc.narg(full_number)::text ELSE full_number END,
    check_again_on = CASE WHEN @set_check_again_on::boolean THEN sqlc.narg(check_again_on)::date ELSE check_again_on END
WHERE id = @id
RETURNING id;

-- name: NewestStatementBalances :many
-- Each account's newest balance a statement reports: what a statement about to be stored is newer than.
SELECT account_id, max(as_of)::date AS newest
FROM balances
WHERE statement_id IS NOT NULL
GROUP BY account_id;

-- name: SpendCheckAgain :exec
-- The owner's day to look again at an account is spent once a statement reports a balance newer than the
-- account's newest before (given, each account with one), however far off the day: the account is
-- current, and quiet or not from here. Parsing a statement again brings nothing newer, so it keeps it.
WITH before AS (
    SELECT unnest(@account_ids::bigint[]) AS account_id, unnest(@newest::date[]) AS newest
)
UPDATE accounts a
SET check_again_on = NULL
WHERE a.check_again_on IS NOT NULL
  AND EXISTS (SELECT 1
              FROM balances b
              WHERE b.account_id = a.id AND b.statement_id IS NOT NULL
                AND b.as_of > coalesce((SELECT newest FROM before WHERE before.account_id = a.id), '-infinity'::date));

-- name: LatestBalances :many
-- Each account's newest balance in each unit, with its unit's newest price, whatever its source, and its
-- worth in NT$ at that price (ADR 0020). A unit without a price, or priced in a unit without one in NT$,
-- has no worth. Of one account, or with none, of all.
SELECT DISTINCT ON (b.account_id, b.unit_id) b.account_id, b.unit_id, u.symbol AS unit, u.code, b.as_of, b.amount,
       b.statement_id,
       round(b.amount * p.twd_amount / p.unit_amount, 2)::numeric AS worth,
       p.as_of AS priced_at, p.source AS price_source, p.quote AS price_quote,
       trim_scale(p.quote_amount / p.amount)::numeric AS price_quoted,
       trim_scale(p.twd_amount / p.unit_amount)::numeric AS price_twd
FROM balances b
JOIN units u ON u.id = b.unit_id
LEFT JOIN newest_prices p ON p.unit_id = b.unit_id
WHERE sqlc.narg(account_id)::bigint IS NULL OR b.account_id = sqlc.narg(account_id)::bigint
ORDER BY b.account_id, b.unit_id, b.as_of DESC, b.id DESC;

-- name: LatestValuations :many
-- Each account's newest valuation (ADR 0018), with its worth in NT$ at its unit's newest price; none for
-- a unit without one.
SELECT DISTINCT ON (v.account_id) v.account_id, v.as_of, u.symbol AS unit, v.amount,
       round(v.amount * p.twd_amount / p.unit_amount, 2)::numeric AS worth
FROM valuations v
JOIN units u ON u.id = v.unit_id
LEFT JOIN newest_prices p ON p.unit_id = v.unit_id
WHERE sqlc.narg(account_id)::bigint IS NULL OR v.account_id = sqlc.narg(account_id)::bigint
ORDER BY v.account_id, v.as_of DESC;

-- name: AccountBalances :many
SELECT b.account_id, u.symbol AS unit, b.as_of, b.amount, b.statement_id
FROM balances b
JOIN units u ON u.id = b.unit_id
WHERE b.account_id = @account_id
ORDER BY b.as_of DESC, u.symbol, b.id DESC;

-- name: AccountGaps :many
-- Where an account's balances do not join up (ADR 0013): two consecutive balances in one unit whose
-- difference the lines dated between them do not account for, so money moved that no report shows.
-- Accounts of the itemized kinds (domain.AccountKind.Itemized), such as deposit and securities accounts (a
-- security's shares, ADR 0017): a card bill's lines are dated by purchase, which can fall before the
-- previous bill closed, so dates cannot place them between two bills.
-- Itemized: some statement has listed a line of the account in the unit, or its kind's statements list
-- every line (domain.AccountKind.EveryLineListed). A deposit account's unit none
-- has may be one whose statements print only its balance (CTBC's passbook savings, 活儲有摺): a move
-- there is one no source shows, not a gap. A securities statement lists every trade, so a security
-- never traded is itemized all the same: a stock dividend is a gap. A points account's bills list every
-- movement but points that lapse, so a gap there is points lost (ADR 0019).
-- The lines between them are those that count (counted_lines), so a gap the owner filled joins up and is
-- none. With the first day the later balance's statement reports, before which no report covers it.
WITH ordered AS (
    SELECT b.account_id, b.unit_id, b.statement_id, b.as_of, b.amount,
           (a.kind = ANY (@every_line_listed::text[]) OR EXISTS (
               SELECT 1 FROM transactions t
               WHERE t.account_id = b.account_id AND t.unit_id = b.unit_id AND t.statement_id IS NOT NULL))::boolean AS itemized,
           lag(b.as_of) OVER w AS prev_as_of,
           lag(b.amount) OVER w AS prev_amount
    FROM balances b
    JOIN accounts a ON a.id = b.account_id
    WHERE a.kind = ANY (@itemized::text[]) AND b.statement_id IS NOT NULL
      AND (sqlc.narg(account_id)::bigint IS NULL OR b.account_id = sqlc.narg(account_id)::bigint)
    WINDOW w AS (PARTITION BY b.account_id, b.unit_id ORDER BY b.as_of, b.id)
)
SELECT o.account_id, o.unit_id, u.symbol AS unit, o.prev_as_of::date AS from_date, o.as_of AS to_date,
       o.prev_amount::numeric AS from_amount,
       (o.amount - o.prev_amount - coalesce(seen.amount, 0))::numeric AS unseen, o.itemized,
       st.period_start AS reported_from
FROM ordered o
JOIN units u ON u.id = o.unit_id
JOIN statements st ON st.id = o.statement_id
LEFT JOIN LATERAL (SELECT sum(t.amount) AS amount
                   FROM counted_lines t
                   WHERE t.account_id = o.account_id AND t.unit_id = o.unit_id
                     AND t.transaction_date > o.prev_as_of AND t.transaction_date <= o.as_of) seen ON true
WHERE o.prev_as_of IS NOT NULL
  AND o.amount - o.prev_amount <> coalesce(seen.amount, 0)
ORDER BY o.account_id, u.symbol, o.as_of;

-- name: FillLineIn :one
-- A fill dated between two days, counting or not: the one to fill the gap between them again.
SELECT id
FROM transactions
WHERE account_id = @account_id AND unit_id = @unit_id AND kind = 'fill'
  AND transaction_date > @from_date AND transaction_date <= @to_date
ORDER BY id
LIMIT 1;

-- name: InsertFillLine :one
-- A fill as a line: on its day, with the balance it leaves.
INSERT INTO transactions (statement_id, line_no, transaction_date, description, kind, amount, account_id, unit_id, memo, balance)
VALUES (NULL, 0, @on_date, '', 'fill', @amount, @account_id, @unit_id, sqlc.narg(memo), @balance)
RETURNING id, memo;

-- name: UpsertExchangeLine :execrows
-- A deposit or withdrawal an exchange reports, or the fee it took for one (ADR 0018), found again by the
-- exchange's id for it. It affects a row only when it is new or the exchange reports it otherwise now.
INSERT INTO transactions (statement_id, line_no, transaction_date, description, kind, amount, account_id, unit_id, memo, external_id)
VALUES (NULL, 0, @on_date, @description, @kind, @amount, @account_id, @unit_id, sqlc.narg(memo), @external_id::text)
ON CONFLICT (account_id, external_id) DO UPDATE
SET transaction_date = excluded.transaction_date, description = excluded.description, kind = excluded.kind,
    amount = excluded.amount, unit_id = excluded.unit_id, memo = excluded.memo
WHERE (transactions.transaction_date, transactions.description, transactions.kind, transactions.amount, transactions.unit_id, transactions.memo)
      IS DISTINCT FROM (excluded.transaction_date, excluded.description, excluded.kind, excluded.amount, excluded.unit_id, excluded.memo);

-- name: ExchangeCursor :one
-- Where the next read of an exchange's deposits and withdrawals starts (ADR 0018).
SELECT read_from FROM exchange_cursors WHERE institution = @institution;

-- name: SetExchangeCursor :exec
INSERT INTO exchange_cursors (institution, read_from)
VALUES (@institution, @read_from)
ON CONFLICT (institution) DO UPDATE SET read_from = excluded.read_from;

-- name: UpdateFillLine :one
-- A gap filled again: what it lacks now, and what the owner wrote unless they write something else.
UPDATE transactions
SET transaction_date = @on_date, amount = @amount, balance = @balance, memo = coalesce(sqlc.narg(memo), memo)
WHERE id = @id AND kind = 'fill'
RETURNING id, memo;

-- name: SetFillMemo :execrows
UPDATE transactions
SET memo = sqlc.narg(memo)
WHERE id = @id AND account_id = @account_id AND kind = 'fill';

-- name: DeleteFillLine :execrows
DELETE FROM transactions
WHERE id = @id AND account_id = @account_id AND kind = 'fill';

-- name: AccountFills :many
-- An account's fills that count (counted_lines), the newest first, with the two balances each falls
-- between.
SELECT t.id, u.symbol AS unit, s.from_date::date AS from_date, s.to_date, t.transaction_date AS on_date, t.amount, t.memo
FROM counted_lines t
JOIN units u ON u.id = t.unit_id
JOIN balance_steps s ON s.account_id = t.account_id AND s.unit_id = t.unit_id
                    AND s.from_date < t.transaction_date AND t.transaction_date <= s.to_date
WHERE t.account_id = @account_id AND t.kind = 'fill'
ORDER BY t.transaction_date DESC, u.symbol;

-- name: AccountLines :many
-- Newest first, the lines that count (counted_lines). Amounts as the source reports them: money in is
-- positive on a deposit account, a purchase is positive on a card bill. A fill is one line for the whole
-- period no statement covers, with no statement, below a statement's lines of the same day.
SELECT t.id, t.statement_id, t.transaction_date, t.description, t.memo, t.counterparty, t.kind, u.symbol AS unit,
       t.amount, t.balance,
       -- A trade's money (ADR 0017).
       tr.category AS trade_category, tr.price AS trade_price, tr.amount AS trade_amount, tr.fee AS trade_fee,
       tr.tax AS trade_tax, tr.net AS trade_net, tr.prepaid AS trade_prepaid,
       -- Where its money went or came from (ADR 0016).
       c.id AS counterpart_id, c.statement_id AS counterpart_statement_id, c.transaction_date AS counterpart_date,
       ca.id AS counterpart_account_id, ca.kind AS counterpart_account_kind, ca.institution AS counterpart_institution,
       coalesce(ca.name, ca.label) AS counterpart_account_name, ca.number AS counterpart_account_number,
       ca.full_number AS counterpart_full_number,
       -- The two balances a fill falls between.
       step.from_date AS fill_from, step.to_date AS fill_to
FROM counted_lines t
JOIN units u ON u.id = t.unit_id
LEFT JOIN trades tr ON tr.transaction_id = t.id
LEFT JOIN transactions c ON c.id = t.counterpart_id
LEFT JOIN accounts ca ON ca.id = c.account_id
LEFT JOIN LATERAL (SELECT s.from_date::date AS from_date, s.to_date
                   FROM balance_steps s
                   WHERE t.kind = 'fill' AND s.account_id = t.account_id AND s.unit_id = t.unit_id
                     AND s.from_date < t.transaction_date AND t.transaction_date <= s.to_date
                   ORDER BY s.to_date
                   LIMIT 1) step ON true
WHERE t.account_id = @account_id
ORDER BY t.transaction_date DESC, t.statement_id DESC NULLS LAST, t.line_no DESC, t.id DESC
LIMIT @row_limit OFFSET @row_offset;

-- name: BalancesOn :many
-- What each account held of each unit at the end of each day (ADR 0023): its newest balance on or before
-- the day, and for the itemized kinds the lines that count (counted_lines, a fill of a gap among them) dated
-- after that balance up to the day; with its worth in
-- NT$ at the unit's newest price at the end of that day in Taipei, none without one then.
WITH days AS (
    SELECT d::date AS day FROM unnest(@days::date[]) d
), held AS (
    SELECT DISTINCT ON (d.day, b.account_id, b.unit_id) d.day, b.account_id, b.unit_id, b.as_of, b.amount, b.statement_id
    FROM days d
    JOIN balances b ON b.as_of <= d.day
    ORDER BY d.day, b.account_id, b.unit_id, b.as_of DESC, b.id DESC
), moved AS (
    SELECT h.day, h.account_id, h.unit_id, h.as_of, h.statement_id,
           (h.amount + CASE WHEN a.kind = ANY (@itemized::text[]) THEN coalesce((
               SELECT sum(t.amount) FROM counted_lines t
               WHERE t.account_id = h.account_id AND t.unit_id = h.unit_id
                 AND t.transaction_date > h.as_of AND t.transaction_date <= h.day), 0) ELSE 0 END)::numeric AS amount
    FROM held h
    JOIN accounts a ON a.id = h.account_id
)
SELECT m.day::date AS day, m.account_id, u.symbol AS unit, m.as_of, m.amount, m.statement_id,
       round(m.amount * p.twd_amount / p.unit_amount, 2)::numeric AS worth
FROM moved m
JOIN units u ON u.id = m.unit_id
LEFT JOIN LATERAL price_at(m.unit_id, ((m.day + 1)::timestamp AT TIME ZONE 'Asia/Taipei'))
    AS p(as_of, source, quote, amount, quote_amount, unit_amount, twd_amount) ON true
ORDER BY m.day, m.account_id, u.symbol;

-- name: ValuationsOn :many
-- Each account's newest valuation on or before each day (ADR 0018), with its worth in NT$ then.
WITH days AS (
    SELECT d::date AS day FROM unnest(@days::date[]) d
), valued AS (
    SELECT DISTINCT ON (d.day, v.account_id) d.day, v.account_id, v.unit_id, v.as_of, v.amount
    FROM days d
    JOIN valuations v ON v.as_of <= d.day
    ORDER BY d.day, v.account_id, v.as_of DESC
)
SELECT v.day::date AS day, v.account_id, v.as_of,
       round(v.amount * p.twd_amount / p.unit_amount, 2)::numeric AS worth
FROM valued v
LEFT JOIN LATERAL price_at(v.unit_id, ((v.day + 1)::timestamp AT TIME ZONE 'Asia/Taipei'))
    AS p(as_of, source, quote, amount, quote_amount, unit_amount, twd_amount) ON true
ORDER BY v.day, v.account_id;
