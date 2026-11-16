package domain

import (
	"slices"
	"testing"
	"time"
)

// Synthetic figures (ADR 0006).

func heldOn(day string) []Balance {
	return []Balance{balance("TWD", "2026-01-31", "1", "1", true), balance("USD", day, "1", "1", true)}
}

func TestAnAccountIsOverdueOnceNothingNewCameForLongerThanItsSourceTakes(t *testing.T) {
	for _, c := range []struct {
		kind          AccountKind
		newest, today string
		overdue       bool
	}{
		{Deposit, "2026-09-30", "2026-11-19", false},
		{Deposit, "2026-09-30", "2026-11-20", true},
		{Points, "2026-09-30", "2026-11-20", true},
		{Securities, "2026-08-04", "2026-11-10", true},
		{Exchange, "2026-11-09", "2026-11-10", false},
		{Exchange, "2026-11-08", "2026-11-10", true},
	} {
		fresh := Fresh(c.kind, false, heldOn(c.newest), time.Time{}, on(c.today))
		if fresh.Overdue != c.overdue || !fresh.Newest.Equal(on(c.newest)) {
			t.Errorf("%s last on %s, on %s: %+v, want overdue %v", c.kind, c.newest, c.today, fresh, c.overdue)
		}
	}
	if fresh := Fresh(Deposit, true, heldOn("2026-01-01"), time.Time{}, on("2026-11-10")); fresh.Overdue || fresh.Excused {
		t.Errorf("an account the owner keeps = %+v, never overdue", fresh)
	}
	if fresh := Fresh(Deposit, false, nil, time.Time{}, on("2026-11-10")); fresh.Overdue || !fresh.Newest.IsZero() {
		t.Errorf("an account with nothing held = %+v", fresh)
	}
}

func TestAnAccountIsNotOverdueBeforeTheDayTheOwnerSaidToLookAgain(t *testing.T) {
	// A brokerage sends nothing for a month without a trade.
	quiet := func(today string) Freshness {
		return Fresh(Securities, false, heldOn("2026-08-04"), on("2027-01-02"), on(today))
	}
	if got := quiet("2026-11-13"); got.Overdue || !got.Excused {
		t.Errorf("before the day = %+v, want excused", got)
	}
	if got := quiet("2027-01-02"); !got.Overdue || got.Excused {
		t.Errorf("on the day = %+v, want overdue", got)
	}
	if got := Fresh(Deposit, false, heldOn("2026-11-01"), on("2027-01-02"), on("2026-11-13")); got.Overdue || got.Excused {
		t.Errorf("current = %+v, nothing to excuse", got)
	}
}

func TestTheNextLapseIsTheSoonestFromTodayOnOfTheRulesLotsAndWhatReportsSay(t *testing.T) {
	today := on("2026-11-13")
	lots := []Lapsing{
		{Unit: "e point", Amount: r("10"), By: on("2026-10-31"), Estimated: true},
		{Unit: "e point", Amount: r("28"), By: on("2026-12-31"), Estimated: true},
	}
	reported := []Lapsing{{Unit: "e point", Amount: r("30"), By: on("2026-12-31")}}
	next := NextLapse(lots, reported, today)
	if next == nil || next.Estimated || next.Amount.Cmp(r("30")) != 0 {
		t.Fatalf("next = %+v, want the reported 30 on 12/31", next)
	}
	if next := NextLapse(lots, nil, today); next == nil || next.Amount.Cmp(r("28")) != 0 || !next.Soon(today) {
		t.Fatalf("by the rule = %+v, want 28 on 12/31, soon", next)
	}
	if later := (Lapsing{By: on("2027-01-12")}); !later.Soon(today) || (Lapsing{By: on("2027-01-13")}).Soon(today) {
		t.Fatal("soon is within 60 days")
	}
	if NextLapse(lots[:1], nil, today) != nil {
		t.Fatal("a lot gone by is no next lapse")
	}
}

// E.SUN's savings pays HSBC's bill on 11/09 and E.SUN's on 11/23; its newest balance is October's end.
func savingsPaying(amount string) ([]Balance, []CardBill) {
	held := []Balance{balance("TWD", "2026-10-31", amount, amount, true)}
	debits := []CardBill{
		{ID: 10, Institution: "hsbc", DueDate: on("2026-11-09"), Unit: "TWD", TotalDue: r("3000"), PaidFrom: savings},
		{ID: 11, Institution: "esun", DueDate: on("2026-11-23"), Unit: "TWD", TotalDue: r("4000.50"), PaidFrom: savings},
	}
	return held, debits
}

func TestAShortfallIsWhatTheNewestBalanceLacksToCoverEveryBillTakenAfterIt(t *testing.T) {
	today := on("2026-11-13")
	held, debits := savingsPaying("5000")
	got := Shortfalls(held, debits, Freshness{}, today)
	if len(got) != 1 || got[0].Owed.Cmp(r("7000.50")) != 0 || got[0].Lacks.Cmp(r("2000.50")) != 0 || len(got[0].Debits) != 2 {
		t.Fatalf("shortfalls = %+v, want 2000.50 short of 7000.50, both bills", got)
	}

	covered, _ := savingsPaying("7000.50")
	if got := Shortfalls(covered, debits, Freshness{}, today); len(got) != 0 {
		t.Errorf("covered = %+v", got)
	}
	if got := Shortfalls(held, debits, Freshness{}, on("2026-11-24")); len(got) != 0 {
		t.Errorf("once every bill is due = %+v", got)
	}
	if got := Shortfalls(held, debits, Freshness{Overdue: true}, today); len(got) != 0 {
		t.Errorf("by an overdue balance = %+v", got)
	}
	// A bill a later bill showed paid counts, but is not one still to come out.
	paid := []CardBill{{ID: 11, DueDate: on("2026-11-23"), Unit: "TWD", TotalDue: r("6000"), PaidOn: on("2026-11-23")}}
	if got := Shortfalls(held, paid, Freshness{}, today); len(got) != 0 {
		t.Errorf("paid = %+v", got)
	}
}

func TestNoticesComeInTheOrderToDealWithThemEachKeyedByItsSituation(t *testing.T) {
	today := on("2026-11-13")
	held, debits := savingsPaying("5000")
	failed := Locked
	accounts := []Watched{
		{
			ID: 1, Shortfalls: Shortfalls(held, debits, Freshness{}, today),
			Gaps: []Gap{{AccountID: 1, Unit: "TWD", From: on("2026-08-31"), To: on("2026-09-30"), Unseen: r("-200")}},
		},
		{ID: 2, Fresh: Freshness{Newest: on("2026-08-04"), Overdue: true}},
		{ID: 3, NextLapse: &Lapsing{Unit: "小樹點", Amount: r("1200"), By: on("2026-12-31")}},
		// Lapses, but not soon.
		{ID: 4, NextLapse: &Lapsing{Unit: "e point", Amount: r("50"), By: on("2027-06-30")}},
	}
	documents := []TroubledDocument{{ID: 9, Filename: "bill.eml", Status: Failed, Failure: &failed}}

	var keys []string
	for _, n := range Notices(accounts, documents, today) {
		keys = append(keys, n.Key)
	}
	want := []string{
		"shortfall:1:TWD:2026-10-31:2026-11-23",
		"document:9:failed",
		"gap:1:TWD:2026-08-31:2026-09-30",
		"overdue:2:2026-08-04",
		"lapsing:3:小樹點:2026-12-31",
	}
	if !slices.Equal(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
}
