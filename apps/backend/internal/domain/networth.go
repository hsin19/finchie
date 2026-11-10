package domain

import (
	"maps"
	"math/big"
	"slices"
	"time"
)

// Bill is a card bill: what it says is owed, and when a deposit account or a later bill showed it paid
// (ADR 0016).
type Bill struct {
	ID          int64
	Institution string
	Closing     time.Time
	Currency    string
	TotalDue    *big.Rat
	PaidOn      *time.Time
}

// Owed is what the owner still owes on cards: each bank's newest bill, when it owes something and
// nothing has shown it paid. An older unpaid bill is the newer one's past: its amount is in the newer
// one's.
func Owed(bills []Bill) []Bill {
	newest := map[string]Bill{}
	for _, b := range bills {
		if seen, ok := newest[b.Institution]; !ok || b.Closing.After(seen.Closing) || (b.Closing.Equal(seen.Closing) && b.ID > seen.ID) {
			newest[b.Institution] = b
		}
	}
	var owed []Bill
	for _, institution := range slices.Sorted(maps.Keys(newest)) {
		if b := newest[institution]; b.PaidOn == nil && b.TotalDue != nil && b.TotalDue.Sign() > 0 {
			owed = append(owed, b)
		}
	}
	return owed
}

// Worth is what one account is worth in the base currency; nil when it cannot be worked out.
type Worth struct {
	Kind  AccountKind
	Worth *big.Rat
}

// KindWorth is the worth of an account kind's accounts added up.
type KindWorth struct {
	Kind AccountKind
	// Of the accounts with a worth.
	Worth *big.Rat
	// Accounts without a worth, left out of it.
	Unvalued int
}

// NetWorth is what the owner has less what they owe on cards, in the base currency.
type NetWorth struct {
	Holdings *big.Rat
	ByKind   []KindWorth
	// Accounts without a worth, left out of Holdings.
	Unvalued int
	Owed     *big.Rat
	// Bills owed in another currency than the base one, left out of Owed.
	OwedElsewhere int
	Net           *big.Rat
}

// Net adds up the accounts' worth and subtracts what bills owe (Owed).
func Net(accounts []Worth, owed []Bill) NetWorth {
	n := NetWorth{Holdings: new(big.Rat), Owed: new(big.Rat)}
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
		n.Holdings.Add(n.Holdings, a.Worth)
		k.Worth.Add(k.Worth, a.Worth)
	}
	for _, kind := range AccountKinds {
		if k := byKind[kind]; k != nil {
			n.ByKind = append(n.ByKind, *k)
		}
	}
	for _, b := range owed {
		if b.Currency != BaseCurrency {
			n.OwedElsewhere++
			continue
		}
		n.Owed.Add(n.Owed, b.TotalDue)
	}
	n.Net = new(big.Rat).Sub(n.Holdings, n.Owed)
	return n
}
