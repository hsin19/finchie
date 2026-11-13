package domain

import (
	"math/big"
	"time"
)

// Points lapse by their program's terms (ADR 0019). What each bill adds is a lot, dated on its closing
// day, which lapses by the day the program's rule gives it. Points used take the lots that lapse soonest
// first, as the banks' terms say; points that vanish between two bills with no line to show it are what
// lapsed. Worked out on every read from the bills, never stored, so a rule corrected later applies to
// every bill.

// LapseRule is how a program's points lapse, as its parser reports it; the zero value is none known.
type LapseRule struct {
	// At the end of the month this many months after a bill's closing month.
	MonthsAfterClosing int
	// Once a year: what the bills closing up to month PurgeLastMonth (1 to 12) add lapse together at the
	// end of the month after it, PurgeYears later.
	PurgeLastMonth, PurgeYears int
}

// Known reports whether the rule says anything.
func (r LapseRule) Known() bool {
	return r.MonthsAfterClosing > 0 || (r.PurgeLastMonth > 0 && r.PurgeYears > 0)
}

// LapsesBy is the day the points a bill closing on `closing` adds lapse by.
func (r LapseRule) LapsesBy(closing time.Time) time.Time {
	if r.MonthsAfterClosing > 0 {
		return monthEnd(closing.Year(), closing.Month()+time.Month(r.MonthsAfterClosing))
	}
	year := closing.Year()
	if int(closing.Month()) > r.PurgeLastMonth {
		year++ // past this year's last month: next year's points
	}
	return monthEnd(year+r.PurgeYears, time.Month(r.PurgeLastMonth+1))
}

// monthEnd is the last day of a month; months past December run on into the years after.
func monthEnd(year int, month time.Month) time.Time {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC)
}

// PointsBill is what one bill reports of a program: its closing day, what its lines added and used (both
// positive), and the balance it closed on.
type PointsBill struct {
	Closing     time.Time
	Added, Used *big.Rat
	Balance     *big.Rat
}

// Lot is points that lapse together: what one bill added, or what the first bill found held already
// (Unaged), added before it on days not known, so lapsing no later than By.
type Lot struct {
	By     time.Time
	Amount *big.Rat
	Unaged bool
}

// Lapse is points that lapsed between two bills, as the rule says: the two balances' days, and how many.
type Lapse struct {
	From, To time.Time
	Amount   *big.Rat
}

// Lots is what a program's bills come to: the lots it still holds, soonest to lapse first, which add up to
// its newest balance, and what lapsed between bills.
type Lots struct {
	Held   []Lot
	Lapsed []Lapse
}

// WorkOutLots follows a program's bills, oldest first, under its rule. Points that vanish between two
// bills are taken as lapsed from what the first bill found held, then from the lots the rule says lapsed
// by the later bill; what is left of them is no lapse the rule explains, a gap, and is taken from the
// soonest lots all the same, so that the lots keep adding up to the balance. Points the balance gained
// with no line to show it are a lot of their own.
func WorkOutLots(bills []PointsBill, rule LapseRule) Lots {
	var lots Lots
	var held []Lot
	var previous time.Time // the bill before's closing; zero for the first
	for _, bill := range bills {
		by := rule.LapsesBy(bill.Closing)
		if previous.IsZero() {
			opening := new(big.Rat).Sub(new(big.Rat).Add(bill.Balance, bill.Used), bill.Added)
			if opening.Sign() > 0 {
				held = append(held, Lot{By: by, Amount: opening, Unaged: true})
			}
		}
		if bill.Added.Sign() > 0 {
			held = append(held, Lot{By: by, Amount: new(big.Rat).Set(bill.Added)})
		}
		take(held, bill.Used, func(Lot) bool { return true })
		if !previous.IsZero() {
			gone := new(big.Rat).Sub(total(held), bill.Balance)
			switch gone.Sign() {
			case 1:
				explained := take(held, gone, func(l Lot) bool { return l.Unaged || !l.By.After(bill.Closing) })
				if explained.Sign() > 0 {
					lots.Lapsed = append(lots.Lapsed, Lapse{From: previous, To: bill.Closing, Amount: explained})
				}
				take(held, gone.Sub(gone, explained), func(Lot) bool { return true })
			case -1:
				held = append(held, Lot{By: by, Amount: gone.Neg(gone)})
			}
		}
		held = nonEmpty(held)
		previous = bill.Closing
	}
	lots.Held = held
	return lots
}

// take takes up to `amount` from the lots that `may` allows, soonest first, and returns how much it took.
func take(lots []Lot, amount *big.Rat, may func(Lot) bool) *big.Rat {
	left := new(big.Rat).Set(amount)
	for i := range lots {
		if left.Sign() <= 0 {
			break
		}
		if !may(lots[i]) || lots[i].Amount.Sign() <= 0 {
			continue
		}
		taken := lots[i].Amount
		if taken.Cmp(left) > 0 {
			taken = left
		}
		taken = new(big.Rat).Set(taken)
		lots[i].Amount = new(big.Rat).Sub(lots[i].Amount, taken)
		left.Sub(left, taken)
	}
	return new(big.Rat).Sub(amount, left)
}

func total(lots []Lot) *big.Rat {
	sum := new(big.Rat)
	for _, l := range lots {
		sum.Add(sum, l.Amount)
	}
	return sum
}

func nonEmpty(lots []Lot) []Lot {
	kept := lots[:0]
	for _, l := range lots {
		if l.Amount.Sign() > 0 {
			kept = append(kept, l)
		}
	}
	return kept
}
