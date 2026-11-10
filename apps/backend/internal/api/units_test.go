package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// price stores what a source says: `amount` of a unit was worth `quoteAmount` of a money unit at asOf. The
// units are found or created as a source reporting them would.
func price(t *testing.T, pool *pgxpool.Pool, kind, symbol, source string, asOf time.Time, amount, quote, quoteAmount string) {
	t.Helper()
	ctx := t.Context()
	queries := dbq.New(pool)
	if _, err := queries.EnsureUnits(ctx, dbq.EnsureUnitsParams{Kind: kind, Symbols: []string{symbol}}); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.EnsureUnits(ctx, dbq.EnsureUnitsParams{Kind: "money", Symbols: []string{quote}}); err != nil {
		t.Fatal(err)
	}
	var a, q pgtype.Numeric
	if err := a.Scan(amount); err != nil {
		t.Fatal(err)
	}
	if err := q.Scan(quoteAmount); err != nil {
		t.Fatal(err)
	}
	if err := queries.UpsertPrices(ctx, dbq.UpsertPricesParams{
		Source: source, Kind: kind, Symbols: []string{symbol}, AsOf: []pgtype.Timestamptz{{Time: asOf, Valid: true}},
		Amounts: []pgtype.Numeric{a}, Quotes: []string{quote}, QuoteAmounts: []pgtype.Numeric{q},
	}); err != nil {
		t.Fatal(err)
	}
}

// rate stores the feed's rate of a unit on a day: how much of it one NT$ buys (ADR 0015).
func rate(t *testing.T, pool *pgxpool.Pool, day, unit, perTWD string) {
	t.Helper()
	d, err := time.Parse(time.DateOnly, day)
	if err != nil {
		t.Fatal(err)
	}
	price(t, pool, "money", unit, "fawaz", taipei.At(d, 0, 0), perTWD, "TWD", "1")
}

func unitNamed(t *testing.T, e *echo.Echo, kind, symbol string) Unit {
	t.Helper()
	for _, u := range decode[[]Unit](t, get(t, e, "/api/units?kind="+kind)) {
		if u.Symbol == symbol {
			return u
		}
	}
	t.Fatalf("no %s unit %s", kind, symbol)
	return Unit{}
}

func TestTheNewestPriceCountsWhateverItsSource(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	storeDollars(t, pool)
	rate(t, pool, "2026-11-07", "USD", "0.03125") // 1 USD is NT$32
	dollars := func() Balance {
		t.Helper()
		return accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), "7777777***777").Balances[0]
	}
	if b := dollars(); *b.Worth != "320.00" || b.Price.Source != Fawaz || *b.Price.Twd != "32" || b.Price.Quote != "TWD" ||
		!b.Price.AsOf.Equal(time.Date(2026, 11, 6, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("at the rate: %+v, price %+v", b, b.Price)
	}

	usd := unitNamed(t, e, "money", "USD")
	set := func(id int64, body string, want int) {
		t.Helper()
		if got := send(t, e, http.MethodPut, fmt.Sprintf("/api/units/%d/price", id), body); got.Code != want {
			t.Fatalf("%s: %d %s, want %d", body, got.Code, got.Body, want)
		}
	}
	// The owner's word, as of now, is newer than the feed's day: it counts.
	set(usd.Id, `{"value": "40"}`, http.StatusNoContent)
	if b := dollars(); *b.Worth != "400.00" || b.Price.Source != Owner || b.Price.Quoted != "40" {
		t.Fatalf("by the owner: %+v, price %+v", b, b.Price)
	}
	// Withdrawn, the feed's counts again.
	if got := send(t, e, http.MethodDelete, fmt.Sprintf("/api/units/%d/price", usd.Id), "").Code; got != http.StatusNoContent {
		t.Fatalf("clear: %d", got)
	}
	if b := dollars(); *b.Worth != "320.00" || b.Price.Source != Fawaz {
		t.Fatalf("cleared: %+v", b)
	}
	// Withdrawn, not forgotten: what the owner said the unit was worth stays on record.
	var kept int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM prices WHERE unit_id = $1 AND source = 'owner' AND withdrawn_at IS NOT NULL", usd.Id).Scan(&kept); err != nil || kept != 1 {
		t.Fatalf("withdrawn owner prices kept = %d (%v), want 1", kept, err)
	}
	// Given again the same day, it counts again.
	set(usd.Id, `{"value": "40"}`, http.StatusNoContent)
	if b := dollars(); *b.Worth != "400.00" || b.Price.Source != Owner {
		t.Fatalf("given again: %+v", b)
	}
	// A rate of a later day is newer than what the owner said: it counts.
	rate(t, pool, taipei.Today(time.Now()).AddDate(0, 0, 1).Format(time.DateOnly), "USD", "0.02")
	if b := dollars(); *b.Worth != "500.00" || b.Price.Source != Fawaz {
		t.Fatalf("a later rate: %+v", b)
	}

	// A price in another unit goes through that unit's newest price in NT$, one step.
	airline := open(t, e, AccountKindPoints, "航空")
	hold(t, e, airline, `[{"unit": "哩程", "amount": "1000", "expiries": []}]`)
	miles := unitNamed(t, e, "points", "哩程")
	if a := accountOf(t, e, airline); a.Worth != nil || a.Balances[0].Price != nil || *a.Balances[0].UnitId != miles.Id {
		t.Fatalf("unpriced: %+v", a.Balances[0])
	}
	set(miles.Id, `{"value": "0.01", "quote": "usd"}`, http.StatusNoContent)
	if a := accountOf(t, e, airline); *a.Worth != "500.00" || a.Balances[0].Price.Quote != "USD" || *a.Balances[0].Price.Twd != "0.5" {
		t.Fatalf("in USD: worth %v, %+v", a.Worth, a.Balances[0].Price)
	}

	set(miles.Id, `{"value": "-1"}`, http.StatusBadRequest)
	set(miles.Id, `{"value": "1", "quote": "XYZ"}`, http.StatusBadRequest)                           // no account holds it
	set(usd.Id, `{"value": "1", "quote": "USD"}`, http.StatusBadRequest)                             // not in itself
	set(unitNamed(t, e, "money", "TWD").Id, `{"value": "1", "quote": "USD"}`, http.StatusBadRequest) // worth itself
	set(999999, `{"value": "1"}`, http.StatusNotFound)
}

func TestCardPointsStartAtTheOwnersWordOfNTOneEach(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	store(t, pool, "points", cathayBill(11, program("小樹點(信用卡)", "3253")))
	p := pointsOf(t, e)
	if p.Worth == nil || *p.Worth != "3253.00" || p.Balances[0].Price.Source != Owner || p.Balances[0].Price.Quoted != "1" {
		t.Fatalf("points: worth %v, %+v", p.Worth, p.Balances[0].Price)
	}
	// The programs the migration priced, among the units.
	for _, symbol := range []string{"小樹點(信用卡)", "e point", "台新Point(信用卡)"} {
		unitNamed(t, e, "points", symbol)
	}
}
