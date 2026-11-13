package domain

import (
	"testing"
	"time"
)

// Synthetic figures (ADR 0006).

func balance(unit, day, amount, worth string, fromStatement bool) Balance {
	b := Balance{Unit: unit, AsOf: on(day), Amount: r(amount), FromStatement: fromStatement}
	if worth != "" {
		b.Worth = r(worth)
	}
	return b
}

func TestAnItemizedAccountAStatementReportsIsCheckedUnlessABalanceMovedUnshowably(t *testing.T) {
	latest := []Balance{balance("TWD", "2026-10-31", "1000", "1000", true)}
	if got := Assess(Deposit, latest, Gaps{}, nil).Continuity; got != Checked {
		t.Fatalf("deposit = %s, want checked", got)
	}
	if got := Assess(Deposit, latest, Gaps{Unshowable: true}, nil).Continuity; got != Unknown {
		t.Fatalf("deposit with an unshowable move = %s, want unknown", got)
	}
	snapshot := []Balance{balance("TWD", "2026-10-31", "1000", "1000", false)}
	if got := Assess(Deposit, snapshot, Gaps{}, nil).Continuity; got != Unknown {
		t.Fatalf("deposit known from snapshots alone = %s, want unknown", got)
	}
	if got := Assess(Exchange, latest, Gaps{}, nil).Continuity; got != Unknown {
		t.Fatalf("exchange = %s, want unknown", got)
	}
}

func TestAnAccountIsWorthItsHoldingsElseItsValuationOfTheSameDay(t *testing.T) {
	held := []Balance{
		balance("BTC", "2026-11-01", "0.5", "1000000", false),
		balance("USDT", "2026-11-01", "100", "3200", false),
		balance("ETH", "2026-11-01", "0", "", false),
	}
	standing := Assess(Exchange, held, Gaps{}, nil)
	if standing.Worth.Cmp(r("1003200")) != 0 || standing.WorthFrom != FromHoldings || len(standing.Held) != 2 {
		t.Fatalf("standing = %+v; want 1003200 from holdings, ETH at 0 not held", standing)
	}

	unpriced := append([]Balance{balance("XYZ", "2026-11-01", "7", "", false)}, held...)
	valuation := &Valuation{AsOf: on("2026-11-01"), Worth: r("1003500")}
	if standing := Assess(Exchange, unpriced, Gaps{}, valuation); standing.Worth.Cmp(r("1003500")) != 0 || standing.WorthFrom != FromValuation {
		t.Fatalf("with a unit unpriced = %+v, want the valuation", standing)
	}
	stale := &Valuation{AsOf: on("2026-10-31"), Worth: r("1003500")}
	if standing := Assess(Exchange, unpriced, Gaps{}, stale); standing.Worth != nil {
		t.Fatalf("with an older valuation = %+v, want no worth", standing)
	}
}

func TestAnAccountItsSourcesSayHoldsNothingIsWorthNothingButADepositAtZeroStillHasItsCurrency(t *testing.T) {
	soldOut := []Balance{balance("範例電子", "2026-11-01", "0", "0", true)}
	if standing := Assess(Securities, soldOut, Gaps{}, nil); standing.Worth.Sign() != 0 || len(standing.Held) != 0 {
		t.Fatalf("sold out = %+v", standing)
	}
	empty := []Balance{balance("TWD", "2026-11-01", "0", "0", true)}
	if standing := Assess(Deposit, empty, Gaps{}, nil); len(standing.Held) != 1 || standing.Worth.Sign() != 0 {
		t.Fatalf("deposit at 0 = %+v", standing)
	}
	if standing := Assess(Deposit, nil, Gaps{}, nil); standing.Worth != nil {
		t.Fatalf("never reported = %+v, want no worth", standing)
	}
}

func TestOnlyMoneyThatMovedInAUnitAStatementItemizesIsAGap(t *testing.T) {
	gaps := []Gap{
		{AccountID: 1, Unit: "TWD", Unseen: r("800"), Itemized: true},
		{AccountID: 2, Unit: "TWD", Unseen: r("5"), Itemized: false},
		{AccountID: 3, Unit: "USD", Unseen: r("1"), Itemized: true},
		{AccountID: 3, Unit: "TWD", Unseen: r("2"), Itemized: false},
	}
	read := ReadGaps(gaps)
	if len(read[1].Unseen) != 1 || read[1].Unshowable || len(read[2].Unseen) != 0 || !read[2].Unshowable || !read[3].Unshowable {
		t.Fatalf("read = %+v", read)
	}
	if n := GappedAccounts(gaps); n != 2 {
		t.Fatalf("gapped accounts = %d, want 2", n)
	}
}

func TestAFillCountsOnTheLastDayNoReportCovers(t *testing.T) {
	// July's statement closed 7/31 and September's covers 9/1 to 9/30: August's is the one missing.
	gap := Gap{From: on("2026-07-31"), To: on("2026-09-30"), ReportedFrom: on("2026-09-01")}
	if got := gap.FilledOn(); !got.Equal(on("2026-08-31")) {
		t.Fatalf("filled on %s, want the day before September's statement starts", got.Format(time.DateOnly))
	}
	// A statement that prints no first day, or one that starts right after the balance before: the gap is
	// somewhere up to its closing.
	for _, gap := range []Gap{
		{From: on("2026-07-31"), To: on("2026-09-30")},
		{From: on("2026-07-31"), To: on("2026-09-30"), ReportedFrom: on("2026-08-01")},
	} {
		if got := gap.FilledOn(); !got.Equal(on("2026-09-30")) {
			t.Fatalf("%+v filled on %s, want its closing day", gap, got.Format(time.DateOnly))
		}
	}
}

func TestAFullNumberMustBeTheOneTheBankMasks(t *testing.T) {
	for given, want := range map[string]string{
		"1234-5678 9012": "123456789012",
		"123456789013":   "",
		"12345678901":    "",
	} {
		got, ok := FullNumber("1234****9012", given)
		if ok != (want != "") || got != want {
			t.Errorf("%q: %q %v, want %q", given, got, ok, want)
		}
	}
	if got, ok := FullNumber("", " 存摺 7 號 "); !ok || got != "存摺 7 號" {
		t.Fatalf("no printed number: %q %v, want any number kept", got, ok)
	}
}

func TestEverySourceSpellsAUnitTheSameWay(t *testing.T) {
	if got := Symbol(Money, " usdt "); got != "USDT" {
		t.Fatalf("money = %q", got)
	}
	if got := Symbol(Security, "範例　電子  ＡＢＣ"); got != "範例 電子 ABC" {
		t.Fatalf("security = %q, want the parser's clean", got)
	}
	if got := Symbol(PointsUnit, " e point "); got != "e point" {
		t.Fatalf("points = %q", got)
	}
}

func TestAStatementIsKeyedByTheAccountsItReports(t *testing.T) {
	if got := AccountNumbers("98**765", "", "12**345", "98**765"); got != "12**345,98**765" {
		t.Fatalf("key = %q", got)
	}
	if got := AccountNumbers("", ""); got != "" {
		t.Fatalf("a card bill's key = %q, want none", got)
	}
}

func TestALiabilityIsWorthWhatItOwesTakenOff(t *testing.T) {
	bill := []Balance{balance("TWD", "2026-10-21", "3000", "3000", true)}
	standing := Assess(CreditCard, bill, Gaps{}, nil)
	if standing.Worth.Cmp(r("-3000")) != 0 || standing.Continuity != Unknown || !standing.Newest.Equal(on("2026-10-21")) {
		t.Fatalf("card = %+v", standing)
	}
	if !Loan.Liability() || Deposit.Liability() || Loan.KeptByHand() != true || CreditCard.KeptByHand() {
		t.Fatal("sides or keeping by hand wrong")
	}
}
