-- name: InvestmentFlows :many
-- Money that crossed into an investment account or out of it up to a day (domain.Flow), in NT$, positive
-- in. A trade's money paid or received (ADR 0017): the settlement account holds it outside the
-- investments, so a buy is money put in and a sale money taken out, fee and tax included; a buy paid in
-- advance prints nothing to pay and the amount prepaid. An exchange's deposit or withdrawal (ADR 0018),
-- whichever bank, wallet, or other exchange it came from or went to: its own record of it is the crossing,
-- so the other side's line is not needed. A fee moved nowhere: it shows in what the account is worth.
-- Coins and currencies at their newest price at the end of the day in Taipei; before their first price,
-- since Finchie fetches none backwards (ADR 0023), at that first one: an estimate.
SELECT t.account_id, t.transaction_date AS day,
       (CASE WHEN a.kind = 'securities' THEN coalesce(tr.prepaid, -tr.net)
             ELSE round(t.amount * p.twd_amount / p.unit_amount, 2) END)::numeric AS worth
FROM transactions t
JOIN accounts a ON a.id = t.account_id
LEFT JOIN trades tr ON tr.transaction_id = t.id
LEFT JOIN LATERAL (SELECT min(f.as_of) AS first FROM prices f WHERE f.unit_id = t.unit_id) f ON a.kind = 'exchange'
-- An interval, not `+ 1`: see BalancesOn.
LEFT JOIN LATERAL price_at(t.unit_id, greatest((t.transaction_date + '1 day'::interval) AT TIME ZONE 'Asia/Taipei', f.first))
    AS p(as_of, source, quote, amount, quote_amount, unit_amount, twd_amount) ON a.kind = 'exchange'
WHERE t.transaction_date <= @through::date
  AND ((a.kind = 'securities' AND tr.transaction_id IS NOT NULL) OR (a.kind = 'exchange' AND t.kind <> 'fee'))
ORDER BY t.transaction_date, t.id;

-- name: InvestmentsSince :one
-- The earliest day an investment account may have a worth, NULL when none has balances: none has one
-- before its first balance, nor, unless it has held NT$ (which needs no price), the day before any unit it
-- has held has a price (a rate stored at midnight counts for the day before, BalancesOn). A bound for the
-- series to start from, so a year asked for does not work out the many days before Finchie had prices; the
-- domain still finds the day each account opens. The owner's own accounts are no investments.
SELECT min(greatest(f.first, coalesce(f.priced, f.first)))::date AS since
FROM (
    SELECT b.account_id, min(b.as_of) AS first,
           CASE WHEN bool_or(u.kind = 'money' AND u.symbol = 'TWD') THEN NULL
                ELSE (SELECT ((min(p.as_of) AT TIME ZONE 'Asia/Taipei') - '1 day'::interval)::date FROM prices p
                      WHERE p.unit_id IN (SELECT h.unit_id FROM balances h WHERE h.account_id = b.account_id)) END AS priced
    FROM balances b
    JOIN accounts a ON a.id = b.account_id
    JOIN units u ON u.id = b.unit_id
    WHERE a.kind IN ('securities', 'exchange') AND a.institution <> 'owner'
    GROUP BY b.account_id
) f;
