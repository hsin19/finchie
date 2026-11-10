package domain

import (
	"math/big"
	"slices"
	"strings"
	"time"
)

// An account's standing is worked out on every read from its balances, gaps, and valuation (ADR 0013):
// cheap at a person's volume, and never stale whatever order its sources report in.

// Balance is what an account held of a unit on a day.
type Balance struct {
	Unit   string
	AsOf   time.Time
	Amount *big.Rat
	// In the base currency at the unit's newest price; nil when the unit has none.
	Worth *big.Rat
	// Reported by a statement, rather than a snapshot (an exchange's or the owner's, ADR 0018, ADR 0019).
	FromStatement bool
}

// Valuation is what an exchange says a whole account is worth (ADR 0018).
type Valuation struct {
	AsOf time.Time
	// In the base currency; nil when the valuation's unit has no price.
	Worth *big.Rat
}

// Gap is where two consecutive balances of an account in a unit do not join up: their difference is
// money that moved with no line dated between them to show it (ADR 0013).
type Gap struct {
	AccountID int64
	Unit      string
	From, To  time.Time
	Unseen    *big.Rat
	// Some statement lists lines of the account in the unit, or lists every line whether any moved
	// (EveryLineListed). A move in a unit no statement itemizes is one no source could show: not a gap.
	Itemized bool
}

// Gaps is what an account's balances say about its continuity.
type Gaps struct {
	// Money that moved unseen.
	Unseen []Gap
	// Its balance moved in a unit no statement lists a line of, so it cannot be checked.
	Unshowable bool
}

// ReadGaps sorts gaps, of any accounts, by account.
func ReadGaps(gaps []Gap) map[int64]Gaps {
	byAccount := map[int64]Gaps{}
	for _, g := range gaps {
		read := byAccount[g.AccountID]
		if g.Itemized {
			read.Unseen = append(read.Unseen, g)
		} else {
			read.Unshowable = true
		}
		byAccount[g.AccountID] = read
	}
	return byAccount
}

// GappedAccounts counts the accounts with money that moved unseen: what the owner is told about.
func GappedAccounts(gaps []Gap) int {
	n := 0
	for _, read := range ReadGaps(gaps) {
		if len(read.Unseen) > 0 {
			n++
		}
	}
	return n
}

// Continuity is whether an account's balances can be checked against its lines.
type Continuity string

const (
	// Every balance joins the one before it through the lines between them, or a gap says where not.
	Checked Continuity = "checked"
	// Nothing could show whether money moved unseen.
	Unknown Continuity = "unknown"
)

// WorthFrom is what an account's worth was worked out from.
type WorthFrom string

const (
	FromHoldings  WorthFrom = "holdings"
	FromValuation WorthFrom = "valuation"
)

// Standing is how an account stands now.
type Standing struct {
	// Its newest balance in each unit it holds.
	Held       []Balance
	Continuity Continuity
	// In the base currency, negative for a liability; nil when it cannot be worked out.
	Worth     *big.Rat
	WorthFrom WorthFrom
	// The day of its newest report, zero when nothing has reported it.
	Newest time.Time
}

// Assess works out an account's standing from its newest balance in each unit, its gaps, and its newest
// valuation, if any:
//   - It can be checked when it is itemized, a statement reports it, and no balance moved unshowably.
//     Snapshots alone have no lines to join balances with (ADR 0018, ADR 0019).
//   - A unit at 0 is one an account that holds positions no longer holds.
//   - Its worth is its holdings' worth added up exactly; when one of them has none, its valuation on the
//     day of its newest balance; else none. An account its sources say holds nothing is worth 0. A
//     liability's worth is negative (ADR 0022).
func Assess(kind AccountKind, latest []Balance, gaps Gaps, valuation *Valuation) Standing {
	standing := Standing{Continuity: Unknown}
	fromStatement := slices.ContainsFunc(latest, func(b Balance) bool { return b.FromStatement })
	if kind.Itemized() && fromStatement && !gaps.Unshowable {
		standing.Continuity = Checked
	}

	// A snapshot lists a unit sold out at 0 on its day: the newest day counts zeros too (ADR 0018).
	var newest time.Time
	for _, b := range latest {
		if b.AsOf.After(newest) {
			newest = b.AsOf
		}
	}
	standing.Held = slices.Clone(latest)
	if kind.HoldsPositions() {
		standing.Held = slices.DeleteFunc(standing.Held, func(b Balance) bool { return b.Amount.Sign() == 0 })
	}

	switch {
	case len(latest) > 0 && len(standing.Held) == 0:
		standing.Worth, standing.WorthFrom = new(big.Rat), FromHoldings
	case holdingsWorth(standing.Held) != nil:
		standing.Worth, standing.WorthFrom = holdingsWorth(standing.Held), FromHoldings
	case valuation != nil && valuation.Worth != nil && valuation.AsOf.Equal(newest):
		standing.Worth, standing.WorthFrom = new(big.Rat).Set(valuation.Worth), FromValuation
	}
	// What a liability holds is owed (ADR 0022).
	standing.Worth = kind.Signed(standing.Worth)
	standing.Newest = newest
	return standing
}

// holdingsWorth adds up balances' worth exactly; nil when one has none, or there are none.
func holdingsWorth(balances []Balance) *big.Rat {
	if len(balances) == 0 {
		return nil
	}
	total := new(big.Rat)
	for _, b := range balances {
		if b.Worth == nil {
			return nil
		}
		total.Add(total, b.Worth)
	}
	return total
}

// FullNumber reads the full number the owner gives an account the bank prints masked: spaces and dashes
// dropped, as long as the masked one, all digits, and the same digit wherever the bank prints one (ADR
// 0016). An account no source prints a number of, such as one the owner keeps, takes any.
func FullNumber(masked, given string) (string, bool) {
	if masked == "" {
		return strings.TrimSpace(given), true
	}
	full := strings.NewReplacer(" ", "", "-", "").Replace(given)
	if len(full) != len(masked) {
		return "", false
	}
	for i := range len(full) {
		if full[i] < '0' || full[i] > '9' || (masked[i] != '*' && masked[i] != full[i]) {
			return "", false
		}
	}
	return full, true
}
