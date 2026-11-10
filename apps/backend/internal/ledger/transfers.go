package ledger

import (
	"cmp"
	"context"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
)

// Money that moves between two of the owner's lines is linked (ADR 0016): a deposit account paying a
// card bill, one account paying into another, or one currency exchanged for another. Banks print no
// such link either, so like parents it is worked out from amounts, dates, the counterparties they print,
// and the reference a bank prints on both sides of an exchange.

const (
	// A transfer's two lines are at most this many days apart: banks post them on different days.
	transferWindowDays = 3
	// A bill counts as paid by a payment this many days past its due date at the latest.
	paidGraceDays = 5
	// A counterparty fits an account when at least this many digits both print agree.
	minAgreeingDigits = 4
)

// bankCodes are the institutions' codes (金融機構代號), by which a counterparty names its bank.
var bankCodes = map[string]string{
	"esun":     "808",
	"taishin":  "812",
	"ctbc":     "822",
	"fubon":    "012",
	"nextbank": "823",
	"cathay":   "013",
	"hsbc":     "081",
}

type transferLine = dbq.TransferLinesRow

// retransfer works out every transfer link and every bill's payment again, in the caller's
// transaction, under the lock Relink holds.
func retransfer(ctx context.Context, queries *dbq.Queries) error {
	lines, err := queries.TransferLines(ctx)
	if err != nil {
		return fmt.Errorf("list lines to transfer: %w", err)
	}
	bills, err := queries.CardBills(ctx)
	if err != nil {
		return fmt.Errorf("list card bills: %w", err)
	}
	if err := queries.ClearTransfers(ctx); err != nil {
		return fmt.Errorf("clear transfers: %w", err)
	}
	if err := queries.ClearPaid(ctx); err != nil {
		return fmt.Errorf("clear payments: %w", err)
	}

	pairs := matchTransfers(lines)
	if len(pairs) > 0 {
		params := dbq.SetCounterpartsParams{}
		for id, counterpart := range pairs {
			params.Ids = append(params.Ids, id)
			params.CounterpartIds = append(params.CounterpartIds, counterpart)
		}
		if err := queries.SetCounterparts(ctx, params); err != nil {
			return fmt.Errorf("store transfers: %w", err)
		}
	}

	paid := matchPaid(bills, lines)
	if len(paid) > 0 {
		params := dbq.SetPaidParams{}
		for id, payment := range paid {
			params.Ids = append(params.Ids, id)
			params.PaidOn = append(params.PaidOn, payment.on)
			params.PaidBy = append(params.PaidBy, payment.by)
		}
		if err := queries.SetPaid(ctx, params); err != nil {
			return fmt.Errorf("store payments: %w", err)
		}
	}
	return nil
}

// matchTransfers returns line ID → counterpart line ID, both ways. Each line is in one link at most:
// nearer days pair first, then card payments before own transfers and exchanges.
func matchTransfers(lines []transferLine) map[int64]int64 {
	var outs, ins, payments []transferLine
	for _, l := range lines {
		switch {
		case l.AccountKind == domain.Deposit && amount(l.Amount).Sign() < 0:
			outs = append(outs, l)
		case l.AccountKind == domain.Deposit && amount(l.Amount).Sign() > 0:
			ins = append(ins, l)
		case l.Kind == domain.Payment:
			payments = append(payments, l)
		}
	}

	type candidate struct {
		out, other int64
		days       int
		card       bool
	}
	var candidates []candidate
	for _, out := range outs {
		// A card payment line is credited as the outflow is debited: both negative.
		for _, p := range payments {
			if d := daysApart(out.TransactionDate, p.TransactionDate); d <= transferWindowDays && out.Unit == p.Unit && amount(out.Amount).Cmp(amount(p.Amount)) == 0 {
				candidates = append(candidates, candidate{out.ID, p.ID, d, true})
			}
		}
		for _, in := range ins {
			if exchanged(out, in) {
				candidates = append(candidates, candidate{out.ID, in.ID, 0, false})
				continue
			}
			if in.AccountID == out.AccountID || in.Unit != out.Unit || new(big.Rat).Neg(amount(in.Amount)).Cmp(amount(out.Amount)) != 0 {
				continue
			}
			if d := daysApart(out.TransactionDate, in.TransactionDate); d <= transferWindowDays && (fits(out.Counterparty, out.Institution, in) || fits(in.Counterparty, in.Institution, out)) {
				candidates = append(candidates, candidate{out.ID, in.ID, d, false})
			}
		}
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		return cmp.Or(cmp.Compare(a.days, b.days), compareBool(b.card, a.card), cmp.Compare(a.out, b.out), cmp.Compare(a.other, b.other))
	})

	pairs := map[int64]int64{}
	for _, c := range candidates {
		if _, taken := pairs[c.out]; taken {
			continue
		}
		if _, taken := pairs[c.other]; taken {
			continue
		}
		pairs[c.out], pairs[c.other] = c.other, c.out
	}
	return pairs
}

// exchanged reports whether an outflow and an inflow are one exchange of currencies: at the same bank,
// on the same day, in two units, with the same memo, which carries the reference the bank prints on both
// sides (Next Bank's 新臺幣換美元;@31.5000;FT…). The amounts differ by the rate, so they prove nothing.
func exchanged(out, in transferLine) bool {
	return out.Unit != in.Unit && out.Institution == in.Institution && daysApart(out.TransactionDate, in.TransactionDate) == 0 &&
		out.Memo != nil && in.Memo != nil && strings.TrimSpace(*out.Memo) != "" && *out.Memo == *in.Memo
}

type payment struct {
	on pgtype.Date
	// The line that showed it; 0 for a bill that owes nothing.
	by int64
}

// matchPaid returns statement ID → when and by which line each card bill was paid: an outflow of its
// total due from a deposit account, else a later bill of its bank showing a payment of it, between its
// closing and a few days past its due date. A line pays one bill at most.
func matchPaid(bills []dbq.CardBillsRow, lines []transferLine) map[int64]payment {
	paid := map[int64]payment{}
	used := map[int64]bool{}
	for _, bill := range bills {
		if !bill.TotalDue.Valid || !bill.ClosingDate.Valid || !bill.DueDate.Valid {
			continue
		}
		due := amount(bill.TotalDue)
		if due.Sign() <= 0 {
			paid[bill.ID] = payment{on: bill.ClosingDate}
			continue
		}
		last := bill.DueDate.Time.AddDate(0, 0, paidGraceDays)
		within := func(l transferLine) bool {
			at := l.TransactionDate.Time
			return !used[l.ID] && (bill.Currency == nil || l.Unit == *bill.Currency) &&
				at.After(bill.ClosingDate.Time) && !at.After(last) && new(big.Rat).Neg(amount(l.Amount)).Cmp(due) == 0
		}
		var fromDeposit, fromBill *transferLine
		for i := range lines {
			l := &lines[i]
			if !within(*l) {
				continue
			}
			switch {
			case l.AccountKind == domain.Deposit && (fromDeposit == nil || l.TransactionDate.Time.Before(fromDeposit.TransactionDate.Time)):
				fromDeposit = l
			case l.Kind == domain.Payment && l.Institution == bill.Institution && l.StatementID != bill.ID &&
				(fromBill == nil || l.TransactionDate.Time.Before(fromBill.TransactionDate.Time)):
				fromBill = l
			}
		}
		by := cmp.Or(fromDeposit, fromBill)
		if by == nil {
			continue
		}
		used[by.ID] = true
		paid[bill.ID] = payment{on: by.TransactionDate, by: by.ID}
	}
	return paid
}

// fits reports whether a counterparty, as a line of `own`'s bank prints it, is the account: the same
// bank, and its digits agree with the account's number, or its full number where the owner gave one.
func fits(counterparty *string, own string, account transferLine) bool {
	if counterparty == nil {
		return false
	}
	code, number, ok := splitCounterparty(*counterparty, own)
	if !ok || code != bankCodes[account.Institution] {
		return false
	}
	target := account.Number
	if account.FullNumber != nil && *account.FullNumber != "" {
		target = *account.FullNumber
	}
	return DigitsFit(number, target, minAgreeingDigits)
}

// splitCounterparty reads "812 0087654321***234" as a bank code and an account; a lone account number
// is at the line's own bank.
func splitCounterparty(s, own string) (code, number string, ok bool) {
	fields := strings.Fields(s)
	switch {
	case len(fields) == 2 && len(fields[0]) == 3 && strings.IndexFunc(fields[0], func(r rune) bool { return !unicode.IsDigit(r) }) < 0:
		return fields[0], fields[1], true
	case len(fields) == 1:
		return bankCodes[own], fields[0], bankCodes[own] != ""
	default:
		return "", "", false
	}
}

// DigitsFit compares two account numbers as banks print them, masked or not: right-aligned with
// leading zeros dropped, no digit both print may differ, and at least `least` must agree.
func DigitsFit(a, b string, least int) bool {
	a, b = accountDigits(a), accountDigits(b)
	agree := 0
	for i := 1; i <= min(len(a), len(b)); i++ {
		x, y := a[len(a)-i], b[len(b)-i]
		if x == '*' || y == '*' {
			continue
		}
		if x != y {
			return false
		}
		agree++
	}
	return agree >= least
}

// accountDigits keeps digits and mask stars, without leading zeros: "0087654321***234" → "87654321***234".
func accountDigits(s string) string {
	kept := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '*' {
			return r
		}
		return -1
	}, s)
	return strings.TrimLeft(kept, "0")
}

func daysApart(a, b pgtype.Date) int {
	d := a.Time.Sub(b.Time)
	if d < 0 {
		d = -d
	}
	return int(d / (24 * time.Hour))
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
