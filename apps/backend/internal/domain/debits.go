package domain

import (
	"cmp"
	"math/big"
	"slices"
	"time"
)

// PaidGraceDays is how many days past its due date a payment can still pay a card bill (ADR 0016): an
// autopay due on a holiday is posted on the next working day.
const PaidGraceDays = 5

// CardBill is a card bill as far as paying it goes (ADR 0016).
type CardBill struct {
	ID          int64
	Institution string
	ClosingDate time.Time
	DueDate     time.Time
	// The bill's currency.
	Unit     string
	TotalDue *big.Rat
	// When a deposit account or a later bill showed it paid; zero while that is not known.
	PaidOn time.Time
	// The deposit account its payment left, where a line of one showed it paid; else 0.
	PaidFrom int64
}

// AccountUnit names an account's holding of one unit.
type AccountUnit struct {
	AccountID int64
	Unit      string
}

// Debits works out, for each deposit account, the card bills whose money leaves it after its newest
// balance in the bill's unit (`newest`), soonest due first, each with PaidFrom set to the account. A
// deposit account's newest balance is a month-end's, three weeks old by the time a bill is due: what
// the bills since take from it tells whether its autopay can go through.
//
// Nothing is set up: a bank's bills leave the account that the newest of them a deposit line showed
// paid left, as an autopay does month after month.
//   - A bank's newer bill that only a later bill showed paid, on a day that account's newest balance
//     already covers without the payment, was paid from somewhere else: the bank's bills then leave no
//     account Finchie knows of. Paid on a later day, its money left after that balance.
//   - A bill not known to be paid leaves after that balance unless the balance is past its due date and
//     the grace after it: what a deposit statement would have shown by then is no debit to come.
//   - A bill that owes nothing leaves nothing.
func Debits(bills []CardBill, newest map[AccountUnit]time.Time) map[int64][]CardBill {
	byBank := map[string][]CardBill{}
	for _, b := range bills {
		if b.TotalDue == nil || b.TotalDue.Sign() <= 0 {
			continue
		}
		byBank[b.Institution] = append(byBank[b.Institution], b)
	}

	debits := map[int64][]CardBill{}
	for _, owed := range byBank {
		slices.SortFunc(owed, func(a, b CardBill) int {
			return cmp.Or(a.ClosingDate.Compare(b.ClosingDate), cmp.Compare(a.ID, b.ID))
		})
		for _, b := range bankDebits(owed, newest) {
			debits[b.PaidFrom] = append(debits[b.PaidFrom], b)
		}
	}
	for _, bills := range debits {
		slices.SortFunc(bills, func(a, b CardBill) int {
			return cmp.Or(a.DueDate.Compare(b.DueDate), cmp.Compare(a.ID, b.ID))
		})
	}
	return debits
}

// bankDebits is Debits for one bank's bills that owe something, oldest first.
func bankDebits(owed []CardBill, newest map[AccountUnit]time.Time) []CardBill {
	last := -1
	for i, b := range owed {
		if b.PaidFrom != 0 {
			last = i
		}
	}
	if last < 0 {
		return nil
	}
	account := owed[last].PaidFrom

	var found []CardBill
	for i, b := range owed {
		balance, held := newest[AccountUnit{AccountID: account, Unit: b.Unit}]
		switch {
		case b.PaidFrom != 0 || !held:
			// A payment a deposit line showed is gone in that statement's balance already.
			continue
		case !b.PaidOn.IsZero():
			if i < last {
				continue
			}
			if !b.PaidOn.After(balance) {
				return nil
			}
		case balance.After(b.DueDate.AddDate(0, 0, PaidGraceDays)):
			continue
		}
		b.PaidFrom = account
		found = append(found, b)
	}
	return found
}
