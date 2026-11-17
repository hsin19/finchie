package api

import (
	"fmt"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hsin19/finchie/apps/backend/internal/exchanges"
	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
	pt "github.com/hsin19/finchie/apps/backend/internal/parsertest"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// Synthetic figures (ADR 0006).

func TestInvestmentsCountWhatTheyWereWorthWhenFirstValuedAndTheMoneyThatCrossedInOrOutSince(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	ctx := t.Context()
	november := func(day, hour int) time.Time {
		return taipei.At(time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC), hour, 0)
	}
	rate(t, pool, "2026-11-01", "USDT", "0.03125") // NT$32
	price(t, pool, "security", "範例電子", "twse", november(2, 13), "1", "TWD", "100")
	price(t, pool, "security", "範例電子", "twse", november(5, 13), "1", "TWD", "120")

	// 1,000 shares held on 11/3, bought before Finchie saw them. Then 500 bought on 11/4, 100 paid in advance
	// on 11/5, 200 sold on 11/6.
	store(t, pool, "october.eml", pt.Response("skis.securities", pt.Securities("skis", "12**345", pt.Day(10, 1), pt.Day(11, 3),
		[]*parserv2.Balance{pt.Balance("範例電子", pt.Day(11, 3), "1000")})))
	prepaid := pt.Trade(pt.Day(11, 5), "範例電子", parserv2.LineKind_LINE_KIND_BUY, "100", "120", "12000", "5", "0", "0")
	prepaid.GetTrade().Prepaid = pt.Dec("12005")
	store(t, pool, "november.eml", pt.Response("skis.securities", pt.Securities("skis", "12**345", pt.Day(11, 4), pt.Day(12, 3),
		[]*parserv2.Balance{pt.Balance("範例電子", pt.Day(12, 3), "1400")},
		pt.Trade(pt.Day(11, 4), "範例電子", parserv2.LineKind_LINE_KIND_BUY, "500", "100", "50000", "21", "0", "-50021"),
		prepaid,
		pt.Trade(pt.Day(11, 6), "範例電子", parserv2.LineKind_LINE_KIND_SELL, "-200", "120", "24000", "10", "72", "23918"),
	)))

	// NT$10,000 from the bank on 11/5, linked to MAX's deposit of it.
	storeDeposit(t, pool, 11, "0", depositLine{day: 5, amount: "-10000", after: "0", description: "轉出", opposite: "805 1234567890***123"})
	span := func(at time.Time, moves ...exchanges.Movement) exchanges.History {
		return exchanges.History{From: at.AddDate(0, -1, 0), To: at, Movements: moves}
	}
	type held = map[string]int64
	holding := func(units held) exchanges.Snapshot {
		s := exchanges.Snapshot{Holdings: map[string]*big.Rat{}}
		for unit, amount := range units {
			s.Holdings[unit] = big.NewRat(amount, 1)
		}
		return s
	}
	moved := func(id string, at time.Time, unit string, amount int64, txID string) exchanges.Movement {
		return exchanges.Movement{ID: id, At: at, Unit: unit, Amount: big.NewRat(amount, 1), TxID: txID}
	}
	stored := func(institution string, at time.Time, s exchanges.Snapshot, moves ...exchanges.Movement) {
		t.Helper()
		if err := exchanges.Store(ctx, pool, institution, at, s, span(at, moves...)); err != nil {
			t.Fatal(err)
		}
	}
	// MAX: NT$20,000 deposited on 11/2 and 201 USDT from a wallet on 11/3, before its first snapshot;
	// NT$10,000 more on 11/5, and 100 USDT sent on to Bitget, 1 USDT taken for it; 50 USDT sent to Binance
	// on 11/6.
	stored("max", november(4, 10), holding(held{"TWD": 20000, "USDT": 201}),
		moved("deposit/1", november(2, 9), "TWD", 20000, ""), moved("deposit/0", november(3, 9), "USDT", 201, "0x201"))
	toBitget := moved("withdrawal/1", november(5, 9), "USDT", -100, "0xabc")
	toBitget.Fee, toBitget.FeeUnit = big.NewRat(1, 1), "USDT"
	stored("max", november(5, 10), holding(held{"TWD": 30000, "USDT": 100}), moved("deposit/2", november(5, 8), "TWD", 10000, ""), toBitget)
	stored("max", november(6, 10), holding(held{"TWD": 30000, "USDT": 50}), moved("withdrawal/2", november(6, 8), "USDT", -50, "0x456"))
	// Bitget: 100 USDT from a wallet on 10/20, before USDT had a price, of which 10 are left on 11/4; the
	// 100 from MAX on 11/5; 50 from a wallet of the owner's on 11/6.
	stored("bitget", november(4, 10), holding(held{"USDT": 10}), moved("deposit/0", november(1, 9).AddDate(0, 0, -12), "USDT", 100, "0x100"))
	stored("bitget", november(5, 10), holding(held{"USDT": 110}), moved("deposit/a", november(5, 9), "USDT", 100, "0xabc"))
	stored("bitget", november(6, 10), holding(held{"USDT": 160}), moved("deposit/b", november(6, 9), "USDT", 50, "0xdef"))
	// Binance first reports on 11/6, holding the 50 from MAX.
	stored("binance", november(6, 10), holding(held{"USDT": 50}), moved("deposit/x", november(6, 9), "USDT", 50, "0x456"))

	var got []string
	for _, p := range decode[[]InvestmentPoint](t, get(t, e, "/api/investments/history?from=2026-11-01&to=2026-11-06&step=day")) {
		ret := "-"
		if p.ReturnPercent != nil {
			ret = *p.ReturnPercent
		}
		got = append(got, fmt.Sprintf("%s worth %s in %s gain %s %s%%", p.On, p.Worth, p.Invested, p.Gain, ret))
	}
	want := []string{
		// The brokerage first has a worth: 1,000 shares at 100.
		"2026-11-03 worth 100000.00 in 100000.00 gain 0.00 0.00%",
		// 500 more bought; MAX and Bitget first report, with everything that went into them before: MAX's
		// NT$20,000 and 201 USDT, Bitget's 100 USDT at USDT's first price (32), 90 of them lost.
		"2026-11-04 worth 176752.00 in 179653.00 gain -2901.00 -1.61%",
		// The close is 120; NT$10,000 into MAX and 12,005 paid in advance. The 100 USDT MAX sent Bitget
		// left one and entered the other; the 1 USDT MAX took for it is a loss.
		"2026-11-05 worth 228720.00 in 201658.00 gain 27062.00 13.42%",
		// 23,918 out from the sale; 50 USDT in at 32 from a wallet. Binance opens holding the 50 USDT MAX
		// sent it: out of MAX, into Binance, no gain.
		"2026-11-06 worth 206320.00 in 179340.00 gain 26980.00 15.04%",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("investments =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Each account on its own, from the day it opened to 11/06: the brokerage made 29,892 on what went into
	// it; MAX lost the fee; Bitget the 90 USDT. A bank's account is none.
	var accounts []string
	ids := map[string]int64{}
	for _, a := range decode[[]Account](t, get(t, e, "/api/accounts")) {
		ids[a.Institution] = a.Id
		points := decode[[]InvestmentPoint](t, get(t, e, fmt.Sprintf("/api/investments/history?account=%d&from=2026-11-01&to=2026-11-06&step=day", a.Id)))
		if len(points) == 0 {
			accounts = append(accounts, a.Institution+" none")
			continue
		}
		last := points[len(points)-1]
		accounts = append(accounts, fmt.Sprintf("%s from %s worth %s in %s gain %s %s%%", a.Institution, points[0].On, last.Worth, last.Invested, last.Gain, *last.ReturnPercent))
	}
	slices.Sort(accounts)
	wantAccounts := []string{
		"binance from 2026-11-06 worth 1600.00 in 1600.00 gain 0.00 0.00%",
		"bitget from 2026-11-04 worth 5120.00 in 8000.00 gain -2880.00 -36.00%",
		"esun none",
		"max from 2026-11-04 worth 31600.00 in 31632.00 gain -32.00 -0.10%",
		"skis from 2026-11-03 worth 168000.00 in 138108.00 gain 29892.00 21.64%",
	}
	if !slices.Equal(accounts, wantAccounts) {
		t.Fatalf("accounts =\n%s\nwant\n%s", strings.Join(accounts, "\n"), strings.Join(wantAccounts, "\n"))
	}

	// An exchange counts everything that ever went into it, whatever day the series starts on.
	bitget := decode[[]InvestmentPoint](t, get(t, e, fmt.Sprintf("/api/investments/history?account=%d&from=2026-11-06&to=2026-11-06", ids["bitget"])))
	if len(bitget) != 1 || bitget[0].Invested != "8000.00" {
		t.Fatalf("bitget from 11/06 = %+v, want 8,000 in", bitget)
	}

	if got := get(t, e, "/api/investments/history?from=2026-11-06&to=2026-11-01").Code; got != http.StatusBadRequest {
		t.Errorf("to before from: status = %d", got)
	}
	// Years of days asked for, of which only those since the first price are worked out: within the limit
	// on points, and the same series.
	long := decode[[]InvestmentPoint](t, get(t, e, "/api/investments/history?from=2020-01-01&to=2026-11-06&step=day"))
	if len(long) != len(want) || long[0].On.String() != "2026-11-03" {
		t.Fatalf("from 2020: %d points from %v, want the same %d from 2026-11-03", len(long), long[0].On, len(want))
	}
	// Before any of it had a price: nothing yet.
	if early := decode[[]InvestmentPoint](t, get(t, e, "/api/investments/history?from=2026-01-01&to=2026-10-31")); len(early) != 0 {
		t.Fatalf("before any price: %+v", early)
	}
}

func TestAnExchangeIsAnInvestmentFromTheDayARateStoredAtMidnightFirstPricesIt(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	// USDT's first rate is stored at midnight starting 11/7, so it prices the end of 11/6, Bitget's first day.
	rate(t, pool, "2026-11-07", "USDT", "0.03125")
	at := taipei.At(time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC), 10, 0)
	snapshot := exchanges.Snapshot{Holdings: map[string]*big.Rat{"USDT": big.NewRat(10, 1)}}
	if err := exchanges.Store(t.Context(), pool, "bitget", at, snapshot, exchanges.History{}); err != nil {
		t.Fatal(err)
	}
	points := decode[[]InvestmentPoint](t, get(t, e, "/api/investments/history?from=2026-11-01&to=2026-11-07&step=day"))
	if len(points) == 0 || points[0].On.String() != "2026-11-06" || points[0].Worth != "320.00" {
		t.Fatalf("points = %+v, want from 11/06 worth 320", points)
	}
}
