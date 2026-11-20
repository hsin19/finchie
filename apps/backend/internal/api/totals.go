package api

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
)

type tally struct {
	count  int
	amount pgtype.Numeric
}

func (t *tally) add(count int32, amount pgtype.Numeric) {
	t.count += int(count)
	t.amount = numeric.Add(t.amount, amount)
}

type summary struct {
	total     tally
	flows     []UnitFlow
	months    []MonthFlows
	byCard    []CardTotal
	byAccount []AccountTotal
}

type flow struct {
	spent, received, moved pgtype.Numeric
	movedCount             int
}

// flows is what went out, came in, and moved, per unit.
type flows map[string]*flow

func (fs flows) add(g dbq.TransactionTotalsRow) {
	if fs[g.Unit] == nil {
		fs[g.Unit] = &flow{spent: numeric.Zero(), received: numeric.Zero(), moved: numeric.Zero()}
	}
	f := fs[g.Unit]
	// A folded line is always a transfer: never spent or received, and its link is counted once, at the
	// outflow it is listed under.
	f.spent, f.received = numeric.Add(f.spent, g.Spent), numeric.Add(f.received, g.Received)
	f.moved, f.movedCount = numeric.Add(f.moved, g.Moved), f.movedCount+int(g.MovedCount)
}

func (fs flows) list() []UnitFlow {
	list := make([]UnitFlow, 0, len(fs))
	for _, unit := range slices.Sorted(maps.Keys(fs)) {
		f := fs[unit]
		list = append(list, UnitFlow{
			Unit: unit, Spent: numeric.String(f.spent), Received: numeric.String(f.received), Moved: numeric.String(f.moved), MovedCount: f.movedCount,
		})
	}
	return list
}

// summarize folds the groups from TransactionTotals into the search total, what went out, came in, and
// moved per unit, and the account picker's facets: cards and deposit accounts, which skip the picker's
// own filter but keep the kind filter; with groups split by month (TransactionTotals' by_month), the flows
// of each month too. The facets count each account's lines whether folded under a
// link's outflow or not: picking one account lists them all, since a link joins two accounts. (A bank's
// count, the sum of its accounts', counts both lines of a link within the bank.)
func summarize(groups []dbq.TransactionTotalsRow) summary {
	total := tally{amount: numeric.Zero()}
	all := flows{}
	byMonth := map[string]flows{}
	type cardKey struct{ institution, last4 string }
	cards := map[cardKey]*tally{}
	accounts := map[int64]*AccountTotal{}

	for _, g := range groups {
		if g.KindMatch && g.PickMatch && !g.Folded {
			total.add(g.RowCount, g.Amount)
		}
		// Shares or points, not money: neither spent nor received (ADR 0017, ADR 0019).
		if g.KindMatch && g.PickMatch && !g.Folded && domain.AccountKind(g.AccountKind).MovesMoney() {
			all.add(g)
			if g.Month.Valid {
				month := g.Month.Time.Format("2006-01")
				if byMonth[month] == nil {
					byMonth[month] = flows{}
				}
				byMonth[month].add(g)
			}
		}
		if !g.KindMatch {
			continue
		}
		// Cards are picked by card, the rest by account.
		if domain.AccountKind(g.AccountKind) == domain.CreditCard {
			key := cardKey{institution: g.Institution, last4: deref(g.CardLast4)}
			if cards[key] == nil {
				cards[key] = &tally{amount: numeric.Zero()}
			}
			cards[key].add(g.RowCount, g.Amount)
			continue
		}
		if accounts[g.AccountID] == nil {
			accounts[g.AccountID] = &AccountTotal{AccountId: g.AccountID, Institution: g.Institution, AccountName: g.AccountName, AccountNumber: &g.AccountNumber}
		}
		accounts[g.AccountID].Count += int(g.RowCount)
	}

	result := summary{total: total, flows: all.list(), months: make([]MonthFlows, 0, len(byMonth))}
	for _, month := range slices.Sorted(maps.Keys(byMonth)) {
		result.months = append(result.months, MonthFlows{Month: month, Flows: byMonth[month].list()})
	}
	cardOrder := slices.SortedFunc(maps.Keys(cards), func(a, b cardKey) int {
		return cmp.Or(strings.Compare(a.institution, b.institution), strings.Compare(a.last4, b.last4))
	})
	result.byCard = make([]CardTotal, 0, len(cards))
	for _, key := range cardOrder {
		t := cards[key]
		card := CardTotal{Institution: key.institution, Count: t.count, Amount: numeric.String(t.amount)}
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
		byKind = append(byKind, KindTotal{Kind: TransactionKind(row.Kind), Count: int(row.RowCount), Amount: numeric.String(row.Amount), Flow: numeric.String(row.Flow)})
	}
	return byKind
}
