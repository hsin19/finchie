package domain

import (
	"fmt"
	"testing"
	"time"
)

// Synthetic figures (ADR 0006).

func investedLine(p Invested) string {
	ret := "-"
	if p.Return != nil {
		ret = p.Return.FloatString(4)
	}
	return fmt.Sprintf("%s worth %s in %s gain %s return %s", p.Day.Format(time.DateOnly), p.Worth.FloatString(0), p.Invested.FloatString(0), p.Gain.FloatString(0), ret)
}

func TestWhatAnInvestmentWasWorthWhenFirstValuedCountsAsPutInAndFlowsSinceAreAdded(t *testing.T) {
	days := []time.Time{on("2026-11-01"), on("2026-11-02"), on("2026-11-03"), on("2026-11-04")}
	brokerage := func(worth string) Worth {
		w := Worth{AccountID: 1, Kind: Securities, Newest: on("2026-11-04")}
		if worth != "" {
			w.Worth = r(worth)
		}
		return w
	}
	worths := [][]Worth{
		// Held before any price: no worth, so not open yet.
		{brokerage(""), {AccountID: 9, Kind: Deposit, Worth: r("50000"), Newest: on("2026-11-04")}},
		{brokerage("100000")},
		{brokerage("130000")},
		{brokerage("125000")},
	}
	flows := []Flow{
		// Bought before it opened: in what it was worth on the 2nd.
		{AccountID: 1, Day: on("2026-11-01"), Worth: r("40000")},
		{AccountID: 1, Day: on("2026-11-02"), Worth: r("1000")},
		// Bought 20,000 on the 3rd, sold 10,000 on the 4th.
		{AccountID: 1, Day: on("2026-11-03"), Worth: r("20000")},
		{AccountID: 1, Day: on("2026-11-04"), Worth: r("-10000")},
	}
	var got []string
	for _, p := range InvestedOver(days, worths, flows) {
		got = append(got, investedLine(p))
	}
	want := []string{
		"2026-11-02 worth 100000 in 100000 gain 0 return 0.0000",
		"2026-11-03 worth 130000 in 120000 gain 10000 return 0.0833",
		"2026-11-04 worth 125000 in 110000 gain 15000 return 0.1364",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("series =\n%v\nwant\n%v", got, want)
	}
}

func TestABrokerageFirstValuedLaterBringsNoGainWithIt(t *testing.T) {
	days := []time.Time{on("2026-11-06"), on("2026-11-09")}
	worths := [][]Worth{
		{{AccountID: 1, Kind: Securities, Worth: r("100000"), Newest: on("2026-11-09")}},
		{{AccountID: 1, Kind: Securities, Worth: r("101000"), Newest: on("2026-11-09")}, {AccountID: 2, Kind: Securities, Worth: r("300000"), Newest: on("2026-11-09")}},
	}
	series := InvestedOver(days, worths, nil)
	if got := investedLine(series[1]); got != "2026-11-09 worth 401000 in 400000 gain 1000 return 0.0025" {
		t.Fatalf("on 11/09: %s", got)
	}
}

func TestAnInvestmentWithoutAWorthOnADayIsLeftOutOfBothSidesAndCounted(t *testing.T) {
	days := []time.Time{on("2026-11-01"), on("2026-11-02")}
	worths := [][]Worth{
		{{AccountID: 1, Kind: Securities, Worth: r("100000"), Newest: on("2026-11-02")}, {AccountID: 2, Kind: Exchange, Worth: r("50000"), Newest: on("2026-11-02")}},
		{{AccountID: 1, Kind: Securities, Newest: on("2026-11-02")}, {AccountID: 2, Kind: Exchange, Worth: r("55000"), Newest: on("2026-11-02")}},
	}
	// NT$50,000 deposited on the exchange's first day; a coin sent in from a wallet without a price.
	flows := []Flow{{AccountID: 2, Day: on("2026-11-01"), Worth: r("50000")}, {AccountID: 2, Day: on("2026-11-02")}}
	p := InvestedOver(days, worths, flows)[1]
	if got := investedLine(p); got != "2026-11-02 worth 55000 in 50000 gain 5000 return 0.1000" || p.Unvalued != 1 || p.Unpriced != 1 {
		t.Fatalf("on 11/02: %s, unvalued %d, unpriced %d", got, p.Unvalued, p.Unpriced)
	}
}

func TestWhatWentIntoAnExchangeIsEveryDepositAndWithdrawalItEverReported(t *testing.T) {
	days := []time.Time{on("2026-11-09"), on("2026-11-10")}
	exchange := func(worth string) Worth {
		return Worth{AccountID: 2, Kind: Exchange, Worth: r(worth), Newest: on("2026-11-10")}
	}
	worths := [][]Worth{{exchange("80000")}, {exchange("70000")}}
	// NT$700,000 in long before its first snapshot, 20,000 taken out, and coins from a wallet before any
	// price: all of it went in, and most of it is gone.
	flows := []Flow{
		{AccountID: 2, Day: on("2026-01-04"), Worth: r("700000")},
		{AccountID: 2, Day: on("2026-02-01")},
		{AccountID: 2, Day: on("2026-03-01"), Worth: r("-20000")},
	}
	series := InvestedOver(days, worths, flows)
	var got []string
	for _, p := range series {
		got = append(got, investedLine(p))
	}
	want := []string{
		"2026-11-09 worth 80000 in 680000 gain -600000 return -0.8824",
		"2026-11-10 worth 70000 in 680000 gain -610000 return -0.8971",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) || series[0].Unpriced != 1 {
		t.Fatalf("series =\n%v\nwant\n%v, unpriced %d", got, want, series[0].Unpriced)
	}
}

func TestMoreTakenOutThanPutInHasAGainButNoReturn(t *testing.T) {
	days := []time.Time{on("2026-11-01"), on("2026-11-02")}
	worths := [][]Worth{
		{{AccountID: 2, Kind: Exchange, Worth: r("1000"), Newest: on("2026-11-02")}},
		{{AccountID: 2, Kind: Exchange, Worth: r("500"), Newest: on("2026-11-02")}},
	}
	flows := []Flow{{AccountID: 2, Day: on("2026-10-01"), Worth: r("1000")}, {AccountID: 2, Day: on("2026-11-02"), Worth: r("-1200")}}
	p := InvestedOver(days, worths, flows)[1]
	if got := investedLine(p); got != "2026-11-02 worth 500 in -200 gain 700 return -" {
		t.Fatalf("on 11/02: %s", got)
	}
}

func TestOnlyMarketAccountsTheOwnerDoesNotKeepByHandAreInvestments(t *testing.T) {
	days := []time.Time{on("2026-11-01")}
	worths := [][]Worth{{
		{AccountID: 1, Kind: Deposit, Worth: r("1000"), Newest: on("2026-11-01")},
		{AccountID: 2, Kind: Points, Worth: r("10"), Newest: on("2026-11-01")},
		{AccountID: 3, Kind: Securities, Worth: r("900"), Newest: on("2026-11-01"), ByHand: true},
	}}
	if series := InvestedOver(days, worths, nil); len(series) != 0 {
		t.Fatalf("series = %+v, want none", series)
	}
}

func TestAnInvestmentsFigureIsExactUpToTheOldestOfItsAccountsNewestReports(t *testing.T) {
	days := []time.Time{on("2026-11-01"), on("2026-11-05")}
	accounts := []Worth{
		{AccountID: 1, Kind: Securities, Worth: r("100"), Newest: on("2026-11-03")},
		{AccountID: 2, Kind: Exchange, Worth: r("100"), Newest: on("2026-11-05")},
		// A bank behind by a month says nothing of the investments.
		{AccountID: 3, Kind: Deposit, Worth: r("100"), Newest: on("2026-10-01")},
	}
	series := InvestedOver(days, [][]Worth{accounts, accounts}, nil)
	if !series[0].Complete || series[1].Complete {
		t.Fatalf("complete = %v, %v; want exact on 11/01 only", series[0].Complete, series[1].Complete)
	}
	if len(series[0].Behind) != 0 || fmt.Sprint(series[1].Behind) != "[1]" {
		t.Fatalf("behind = %v, %v; want [], [1]", series[0].Behind, series[1].Behind)
	}
}
