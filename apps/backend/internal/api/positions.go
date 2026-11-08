package api

import (
	"cmp"
	"math/big"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
)

// A securities account's positions (ADR 0017): what each security's shares held cost, by average cost,
// and what its sales gained. Worked out on every read from the stored trades, holdings, and gaps.

type held struct {
	day    time.Time
	shares *big.Rat
}

type traded struct {
	day              time.Time
	sell             bool
	shares           *big.Rat
	amount, fee, tax *big.Rat
}

// costing is what a security's trades say about it, from the day it was last held at none.
type costing struct {
	since    time.Time
	cost     *big.Rat
	realized *big.Rat
}

// costOf replays a security's trades from the first day the account held none of it after its last gap,
// the longest stretch every change is seen in: a buy adds its amount and fee to the cost; a sale takes
// the average cost of the shares it sold off it, and gains its amount less fee and tax against that cost.
// Without such a day, or when the trades do not end at the newest holding, the cost is not known: never
// estimated. Realized gains count only when every sale falls after that day.
func costOf(holdings []held, gapsTo []time.Time, trades []traded) (costing, bool) {
	if len(holdings) == 0 {
		return costing{}, false
	}
	var lastGap time.Time
	for _, to := range gapsTo {
		if to.After(lastGap) {
			lastGap = to
		}
	}
	start := slices.IndexFunc(holdings, func(h held) bool { return h.shares.Sign() == 0 && !h.day.Before(lastGap) })
	if start < 0 {
		return costing{}, false
	}
	since := holdings[start].day

	shares, cost, realized := new(big.Rat), new(big.Rat), new(big.Rat)
	soldBefore := false
	for _, t := range trades {
		// A holding counts its day's trades.
		if !t.day.After(since) {
			soldBefore = soldBefore || t.sell
			continue
		}
		if !t.sell {
			shares.Add(shares, t.shares)
			cost.Add(cost, new(big.Rat).Add(t.amount, t.fee))
			continue
		}
		if shares.Cmp(t.shares) < 0 {
			return costing{}, false
		}
		sold := new(big.Rat).Mul(cost, new(big.Rat).Quo(t.shares, shares))
		proceeds := new(big.Rat).Sub(new(big.Rat).Sub(t.amount, t.fee), t.tax)
		realized.Add(realized, new(big.Rat).Sub(proceeds, sold))
		cost.Sub(cost, sold)
		shares.Sub(shares, t.shares)
	}
	if shares.Cmp(holdings[len(holdings)-1].shares) != 0 {
		return costing{}, false
	}
	result := costing{since: since, cost: cost}
	if !soldBefore {
		result.realized = realized
	}
	return result, true
}

// positions are a securities account's securities, by name: the newest holding with its worth, and its
// cost and gains where known.
func positions(trades []dbq.AccountTradesRow, history []dbq.AccountBalancesRow, gaps []dbq.AccountGapsRow, latest []Balance) []Position {
	holdings := map[string][]held{}
	for _, b := range history {
		holdings[b.Unit] = append(holdings[b.Unit], held{day: b.AsOf.Time, shares: rat(b.Amount)})
	}
	gapsTo := map[string][]time.Time{}
	for _, g := range gaps {
		gapsTo[g.Unit] = append(gapsTo[g.Unit], g.ToDate.Time)
	}
	tradesOf := map[string][]traded{}
	for _, t := range trades {
		shares := rat(t.Shares)
		tradesOf[t.Unit] = append(tradesOf[t.Unit], traded{
			day: t.TransactionDate.Time, sell: t.Kind == "sell", shares: shares.Abs(shares),
			amount: rat(t.Amount), fee: rat(t.Fee), tax: rat(t.Tax),
		})
	}
	newest := map[string]Balance{}
	for _, b := range latest {
		newest[b.Unit] = b
	}

	units := make([]string, 0, len(holdings))
	for unit := range holdings {
		units = append(units, unit)
	}
	slices.Sort(units)
	result := make([]Position, 0, len(units))
	for _, unit := range units {
		// Oldest first, as costOf replays them.
		slices.SortStableFunc(holdings[unit], func(a, b held) int { return a.day.Compare(b.day) })
		last := holdings[unit][len(holdings[unit])-1]
		position := Position{Unit: unit, Shares: last.shares.FloatString(0)}
		if b, ok := newest[unit]; ok {
			position.Code, position.Worth = b.Code, b.Worth
		}
		if c, ok := costOf(holdings[unit], gapsTo[unit], tradesOf[unit]); ok {
			position.Since = &openapi_types.Date{Time: c.since}
			position.Cost = cents(c.cost)
			if c.realized != nil {
				position.Realized = cents(c.realized)
			}
			if position.Worth != nil {
				if worth, ok := new(big.Rat).SetString(*position.Worth); ok {
					position.Gain = cents(new(big.Rat).Sub(worth, c.cost))
				}
			}
		}
		result = append(result, position)
	}
	// Held first, by worth; then what has no worth: sold out, or without a close.
	slices.SortStableFunc(result, func(a, b Position) int {
		return cmp.Or(compareWorth(b, a), cmp.Compare(a.Unit, b.Unit))
	})
	return result
}

func compareWorth(a, b Position) int {
	x, y := positionWorth(a), positionWorth(b)
	switch {
	case x == nil || y == nil:
		return cmp.Compare(boolInt(x != nil), boolInt(y != nil))
	default:
		return x.Cmp(y)
	}
}

func positionWorth(p Position) *big.Rat {
	if p.Worth == nil {
		return nil
	}
	r, ok := new(big.Rat).SetString(*p.Worth)
	if !ok {
		return nil
	}
	return r
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func rat(n pgtype.Numeric) *big.Rat {
	value, err := n.Value()
	if err != nil || value == nil {
		return new(big.Rat)
	}
	r, ok := new(big.Rat).SetString(value.(string))
	if !ok {
		return new(big.Rat)
	}
	return r
}

func cents(r *big.Rat) *Decimal {
	s := r.FloatString(2)
	return &s
}
