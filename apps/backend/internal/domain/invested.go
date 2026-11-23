package domain

import (
	"math/big"
	"slices"
	"time"
)

// Invests reports whether an account's worth is put to work in a market: shares and coins (ADR 0017,
// ADR 0018), whose return is what they are worth less what went into them.
func (k AccountKind) Invests() bool { return k == Securities || k == Exchange }

// InvestedFromStart reports whether every flow into and out of an account of the kind is known, so that
// what went into it is their sum: an exchange's deposits and withdrawals are read from two years back
// (ADR 0018), and an account is taken to hold nothing before the first of them. A securities account's
// holdings may be older than its oldest statement, and its money older than Finchie's prices.
func (k AccountKind) InvestedFromStart() bool { return k == Exchange }

// Flow is money that crossed into an investment account (positive) or out of it (negative) on a day, in
// the base currency: a trade's money paid or received, which the settlement account outside holds; an
// exchange's deposit or withdrawal, whatever the other end. Its worth is nil when its unit had no price.
//
// Each account is a boundary of its own: coins sent from one exchange to another leave one and enter the
// other, and the two cancel out.
type Flow struct {
	AccountID int64
	Day       time.Time
	Worth     *big.Rat
}

// Invested is how the investments stand on a day.
type Invested struct {
	Day time.Time
	// What the opened accounts with a worth on the day are worth.
	Worth *big.Rat
	// What went into those same accounts (InvestedOver).
	Invested *big.Rat
	// Worth less Invested.
	Gain *big.Rat
	// Gain over Invested; nil while nothing net is in.
	Return *big.Rat
	// Opened accounts without a worth on the day, left out of both; flows that count without a price,
	// left out of Invested.
	Unvalued, Unpriced int
	// Every opened account had reported by the day (CompleteAsOf): the figure is exact.
	Complete bool
	// Opened accounts whose newest report is older than the day.
	Behind []int64
}

// InvestedOver works out the investments on each day of a series, oldest first, from every account's
// worth on each day (worths[i] is days[i]'s) and every flow up to the last day.
//
// Each account opens on the first day of the series it has a worth. What went into an exchange is every
// deposit and withdrawal it ever reported (InvestedFromStart). What went into a securities account before
// Finchie saw it is unknown: holdings older than the oldest statement, prices before Finchie started. So
// what it was worth the day it opened counts as put in that day, and from then on its trades' money; its
// return is the one over the series, without guessing a cost (ADR 0017), and one that opens later brings
// no gain with it. An account the owner keeps by hand has no flows to tell what went in from what it made,
// so it is left out. The series starts on the first day an account opens.
func InvestedOver(days []time.Time, worths [][]Worth, flows []Flow) []Invested {
	flows = slices.SortedStableFunc(slices.Values(flows), func(a, b Flow) int { return a.Day.Compare(b.Day) })
	opened := map[int64]bool{}
	// What went into each account: before it opens, its flows so far.
	in := map[int64]*big.Rat{}
	before := map[int64]int{}
	unpriced, next := 0, 0
	var series []Invested
	for i, day := range days {
		accounts := worths[i]
		for ; next < len(flows) && !flows[next].Day.After(day); next++ {
			f := flows[next]
			if in[f.AccountID] == nil {
				in[f.AccountID] = new(big.Rat)
			}
			switch {
			case f.Worth != nil:
				in[f.AccountID].Add(in[f.AccountID], f.Worth)
			case opened[f.AccountID]:
				unpriced++
			default:
				before[f.AccountID]++
			}
		}

		var held []Worth
		for _, w := range accounts {
			if !w.Kind.Invests() || w.ByHand {
				continue
			}
			if !opened[w.AccountID] {
				if w.Worth == nil {
					continue
				}
				opened[w.AccountID] = true
				if w.Kind.InvestedFromStart() {
					if in[w.AccountID] == nil {
						in[w.AccountID] = new(big.Rat)
					}
					unpriced += before[w.AccountID]
				} else {
					// Up to the day it opens, its flows are in what it is worth then.
					in[w.AccountID] = new(big.Rat).Set(w.Worth)
				}
			}
			held = append(held, w)
		}
		if len(opened) == 0 {
			continue
		}

		point := Invested{Day: day, Worth: new(big.Rat), Invested: new(big.Rat), Unpriced: unpriced, Unvalued: len(opened) - len(held)}
		for _, w := range held {
			if w.Worth == nil {
				point.Unvalued++
				continue
			}
			point.Worth.Add(point.Worth, w.Worth)
			point.Invested.Add(point.Invested, in[w.AccountID])
		}
		point.Gain = new(big.Rat).Sub(point.Worth, point.Invested)
		if point.Invested.Sign() > 0 {
			point.Return = new(big.Rat).Quo(point.Gain, point.Invested)
		}
		complete := CompleteAsOf(held)
		point.Complete = !complete.IsZero() && !day.After(complete)
		point.Behind = Behind(day, held)
		series = append(series, point)
	}
	return series
}
