package api

import (
	"cmp"
	"maps"
	"math/big"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/ingest"
)

type tally struct {
	count  int
	amount pgtype.Numeric
}

func (t *tally) add(count int32, amount pgtype.Numeric) {
	t.count += int(count)
	t.amount = addNumeric(t.amount, amount)
}

type summary struct {
	total     tally
	flows     []UnitFlow
	byCard    []CardTotal
	byAccount []AccountTotal
}

// summarize folds the groups from TransactionTotals into the search total, what went out, came in, and
// moved per unit, and the account picker's facets: cards and deposit accounts, which skip the picker's
// own filter but keep the kind filter. The facets count each account's lines whether folded under a
// link's outflow or not: picking one account lists them all, since a link joins two accounts. (A bank's
// count, the sum of its accounts', counts both lines of a link within the bank.)
func summarize(groups []dbq.TransactionTotalsRow) summary {
	total := tally{amount: zero()}
	type flow struct {
		spent, received, moved pgtype.Numeric
		movedCount             int
	}
	flows := map[string]*flow{}
	type cardKey struct{ institution, last4 string }
	cards := map[cardKey]*tally{}
	accounts := map[int64]*AccountTotal{}

	for _, g := range groups {
		if g.KindMatch && g.PickMatch && !g.Folded {
			total.add(g.RowCount, g.Amount)
		}
		// Shares, not money: a trade is neither spent nor received (ADR 0017).
		if g.KindMatch && g.PickMatch && !g.Folded && g.AccountKind != ingest.KindSecurities {
			if flows[g.Unit] == nil {
				flows[g.Unit] = &flow{spent: zero(), received: zero(), moved: zero()}
			}
			f := flows[g.Unit]
			// A folded line is always a transfer: never spent or received, and its link is counted once,
			// at the outflow it is listed under.
			f.spent, f.received = addNumeric(f.spent, g.Spent), addNumeric(f.received, g.Received)
			f.moved, f.movedCount = addNumeric(f.moved, g.Moved), f.movedCount+int(g.MovedCount)
		}
		if !g.KindMatch {
			continue
		}
		if g.AccountKind == ingest.KindCreditCard {
			key := cardKey{institution: g.Institution, last4: deref(g.CardLast4)}
			if cards[key] == nil {
				cards[key] = &tally{amount: zero()}
			}
			cards[key].add(g.RowCount, g.Amount)
			continue
		}
		if accounts[g.AccountID] == nil {
			accounts[g.AccountID] = &AccountTotal{AccountId: g.AccountID, Institution: g.Institution, AccountName: g.AccountName, AccountNumber: &g.AccountNumber}
		}
		accounts[g.AccountID].Count += int(g.RowCount)
	}

	result := summary{total: total, flows: make([]UnitFlow, 0, len(flows))}
	for _, unit := range slices.Sorted(maps.Keys(flows)) {
		f := flows[unit]
		result.flows = append(result.flows, UnitFlow{
			Unit: unit, Spent: decimalString(f.spent), Received: decimalString(f.received), Moved: decimalString(f.moved), MovedCount: f.movedCount,
		})
	}
	cardOrder := slices.SortedFunc(maps.Keys(cards), func(a, b cardKey) int {
		return cmp.Or(strings.Compare(a.institution, b.institution), strings.Compare(a.last4, b.last4))
	})
	result.byCard = make([]CardTotal, 0, len(cards))
	for _, key := range cardOrder {
		t := cards[key]
		card := CardTotal{Institution: key.institution, Count: t.count, Amount: decimalString(t.amount)}
		if key.last4 != "" {
			card.CardLast4 = &key.last4
		}
		result.byCard = append(result.byCard, card)
	}
	result.byAccount = make([]AccountTotal, 0, len(accounts))
	for _, account := range accounts {
		result.byAccount = append(result.byAccount, *account)
	}
	slices.SortFunc(result.byAccount, func(a, b AccountTotal) int {
		return cmp.Or(strings.Compare(a.Institution, b.Institution), cmp.Compare(a.AccountId, b.AccountId))
	})
	return result
}

// kindTotals is the kind facet, from KindTotals (already in kind order).
func kindTotals(rows []dbq.KindTotalsRow) []KindTotal {
	byKind := make([]KindTotal, 0, len(rows))
	for _, row := range rows {
		byKind = append(byKind, KindTotal{Kind: TransactionKind(row.Kind), Count: int(row.RowCount), Amount: decimalString(row.Amount), Flow: decimalString(row.Flow)})
	}
	return byKind
}

func zero() pgtype.Numeric {
	return pgtype.Numeric{Int: big.NewInt(0), Valid: true}
}

// addNumeric adds exactly, keeping the finer of the two scales ("1.5" + "2.25" = "3.75").
func addNumeric(a, b pgtype.Numeric) pgtype.Numeric {
	if !b.Valid {
		return a
	}
	if !a.Valid {
		return b
	}
	exp := min(a.Exp, b.Exp)
	sum := new(big.Int).Add(scaled(a, exp), scaled(b, exp))
	return pgtype.Numeric{Int: sum, Exp: exp, Valid: true}
}

// scaled returns n's digits as an integer count of 10^exp, for exp <= n.Exp.
func scaled(n pgtype.Numeric, exp int32) *big.Int {
	factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n.Exp-exp)), nil)
	return new(big.Int).Mul(n.Int, factor)
}
