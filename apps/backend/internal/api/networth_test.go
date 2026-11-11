package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
	pt "github.com/hsin19/finchie/apps/backend/internal/parsertest"
)

// Synthetic figures (ADR 0006).

func TestALoanTheOwnerKeepsIsALiabilityThatCountsAgainstThem(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	storeDeposit(t, pool, 9, "50000")
	loan := open(t, e, AccountKindLoan, "房貸")
	hold(t, e, loan, `[{"unit": "TWD", "amount": "800000", "expiries": []}]`)

	account := accountOf(t, e, loan)
	if account.Side != Liability || account.Worth == nil || *account.Worth != "-800000.00" || account.Balances[0].Amount != "800000" {
		t.Fatalf("loan = %+v", account)
	}
	net := decode[NetWorth](t, get(t, e, "/api/net-worth"))
	if net.Assets != "50000.00" || net.Liabilities != "-800000.00" || net.Net != "-750000.00" {
		t.Fatalf("net worth = %+v", net)
	}
	// Kept by hand, it says nothing of how current anything else is.
	if net.CompleteAsOf == nil || net.CompleteAsOf.String() != "2026-09-30" {
		t.Fatalf("complete as of %v", net.CompleteAsOf)
	}
}

func TestNetWorthOnADayIsEachAccountThenAtThatDaysPrices(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	// July closes at 1000; 500 comes in on 8/10; August's statement closes at 1500, September's at 1200.
	storeDeposit(t, pool, 7, "1000", depositLine{day: 5, amount: "1000", after: "1000", description: "薪資"})
	storeDeposit(t, pool, 8, "1500", depositLine{day: 10, amount: "500", after: "1500", description: "轉入"})
	storeDeposit(t, pool, 9, "1200", depositLine{day: 20, amount: "-300", after: "1200", description: "轉出"})
	// US$10 from 8/31, worth NT$30 a dollar until a rate of 9/15 says 32.
	store(t, pool, "dollars.eml", pt.Response("esun.deposit", pt.Deposit("esun", pt.Day(8, 1), pt.Day(8, 31),
		pt.DepositAccount("7777777***777", "外幣活存", []*parserv2.Balance{pt.Balance("USD", pt.Day(8, 31), "10")}))))
	rate(t, pool, "2026-08-01", "USD", "0.0333333333333333333")
	rate(t, pool, "2026-09-15", "USD", "0.03125")

	on := func(day string) NetWorth {
		t.Helper()
		return decode[NetWorth](t, get(t, e, "/api/net-worth?on="+day))
	}
	// Before anything was reported, nothing; 8/15 is July's 1000 and the 500 of 8/10 since.
	if n := on("2026-06-30"); n.Net != "0.00" || len(n.ByKind) != 0 {
		t.Fatalf("on 6/30 = %+v", n)
	}
	if n := on("2026-08-15"); n.Net != "1500.00" {
		t.Fatalf("on 8/15 = %+v", n)
	}
	if n := on("2026-09-10"); n.Net != "1800.00" { // 1500 + US$10 at 30
		t.Fatalf("on 9/10 = %+v", n)
	}
	if n := on("2026-09-30"); n.Net != "1520.00" { // 1200 + US$10 at 32
		t.Fatalf("on 9/30 = %+v", n)
	}

	history := decode[[]NetWorthPoint](t, get(t, e, "/api/net-worth/history?from=2026-07-15&to=2026-09-30"))
	var got []string
	for _, p := range history {
		got = append(got, fmt.Sprintf("%s %s %v", p.On, p.Net, p.Complete))
	}
	// Month ends; the dollars' account last reported on 8/31, so only up to then is exact.
	want := []string{"2026-07-31 1000.00 true", "2026-08-31 1800.00 true", "2026-09-30 1520.00 false"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("history = %v, want %v", got, want)
	}
	if daily := decode[[]NetWorthPoint](t, get(t, e, "/api/net-worth/history?from=2026-09-01&to=2026-09-03&step=day")); len(daily) != 3 {
		t.Fatalf("daily = %+v", daily)
	}
	for _, bad := range []string{"from=2026-09-30&to=2026-09-01", "from=2020-01-01&to=2026-12-31&step=day"} {
		if got := get(t, e, "/api/net-worth/history?"+bad).Code; got != http.StatusBadRequest {
			t.Errorf("%s: status = %d", bad, got)
		}
	}
}

func TestTheOwnerRecordsWhatIsHeldOrOwedAsAPositiveNumber(t *testing.T) {
	e, _ := newAPI(t, 1<<20)
	loan := open(t, e, AccountKindLoan, "房貸")
	points := open(t, e, AccountKindPoints, "航空")
	refused := func(id int64, holdings, says string) {
		t.Helper()
		got := send(t, e, http.MethodPut, fmt.Sprintf("/api/accounts/%d/holdings", id), `{"holdings": `+holdings+`}`)
		if got.Code != http.StatusBadRequest || !strings.Contains(got.Body.String(), says) {
			t.Fatalf("%s: %d %s, want 400 saying %q", holdings, got.Code, got.Body, says)
		}
	}
	// A negative principal would count the loan as an asset.
	refused(loan, `[{"unit": "TWD", "amount": "-800000", "expiries": []}]`, "what is owed as a positive number")
	refused(points, `[{"unit": "哩程", "amount": "-1", "expiries": []}]`, "what is held as a positive number")
	refused(points, `[{"unit": "哩程", "amount": "1000", "expiries": [{"amount": "-100", "by": "2027-01-31"}]}]`, "what lapses")
	if net := decode[NetWorth](t, get(t, e, "/api/net-worth")); net.Net != "0.00" {
		t.Fatalf("net worth after refusals = %+v", net)
	}
}

func TestAPositionKeepsTheFractionOfAShare(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	store(t, pool, "fractional.eml", pt.Response("skis.securities", pt.Securities("skis", "12**345", pt.Day(7, 1), pt.Day(8, 4),
		[]*parserv2.Balance{pt.Balance("範例基金", pt.Day(8, 4), "10.5")})))
	account := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), "12**345")
	detail := accountOf(t, e, account.Id)
	if detail.Positions == nil || len(*detail.Positions) != 1 || (*detail.Positions)[0].Shares != "10.5" {
		t.Fatalf("positions = %+v", detail.Positions)
	}
}
