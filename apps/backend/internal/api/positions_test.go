package api

import (
	"math/big"
	"slices"
	"testing"
	"time"
)

// Synthetic figures (ADR 0006).

func on(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func r(s string) *big.Rat {
	v, ok := new(big.Rat).SetString(s)
	if !ok {
		panic(s)
	}
	return v
}

// holds takes days and shares in turn: "2026-01-02", "0", "2026-06-01", "1000".
func holds(pairs ...string) []held {
	var out []held
	for pair := range slices.Chunk(pairs, 2) {
		out = append(out, held{day: on(pair[0]), shares: r(pair[1])})
	}
	return out
}

func bought(day, shares, amount, fee string) traded {
	return traded{day: on(day), shares: r(shares), amount: r(amount), fee: r(fee), tax: r("0")}
}

func sold(day, shares, amount, fee, tax string) traded {
	return traded{day: on(day), sell: true, shares: r(shares), amount: r(amount), fee: r(fee), tax: r(tax)}
}

func TestCostIsKnownFromADayTheAccountHeldNoneOfIt(t *testing.T) {
	c, ok := costOf(holds("2026-01-02", "0", "2026-06-01", "1000"), nil, []traded{bought("2026-05-20", "1000", "20000", "28")})
	if !ok || c.cost.FloatString(2) != "20028.00" || c.realized.Sign() != 0 || !c.since.Equal(on("2026-01-02")) {
		t.Fatalf("costing = %+v, %v", c, ok)
	}
}

func TestASaleTakesTheAverageCostOffAndGainsItsAmountLessFeeAndTax(t *testing.T) {
	c, ok := costOf(holds("2026-01-02", "0", "2026-06-01", "1500"), nil, []traded{
		bought("2026-02-03", "1000", "20000", "20"),
		bought("2026-03-03", "1000", "30000", "30"),
		// 500 of 2,000 shares that cost 50,050: 12,512.5 of it; sold for 15,000 less 21 and 45.
		sold("2026-04-03", "500", "15000", "21", "45"),
	})
	if !ok || c.cost.FloatString(2) != "37537.50" || c.realized.FloatString(2) != "2421.50" {
		t.Fatalf("cost %v realized %v (%v)", c.cost.FloatString(2), c.realized.FloatString(2), ok)
	}
}

func TestASecurityBoughtAndSoldOutKeepsWhatItsSaleGained(t *testing.T) {
	c, ok := costOf(holds("2026-01-02", "0", "2026-06-01", "1000", "2026-08-04", "0"), nil, []traded{
		bought("2026-05-20", "1000", "20000", "28"),
		sold("2026-07-24", "1000", "26000", "37", "78"),
	})
	if !ok || c.cost.Sign() != 0 || c.realized.FloatString(2) != "5857.00" || !c.since.Equal(on("2026-01-02")) {
		t.Fatalf("costing = %+v, %v", c, ok)
	}
}

func TestCostIsNotKnownForWhatWasHeldBeforeTheOldestStatement(t *testing.T) {
	if c, ok := costOf(holds("2026-01-02", "5000", "2026-06-01", "3000"), nil, []traded{sold("2026-05-08", "2000", "70000", "99", "210")}); ok {
		t.Fatalf("costing = %+v, want none", c)
	}
}

func TestAGapLeavesTheCostUnknownUntilThePositionStartsAgainFromNone(t *testing.T) {
	history := holds("2026-01-02", "0", "2026-06-01", "1000", "2026-07-02", "1100")
	trades := []traded{bought("2026-05-20", "1000", "20000", "28")}
	// 100 shares no trade explains, a stock dividend say.
	gaps := []time.Time{on("2026-07-02")}
	if c, ok := costOf(history, gaps, trades); ok {
		t.Fatalf("after a gap: costing = %+v, want none", c)
	}

	history = append(history, holds("2026-08-04", "0", "2026-09-01", "500")...)
	trades = append(trades, sold("2026-07-20", "1100", "26000", "37", "78"), bought("2026-08-10", "500", "12000", "17"))
	c, ok := costOf(history, gaps, trades)
	// The sale before is not known to have gained anything: its cost came through the gap.
	if !ok || c.cost.FloatString(2) != "12017.00" || c.realized != nil || !c.since.Equal(on("2026-08-04")) {
		t.Fatalf("started again: costing = %+v, %v", c, ok)
	}
}

func TestTradesThatDoNotEndAtTheNewestHoldingLeaveTheCostUnknown(t *testing.T) {
	if c, ok := costOf(holds("2026-01-02", "0", "2026-06-01", "1200"), nil, []traded{bought("2026-05-20", "1000", "20000", "28")}); ok {
		t.Fatalf("costing = %+v, want none", c)
	}
}
