package domain

import (
	"testing"
	"time"
)

// Synthetic figures (ADR 0006).

func TestNetWorthIsAssetsLessLiabilitiesWithAccountsWithoutAWorthCounted(t *testing.T) {
	day := on("2026-11-30")
	n := Net(day, []Worth{
		{AccountID: 1, Kind: Deposit, Worth: r("50000"), Newest: on("2026-11-30")},
		{AccountID: 2, Kind: Deposit, Worth: r("1000.50"), Newest: on("2026-11-30")},
		{AccountID: 3, Kind: Exchange, Newest: on("2026-11-30")},
		{AccountID: 4, Kind: CreditCard, Worth: r("-3000"), Newest: on("2026-11-21")},
		{AccountID: 5, Kind: Loan, Worth: r("-800000"), Newest: on("2026-06-01"), ByHand: true},
	})
	if n.Assets.Cmp(r("51000.5")) != 0 || n.Liabilities.Cmp(r("-803000")) != 0 || n.Net.Cmp(r("-751999.5")) != 0 {
		t.Fatalf("net worth = %v + %v = %v", n.Assets, n.Liabilities, n.Net)
	}
	if n.Unvalued != 1 || len(n.ByKind) != 4 || n.ByKind[0].Kind != Deposit || n.ByKind[3].Kind != Loan {
		t.Fatalf("by kind = %+v, unvalued %d", n.ByKind, n.Unvalued)
	}
	// The card's bill closed on the 21st: exact up to then; the loan the owner keeps says nothing of it.
	if !n.CompleteAsOf.Equal(on("2026-11-21")) || len(n.Behind) != 1 || n.Behind[0] != 4 {
		t.Fatalf("complete as of %v, behind %v", n.CompleteAsOf, n.Behind)
	}
}

func TestAnAccountThatHoldsNothingDoesNotHoldBackTheDayNetWorthIsExactAsOf(t *testing.T) {
	complete := CompleteAsOf([]Worth{
		{Kind: Deposit, Worth: r("10"), Newest: on("2026-11-30")},
		{Kind: Securities, Worth: r("0"), Newest: on("2025-01-31"), Empty: true},
		{Kind: Points},
	})
	if !complete.Equal(on("2026-11-30")) {
		t.Fatalf("complete as of %v", complete)
	}
}

func TestAnAccountTheOwnerConfirmedIsKnownThroughTodayUntilTheirDayToLookAgain(t *testing.T) {
	today := on("2026-11-13")
	none := time.Time{}
	cases := []struct {
		newest, checkAgainOn time.Time
		want                 string
	}{
		// A brokerage that sent nothing since August, confirmed: its balance stands through today.
		{on("2026-08-04"), on("2027-01-02"), "2026-11-13"},
		// The day to look again has come: it is only as known as its newest report.
		{on("2026-08-04"), on("2026-11-13"), "2026-08-04"},
		{on("2026-08-04"), none, "2026-08-04"},
		// Reported since: nothing to vouch for.
		{on("2026-11-13"), on("2027-01-02"), "2026-11-13"},
	}
	for _, c := range cases {
		if got := ReportedThrough(c.newest, c.checkAgainOn, today).Format(time.DateOnly); got != c.want {
			t.Errorf("newest %v, check again %v: %s, want %s", c.newest, c.checkAgainOn, got, c.want)
		}
	}
}
