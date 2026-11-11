package domain

import (
	"cmp"
	"maps"
	"math/big"
	"slices"
	"time"
)

// A securities account's positions (ADR 0017): what each security's shares held cost, by average cost,
// and what its sales gained. Worked out on every read from the stored trades, holdings, and gaps.

// Holding is how many shares of a security an account held on a day, as a statement reported.
type Holding struct {
	Day    time.Time
	Shares *big.Rat
}

// Trade is a buy or a sale of a security, its shares positive either way.
type Trade struct {
	Day              time.Time
	Sell             bool
	Shares           *big.Rat
	Amount, Fee, Tax *big.Rat
}

// Position is what an account holds or held of one security.
type Position struct {
	Unit string
	// Its newest holding.
	Shares *big.Rat
	// In the base currency at its newest price; nil without one, or when sold out.
	Worth *big.Rat
	// Known only from a day the account held none of it with every change seen since (costOf): the cost of
	// the shares held, what sales since gained, and Worth less Cost. Nil when not known.
	Since          *time.Time
	Cost, Realized *big.Rat
	Gain           *big.Rat
}

// Positions are a securities account's securities, by name: held first, by worth; then what has no worth,
// sold out or without a price. Trades are taken by security; worth by security, of what it holds now.
func Positions(history map[string][]Holding, gapsTo map[string][]time.Time, trades map[string][]Trade, worth map[string]*big.Rat) []Position {
	result := make([]Position, 0, len(history))
	for _, unit := range slices.Sorted(maps.Keys(history)) {
		// Oldest first, as costOf replays them.
		holdings := slices.Clone(history[unit])
		slices.SortStableFunc(holdings, func(a, b Holding) int { return a.Day.Compare(b.Day) })
		position := Position{Unit: unit, Shares: holdings[len(holdings)-1].Shares, Worth: worth[unit]}
		if c, ok := costOf(holdings, gapsTo[unit], trades[unit]); ok {
			position.Since, position.Cost, position.Realized = &c.since, c.cost, c.realized
			if position.Worth != nil {
				position.Gain = new(big.Rat).Sub(position.Worth, c.cost)
			}
		}
		result = append(result, position)
	}
	slices.SortStableFunc(result, func(a, b Position) int {
		return cmp.Or(compareWorth(b.Worth, a.Worth), cmp.Compare(a.Unit, b.Unit))
	})
	return result
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
func costOf(holdings []Holding, gapsTo []time.Time, trades []Trade) (costing, bool) {
	if len(holdings) == 0 {
		return costing{}, false
	}
	var lastGap time.Time
	for _, to := range gapsTo {
		if to.After(lastGap) {
			lastGap = to
		}
	}
	start := slices.IndexFunc(holdings, func(h Holding) bool { return h.Shares.Sign() == 0 && !h.Day.Before(lastGap) })
	if start < 0 {
		return costing{}, false
	}
	since := holdings[start].Day

	shares, cost, realized := new(big.Rat), new(big.Rat), new(big.Rat)
	soldBefore := false
	for _, t := range trades {
		// A holding counts its day's trades.
		if !t.Day.After(since) {
			soldBefore = soldBefore || t.Sell
			continue
		}
		if !t.Sell {
			shares.Add(shares, t.Shares)
			cost.Add(cost, new(big.Rat).Add(t.Amount, t.Fee))
			continue
		}
		if shares.Cmp(t.Shares) < 0 {
			return costing{}, false
		}
		sold := new(big.Rat).Mul(cost, new(big.Rat).Quo(t.Shares, shares))
		proceeds := new(big.Rat).Sub(new(big.Rat).Sub(t.Amount, t.Fee), t.Tax)
		realized.Add(realized, new(big.Rat).Sub(proceeds, sold))
		cost.Sub(cost, sold)
		shares.Sub(shares, t.Shares)
	}
	if shares.Cmp(holdings[len(holdings)-1].Shares) != 0 {
		return costing{}, false
	}
	result := costing{since: since, cost: cost}
	if !soldBefore {
		result.realized = realized
	}
	return result, true
}

// compareWorth orders worths, none before any.
func compareWorth(a, b *big.Rat) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	default:
		return a.Cmp(b)
	}
}
