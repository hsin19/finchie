package domain

import (
	"cmp"
	"fmt"
	"math/big"
	"slices"
	"time"
)

// What needs the owner (AGENTS.md: quiet unless action is needed): an autopay that may not cover the card
// bills it is to take, a document whose statement does not add up or that failed, an account whose
// balances do not join up or whose source has gone quiet, and points about to lapse. Each is a notice the
// owner is told of once, worked out again from the ledger whenever it may have changed.

// FreshFor is how many days a source may take before an account's newest balance is overdue: a
// statement comes monthly, up to about three weeks after it closes (Taishin's took 16 days in 2026,
// CTBC's 20 after month end); an exchange is read every few hours (ADR 0018).
func (k AccountKind) FreshFor() int {
	if k == Exchange {
		return 1
	}
	return 50
}

// Freshness is how current an account is.
type Freshness struct {
	// The day of its newest balance it holds something in; zero when there is none.
	Newest time.Time
	// Nothing new for longer than its source takes, and no day still ahead that the owner said to look
	// again on. Why is not said: a bank sends nothing for a month without a line, which that day is for.
	Overdue bool
	// It would be overdue, but the owner said to look again on a day still ahead.
	Excused bool
}

// Fresh works out how current an account is from its newest balances in the units it holds. One the owner
// keeps is as old as they leave it, never overdue.
func Fresh(kind AccountKind, owned bool, held []Balance, checkAgainOn, today time.Time) Freshness {
	var fresh Freshness
	for _, b := range held {
		if b.AsOf.After(fresh.Newest) {
			fresh.Newest = b.AsOf
		}
	}
	if fresh.Newest.IsZero() || owned {
		return fresh
	}
	if days(fresh.Newest, today) > kind.FreshFor() {
		fresh.Excused = !checkAgainOn.IsZero() && today.Before(checkAgainOn)
		fresh.Overdue = !fresh.Excused
	}
	return fresh
}

func days(from, to time.Time) int { return int(to.Sub(from).Hours() / 24) }

// LapseSoonDays is how near points to lapse are soon enough to stand out.
const LapseSoonDays = 60

// Lapsing is points to lapse by a day: worked out by their program's rule (Estimated), or as their newest
// report says (ADR 0019).
type Lapsing struct {
	Unit      string
	Amount    *big.Rat
	By        time.Time
	Estimated bool
}

// Soon reports whether they lapse within LapseSoonDays of today.
func (l Lapsing) Soon(today time.Time) bool {
	return !l.By.After(today.AddDate(0, 0, LapseSoonDays))
}

// NextLapse is the soonest of an account's points to lapse from today on, of its lots by their program's
// rule and what its newest reports say will lapse; nil when neither has any. The soonest wins: a report
// may know of lapses the rule cannot place, points held from before the first bill. Of two on one day, the
// reported one.
func NextLapse(lots, reported []Lapsing, today time.Time) *Lapsing {
	var ahead []Lapsing
	for _, l := range lots {
		if !l.By.Before(today) {
			ahead = append(ahead, l)
		}
	}
	ahead = append(ahead, reported...)
	if len(ahead) == 0 {
		return nil
	}
	next := slices.MinFunc(ahead, func(a, b Lapsing) int {
		return cmp.Or(a.By.Compare(b.By), compareBool(a.Estimated, b.Estimated))
	})
	return &next
}

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	default:
		return -1
	}
}

// Shortfall is what a deposit account's newest balance in a unit lacks to cover the card bills its
// autopay takes after it (Debits).
type Shortfall struct {
	Unit    string
	AsOf    time.Time
	Balance *big.Rat
	// The bills, soonest due first, and what they come to.
	Debits []CardBill
	Owed   *big.Rat
	Lacks  *big.Rat
}

// Shortfalls are where an account's newest balances do not cover the card bills its autopay takes after
// them, while one of the bills is still to come out, due today or later. An estimate: what came in since
// a balance shows only in the next statement. None for an overdue account, whose balance is too old to
// tell by; it is overdue instead.
func Shortfalls(held []Balance, debits []CardBill, fresh Freshness, today time.Time) []Shortfall {
	if fresh.Overdue {
		return nil
	}
	var found []Shortfall
	for _, b := range held {
		s := Shortfall{Unit: b.Unit, AsOf: b.AsOf, Balance: b.Amount, Owed: new(big.Rat)}
		ahead := false
		for _, d := range debits {
			if d.Unit != b.Unit {
				continue
			}
			s.Debits = append(s.Debits, d)
			s.Owed.Add(s.Owed, d.TotalDue)
			ahead = ahead || (d.PaidOn.IsZero() && !d.DueDate.Before(today))
		}
		if s.Lacks = new(big.Rat).Sub(s.Owed, b.Amount); ahead && s.Lacks.Sign() > 0 {
			found = append(found, s)
		}
	}
	return found
}

// NoticeKind is what a notice is about.
type NoticeKind string

const (
	NoticeShortfall NoticeKind = "shortfall"
	NoticeDocument  NoticeKind = "document"
	NoticeGap       NoticeKind = "gap"
	NoticeOverdue   NoticeKind = "overdue"
	NoticeLapsing   NoticeKind = "lapsing"
)

// NoticeKinds are every kind, in the order to deal with them: money that may not be there in time first.
var NoticeKinds = []NoticeKind{NoticeShortfall, NoticeDocument, NoticeGap, NoticeOverdue, NoticeLapsing}

// Watched is what notices read of an account.
type Watched struct {
	ID         int64
	Fresh      Freshness
	Gaps       []Gap
	NextLapse  *Lapsing
	Shortfalls []Shortfall
}

// TroubledDocument is a document whose statement does not add up, or that failed to parse.
type TroubledDocument struct {
	ID       int64
	Filename string
	Status   DocumentStatus
	Failure  *Failure
}

// Notice is one thing that needs the owner. Its key names the situation: while it lasts, the key is the
// same, and a new key is news, such as a newer balance that still does not cover a bill, or a later bill.
// Exactly one of its details is set, by its kind.
type Notice struct {
	Kind      NoticeKind
	Key       string
	AccountID int64
	Shortfall *Shortfall
	Document  *TroubledDocument
	Gap       *Gap
	// The day of the newest balance of an overdue account.
	Newest  time.Time
	Lapsing *Lapsing
}

// Notices is everything that needs the owner, in the order of NoticeKinds.
func Notices(accounts []Watched, documents []TroubledDocument, today time.Time) []Notice {
	var found []Notice
	for _, a := range accounts {
		for _, s := range a.Shortfalls {
			last := s.Debits[len(s.Debits)-1].DueDate
			found = append(found, Notice{
				Kind: NoticeShortfall, AccountID: a.ID, Shortfall: &s,
				Key: fmt.Sprintf("shortfall:%d:%s:%s:%s", a.ID, s.Unit, day(s.AsOf), day(last)),
			})
		}
	}
	for _, d := range documents {
		found = append(found, Notice{Kind: NoticeDocument, Document: &d, Key: fmt.Sprintf("document:%d:%s", d.ID, d.Status)})
	}
	for _, a := range accounts {
		for _, g := range a.Gaps {
			found = append(found, Notice{
				Kind: NoticeGap, AccountID: a.ID, Gap: &g,
				Key: fmt.Sprintf("gap:%d:%s:%s:%s", a.ID, g.Unit, day(g.From), day(g.To)),
			})
		}
	}
	for _, a := range accounts {
		if a.Fresh.Overdue {
			found = append(found, Notice{
				Kind: NoticeOverdue, AccountID: a.ID, Newest: a.Fresh.Newest,
				Key: fmt.Sprintf("overdue:%d:%s", a.ID, day(a.Fresh.Newest)),
			})
		}
	}
	for _, a := range accounts {
		if l := a.NextLapse; l != nil && l.Soon(today) {
			found = append(found, Notice{
				Kind: NoticeLapsing, AccountID: a.ID, Lapsing: l,
				Key: fmt.Sprintf("lapsing:%d:%s:%s", a.ID, l.Unit, day(l.By)),
			})
		}
	}
	return found
}

func day(t time.Time) string { return t.Format(time.DateOnly) }
