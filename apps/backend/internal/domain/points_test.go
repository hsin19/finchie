package domain

import (
	"fmt"
	"math/big"
	"slices"
	"testing"
	"time"
)

func TestALapseRuleGivesTheDayABillsPointsLapseBy(t *testing.T) {
	cathay := LapseRule{MonthsAfterClosing: 35}
	taishin := LapseRule{PurgeLastMonth: 12, PurgeYears: 2}
	february := LapseRule{PurgeLastMonth: 2, PurgeYears: 2}
	cases := []struct {
		rule          LapseRule
		closing, want string
	}{
		// Added in September 2023, lapsing three years on by month.
		{cathay, "2023-10-01", "2026-09-30"},
		{cathay, "2026-04-01", "2029-03-31"},
		// 2024's points, purged at the end of January 2027; December's bill is 2024's, January's 2025's.
		{taishin, "2024-06-10", "2027-01-31"},
		{taishin, "2024-12-08", "2027-01-31"},
		{taishin, "2025-01-07", "2028-01-31"},
		// A year that ends in February, purged at the end of March.
		{february, "2025-02-20", "2027-03-31"},
		{february, "2025-03-05", "2028-03-31"},
	}
	for _, c := range cases {
		if got := c.rule.LapsesBy(on(c.closing)).Format(time.DateOnly); got != c.want {
			t.Errorf("%+v, closing %s: %s, want %s", c.rule, c.closing, got, c.want)
		}
	}
	if (LapseRule{}).Known() || !cathay.Known() || !taishin.Known() || (LapseRule{PurgeLastMonth: 12}).Known() {
		t.Fatal("Known")
	}
}

func bill(closing string, added, used, balance int64) PointsBill {
	return PointsBill{Closing: on(closing), Added: big.NewRat(added, 1), Used: big.NewRat(used, 1), Balance: big.NewRat(balance, 1)}
}

func lotRows(lots Lots) (held, lapsed []string) {
	for _, l := range lots.Held {
		held = append(held, fmt.Sprintf("%s %s%s", l.By.Format(time.DateOnly), l.Amount.RatString(), map[bool]string{true: " unaged"}[l.Unaged]))
	}
	for _, l := range lots.Lapsed {
		lapsed = append(lapsed, fmt.Sprintf("%s..%s %s", l.From.Format(time.DateOnly), l.To.Format(time.DateOnly), l.Amount.RatString()))
	}
	return held, lapsed
}

func TestPointsLapseAsTheRuleSaysUsedPointsTakingTheSoonestFirst(t *testing.T) {
	// Each bill's points lapse at the end of the second month after its closing month.
	rule := LapseRule{MonthsAfterClosing: 2}
	lots := WorkOutLots([]PointsBill{
		// 100 held already, of days not known: no later than this bill's own 10.
		bill("2026-01-01", 10, 0, 110),
		// 30 used take from the oldest; 20 more vanish, which only the unaged can be.
		bill("2026-02-01", 20, 30, 80),
		// January's 10 lapsed by March's end, and the last 50 unaged with them.
		bill("2026-04-01", 5, 0, 25),
		// February's 20 lapsed by April's end; April's 5 vanishing too is no lapse the rule explains.
		bill("2026-05-01", 0, 0, 0),
		// 7 that no line shows: a lot of their own.
		bill("2026-06-01", 0, 0, 7),
	}, rule)
	held, lapsed := lotRows(lots)
	if want := []string{"2026-08-31 7"}; !slices.Equal(held, want) {
		t.Errorf("held = %v, want %v", held, want)
	}
	if want := []string{"2026-01-01..2026-02-01 20", "2026-02-01..2026-04-01 60", "2026-04-01..2026-05-01 20"}; !slices.Equal(lapsed, want) {
		t.Errorf("lapsed = %v, want %v", lapsed, want)
	}
}

func TestTheLotsHeldAddUpToTheNewestBalanceSoonestFirst(t *testing.T) {
	lots := WorkOutLots([]PointsBill{
		bill("2024-06-10", 285, 0, 400),
		bill("2024-07-07", 332, 0, 732),
		bill("2025-01-07", 139, 50, 821),
	}, LapseRule{PurgeLastMonth: 12, PurgeYears: 2})
	held, lapsed := lotRows(lots)
	// The 50 used came out of the 115 held before the first bill.
	want := []string{"2027-01-31 65 unaged", "2027-01-31 285", "2027-01-31 332", "2028-01-31 139"}
	if !slices.Equal(held, want) || len(lapsed) != 0 {
		t.Errorf("held = %v, lapsed = %v; want %v and none", held, lapsed, want)
	}
}
