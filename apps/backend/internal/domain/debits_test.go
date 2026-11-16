package domain

import (
	"slices"
	"testing"
	"time"
)

// Synthetic figures (ADR 0006).

const savings, other = 7, 8

func cardBill(id int64, bank, closing, due, owed string) CardBill {
	return CardBill{ID: id, Institution: bank, ClosingDate: on(closing), DueDate: on(due), Unit: "TWD", TotalDue: r(owed)}
}

func paidFrom(b CardBill, day string, account int64) CardBill {
	b.PaidOn, b.PaidFrom = on(day), account
	return b
}

func debitIDs(debits map[int64][]CardBill, account int64) []int64 {
	var ids []int64
	for _, b := range debits[account] {
		if b.PaidFrom != account {
			panic("a debit names another account")
		}
		ids = append(ids, b.ID)
	}
	return ids
}

func newestOf(account int64, day string) map[AccountUnit]time.Time {
	return map[AccountUnit]time.Time{{AccountID: account, Unit: "TWD"}: on(day)}
}

func TestBillsLeaveTheAccountTheNewestBillADepositLineShowedPaidLeftAfterItsNewestBalance(t *testing.T) {
	bills := []CardBill{
		paidFrom(cardBill(1, "esun", "2026-09-07", "2026-09-22", "8000"), "2026-09-22", savings),
		cardBill(2, "esun", "2026-10-07", "2026-10-22", "9000"),
		cardBill(3, "esun", "2026-11-07", "2026-11-23", "7000"),
		// Another bank's card, paid from the same account.
		paidFrom(cardBill(4, "hsbc", "2026-09-21", "2026-10-09", "3000"), "2026-10-09", savings),
		cardBill(5, "hsbc", "2026-10-21", "2026-11-09", "4000"),
	}
	debits := Debits(bills, newestOf(savings, "2026-10-31"))
	// Bill 2, due before the balance and unpaid past its grace, would have shown in that statement.
	if got := debitIDs(debits, savings); !slices.Equal(got, []int64{5, 3}) {
		t.Fatalf("debits = %v, want [5 3], soonest due first", got)
	}
}

func TestABillOnlyALaterBillShowedPaidLeavesAfterTheBalanceUnlessTheBalanceCoversItsDay(t *testing.T) {
	bills := []CardBill{
		paidFrom(cardBill(1, "hsbc", "2026-08-21", "2026-09-08", "3000"), "2026-09-08", savings),
		// The next bill printed its payment before the deposit statement that shows it came.
		paidFrom(cardBill(2, "hsbc", "2026-09-21", "2026-10-09", "4000"), "2026-10-09", 0),
		cardBill(3, "hsbc", "2026-10-21", "2026-11-09", "5000"),
	}
	if got := debitIDs(Debits(bills, newestOf(savings, "2026-09-30")), savings); !slices.Equal(got, []int64{2, 3}) {
		t.Fatalf("with the balance before its payment = %v, want [2 3]", got)
	}
	// The account's statements cover the day it was paid and show no payment: paid from elsewhere.
	if got := Debits(bills, newestOf(savings, "2026-10-31")); len(got) != 0 {
		t.Fatalf("with the balance past its payment = %v, want none", got)
	}
}

func TestAnUnpaidBillDueWithinTheGraceBeforeTheBalanceStillLeavesAfterIt(t *testing.T) {
	bills := []CardBill{
		paidFrom(cardBill(1, "taishin", "2026-09-07", "2026-09-22", "8000"), "2026-09-22", savings),
		// Due on a Saturday, posted the next working day, after the month-end balance.
		cardBill(2, "taishin", "2026-10-14", "2026-10-29", "9000"),
	}
	if got := debitIDs(Debits(bills, newestOf(savings, "2026-10-31")), savings); !slices.Equal(got, []int64{2}) {
		t.Fatalf("debits = %v, want [2]", got)
	}
	if got := Debits(bills, newestOf(savings, "2026-11-30")); len(got[savings]) != 0 {
		t.Fatalf("with the balance past its grace = %v, want none", got)
	}
}

func TestNoAccountIsDebitedForABankNoDepositLineShowedPaidNorForWhatOwesNothing(t *testing.T) {
	bills := []CardBill{
		paidFrom(cardBill(1, "cathay", "2026-09-01", "2026-09-17", "5000"), "2026-09-17", 0),
		cardBill(2, "cathay", "2026-10-01", "2026-10-19", "6000"),
		paidFrom(cardBill(3, "esun", "2026-09-07", "2026-09-22", "8000"), "2026-09-22", savings),
		cardBill(4, "esun", "2026-11-07", "2026-11-23", "0"),
	}
	newest := newestOf(savings, "2026-10-31")
	newest[AccountUnit{AccountID: other, Unit: "TWD"}] = on("2026-10-31")
	if got := Debits(bills, newest); len(got) != 0 {
		t.Fatalf("debits = %v, want none", got)
	}
	// An account without a balance in the bill's unit has nothing to take it from.
	owed := []CardBill{bills[2], cardBill(5, "esun", "2026-11-07", "2026-11-23", "7000")}
	if got := Debits(owed, newestOf(other, "2026-10-31")); len(got) != 0 {
		t.Fatalf("without a balance = %v, want none", got)
	}
}
