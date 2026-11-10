// Package domain is Finchie's model: the kinds of accounts, units, lines, and documents, and the rules
// that read them, such as whether an account's balances can be checked or what it is worth. It knows
// nothing of the database, HTTP, the parser, or jobs; everything else depends on it, never the reverse.
package domain

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// AccountKind is what an account is (ADR 0013), as stored in accounts.kind and statements.kind: a card
// bill reports a credit card account, a deposit statement deposit accounts, a securities statement a
// brokerage account (ADR 0017), and a card bill the bank's points too (ADR 0019). An exchange account has
// no statements: its API reports snapshots (ADR 0018).
type AccountKind string

const (
	CreditCard AccountKind = "credit_card"
	Deposit    AccountKind = "deposit"
	Securities AccountKind = "securities"
	Exchange   AccountKind = "exchange"
	Points     AccountKind = "points"
)

// Scan reads a kind from the database, where an account a query outer-joins may be missing: NULL is no
// kind.
func (k *AccountKind) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*k = ""
	case string:
		*k = AccountKind(v)
	case []byte:
		*k = AccountKind(v)
	default:
		return fmt.Errorf("account kind: cannot read %T", src)
	}
	return nil
}

// AccountKinds are every kind, in the order accounts are listed.
var AccountKinds = []AccountKind{Deposit, Securities, Exchange, Points, CreditCard}

// UnitKind is the kind of the units an account of this kind holds: a securities account securities, a
// points account points, every other money.
func (k AccountKind) UnitKind() UnitKind {
	switch k {
	case Securities:
		return Security
	case Points:
		return PointsUnit
	default:
		return Money
	}
}

// OwnersAccount reports whether the owner has the account. A credit_card account is the container for a
// bank's card lines, which owe rather than hold: what a bill says is owed stays on the bill (ADR 0013).
func (k AccountKind) OwnersAccount() bool { return k != CreditCard }

// Itemized reports whether the account's statements list the lines that move its balances, so that two
// balances can be joined by the lines between them (ADR 0013). A card bill dates its lines by purchase,
// which can fall before the previous bill closed; an exchange reports what it holds and nothing else.
func (k AccountKind) Itemized() bool { return k == Deposit || k == Securities || k == Points }

// EveryLineListed reports whether a statement lists every line of the account in each of its units,
// whether it ever moved or not: every trade, every points movement. A deposit statement may print a
// unit's balance alone (CTBC's passbook savings, 活儲有摺).
func (k AccountKind) EveryLineListed() bool { return k == Securities || k == Points }

// KindsWhere are the kinds for which a rule holds, for asking the database about them.
func KindsWhere(rule func(AccountKind) bool) []string {
	var kinds []string
	for _, k := range AccountKinds {
		if rule(k) {
			kinds = append(kinds, string(k))
		}
	}
	return kinds
}

// HoldsPositions reports whether a unit at 0 is one the account no longer holds: a security, a coin, a
// points program (ADR 0017, 0018, 0019). A deposit account at 0 still has its currency.
func (k AccountKind) HoldsPositions() bool { return k == Securities || k == Exchange || k == Points }

// MovesMoney reports whether the account's lines are money spent or received: shares and points are not
// (ADR 0017, ADR 0019).
func (k AccountKind) MovesMoney() bool { return k.UnitKind() == Money }

// OwnerInstitution is the institution of an account the owner opened and keeps by hand: they are its only
// source (ADR 0019). Such an account is found by its id, not by institution, kind, and number.
const OwnerInstitution = "owner"

// UnitKind is what a unit counts (ADR 0020), as stored in units.kind.
type UnitKind string

const (
	Money      UnitKind = "money"
	Security   UnitKind = "security"
	PointsUnit UnitKind = "points"
)

// BaseCurrency is what every worth is in. It is worth itself and has no price of its own.
const BaseCurrency = "TWD"

var spaces = regexp.MustCompile(`\s+`)

// Symbol is how units of the kind are spelled, whatever a source prints: money as an upper-case code or
// ticker, a security by its name with full-width letters and digits made ASCII and runs of whitespace one
// space (the parser's clean), points by the program's name as printed. Every unit is found by its symbol
// so spelled; a source that spelled one differently would create a unit no price reaches.
func Symbol(kind UnitKind, s string) string {
	switch kind {
	case Money:
		return strings.ToUpper(strings.TrimSpace(s))
	case Security:
		return strings.TrimSpace(spaces.ReplaceAllString(norm.NFKC.String(s), " "))
	default:
		return strings.TrimSpace(s)
	}
}

// LineKind is what a line is, as stored in transactions.kind: a card line, a deposit line, a trade, or a
// points movement. Which ones an account has follows from its kind.
type LineKind string

const (
	// Card lines.
	Purchase              LineKind = "purchase"
	ForeignTransactionFee LineKind = "foreign_transaction_fee"
	AnnualFee             LineKind = "annual_fee"
	Refund                LineKind = "refund"
	Rebate                LineKind = "rebate"
	Payment               LineKind = "payment"
	// Deposit lines.
	Interest LineKind = "interest"
	Fee      LineKind = "fee"
	Other    LineKind = "other"
	// Trades (ADR 0017).
	Buy  LineKind = "buy"
	Sell LineKind = "sell"
	// Points (ADR 0019).
	Earn   LineKind = "earn"
	Adjust LineKind = "adjust"
	Redeem LineKind = "redeem"
)

// PriceSource is where a price came from (ADR 0020), as stored in prices.source: for showing it and for
// clearing the owner's, never for picking which one counts.
type PriceSource string

const (
	SourceRates   PriceSource = "fawaz"
	SourceTWSE    PriceSource = "twse"
	SourceTPEx    PriceSource = "tpex"
	SourceMAX     PriceSource = "max"
	SourceBinance PriceSource = "binance"
	SourceBitget  PriceSource = "bitget"
	SourceOwner   PriceSource = "owner"
)
