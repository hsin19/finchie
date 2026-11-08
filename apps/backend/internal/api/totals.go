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

// summarize folds the groups from TransactionTotals into the search total, what went out and came in
// per unit, and the account picker's facets: cards and deposit accounts, which skip the picker's own
// filter but keep the kind filter.
func summarize(groups []dbq.TransactionTotalsRow) summary {
	total := tally{amount: zero()}
	type flow struct{ spent, received pgtype.Numeric }
	flows := map[string]*flow{}
	type cardKey struct{ institution, last4 string }
	cards := map[cardKey]*tally{}
	accounts := map[int64]*AccountTotal{}

	for _, g := range groups {
		if g.KindMatch && g.PickMatch {
			total.add(g.RowCount, g.Amount)
			if flows[g.Unit] == nil {
				flows[g.Unit] = &flow{spent: zero(), received: zero()}
			}
			flows[g.Unit].spent = addNumeric(flows[g.Unit].spent, g.Spent)
			flows[g.Unit].received = addNumeric(flows[g.Unit].received, g.Received)
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
		result.flows = append(result.flows, UnitFlow{Unit: unit, Spent: decimalString(flows[unit].spent), Received: decimalString(flows[unit].received)})
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
