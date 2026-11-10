package domain

import (
	"testing"
	"time"
)

// Synthetic figures (ADR 0006).

func bill(id int64, institution, closing, due string, paid bool) Bill {
	b := Bill{ID: id, Institution: institution, Closing: on(closing), Currency: BaseCurrency, TotalDue: r(due)}
	if paid {
		at := on(closing).Add(10 * 24 * time.Hour)
		b.PaidOn = &at
	}
	return b
}

func TestWhatIsOwedIsEachBanksNewestBillNothingShowedPaid(t *testing.T) {
	owed := Owed([]Bill{
		bill(1, "esun", "2026-09-21", "5000", false), // the next bill's past
		bill(2, "esun", "2026-10-21", "3000", false),
		bill(3, "taishin", "2026-10-05", "800", true),
		bill(4, "cathay", "2026-10-11", "0", false),
		bill(5, "hsbc", "2026-10-15", "-20", false), // a credit
	})
	if len(owed) != 1 || owed[0].ID != 2 {
		t.Fatalf("owed = %+v, want esun's October bill", owed)
	}
}

func TestNetWorthIsWhatTheAccountsAreWorthLessWhatIsOwed(t *testing.T) {
	usd := bill(9, "citi", "2026-10-01", "100", false)
	usd.Currency = "USD"
	n := Net([]Worth{
		{Kind: Deposit, Worth: r("50000")},
		{Kind: Deposit, Worth: r("1000.50")},
		{Kind: Exchange},
		{Kind: Securities, Worth: r("20000")},
	}, []Bill{bill(2, "esun", "2026-10-21", "3000", false), usd})
	if n.Holdings.Cmp(r("71000.5")) != 0 || n.Owed.Cmp(r("3000")) != 0 || n.Net.Cmp(r("68000.5")) != 0 {
		t.Fatalf("net worth = %v - %v = %v", n.Holdings, n.Owed, n.Net)
	}
	if n.Unvalued != 1 || n.OwedElsewhere != 1 || len(n.ByKind) != 3 || n.ByKind[0].Kind != Deposit || n.ByKind[0].Worth.Cmp(r("51000.5")) != 0 {
		t.Fatalf("net worth = %+v", n)
	}
}
