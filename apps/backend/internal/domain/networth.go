package domain

import (
	"math/big"
	"time"
)

// Worth is how one account stands for net worth: its worth in the base currency, negative for a
// liability, or nil when it cannot be worked out; and whether its newest report says when it is exact.
type Worth struct {
	AccountID int64
	Kind      AccountKind
	Worth     *big.Rat
	// The day of its newest report; zero when nothing has reported it.
	Newest time.Time
	// The owner keeps it (ADR 0019): it reports when they do, which says nothing about any other day.
	ByHand bool
	// Its sources say it holds nothing.
	Empty bool
}

// KindWorth is the worth of an account kind's accounts added up.
type KindWorth struct {
	Kind AccountKind
	// Of the accounts with a worth; negative for a liability.
	Worth *big.Rat
	// Accounts without a worth, left out of it.
	Unvalued int
}

// NetWorth is what the owner has less what they owe, in the base currency, on a day (ADR 0023).
type NetWorth struct {
	Assets, Liabilities, Net *big.Rat
	ByKind                   []KindWorth
	// Accounts without a worth, left out.
	Unvalued int
	// The newest day every account has reported on or after (CompleteAsOf); zero when none has.
	CompleteAsOf time.Time
	// The accounts whose newest report is older than the day asked for.
	Behind []int64
}

// Net adds up the accounts' worth on a day: assets, liabilities (negative), and their sum.
func Net(day time.Time, accounts []Worth) NetWorth {
	n := NetWorth{Assets: new(big.Rat), Liabilities: new(big.Rat)}
	byKind := map[AccountKind]*KindWorth{}
	for _, a := range accounts {
		k := byKind[a.Kind]
		if k == nil {
			k = &KindWorth{Kind: a.Kind, Worth: new(big.Rat)}
			byKind[a.Kind] = k
		}
		if a.Worth == nil {
			n.Unvalued++
			k.Unvalued++
			continue
		}
		k.Worth.Add(k.Worth, a.Worth)
		if a.Kind.Liability() {
			n.Liabilities.Add(n.Liabilities, a.Worth)
		} else {
			n.Assets.Add(n.Assets, a.Worth)
		}
	}
	for _, kind := range AccountKinds {
		if k := byKind[kind]; k != nil {
			n.ByKind = append(n.ByKind, *k)
		}
	}
	n.Net = new(big.Rat).Add(n.Assets, n.Liabilities)
	n.CompleteAsOf = CompleteAsOf(accounts)
	for _, a := range accounts {
		if counts(a) && a.Newest.Before(day) {
			n.Behind = append(n.Behind, a.AccountID)
		}
	}
	return n
}

// CompleteAsOf is the newest day on which every account had reported (ADR 0023): sources report on days
// of their own, so a figure is exact only up to the oldest of their newest reports. Accounts the owner
// keeps, which report when they do, and accounts that hold nothing are left out.
func CompleteAsOf(accounts []Worth) time.Time {
	var complete time.Time
	for _, a := range accounts {
		if !counts(a) {
			continue
		}
		if complete.IsZero() || a.Newest.Before(complete) {
			complete = a.Newest
		}
	}
	return complete
}

func counts(a Worth) bool { return !a.ByHand && !a.Empty && !a.Newest.IsZero() }
