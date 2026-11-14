package exchanges

import (
	"context"
	"errors"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

func held(pairs ...string) map[string]*big.Rat {
	holdings := map[string]*big.Rat{}
	for i := 0; i+1 < len(pairs); i += 2 {
		r, ok := new(big.Rat).SetString(pairs[i+1])
		if !ok {
			panic(pairs[i+1])
		}
		holdings[pairs[i]] = r
	}
	return holdings
}

func day(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

// rows lists an account's snapshot balances as "day unit amount", and its valuations as "day = amount unit".
func rows(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	result, err := pool.Query(t.Context(), `
		SELECT to_char(b.as_of, 'YYYY-MM-DD') || ' ' || u.symbol || ' ' || b.amount::text
		FROM balances b JOIN accounts a ON a.id = b.account_id JOIN units u ON u.id = b.unit_id
		WHERE a.institution = 'test' AND a.kind = 'exchange' AND b.statement_id IS NULL
		UNION ALL
		SELECT to_char(v.as_of, 'YYYY-MM-DD') || ' = ' || v.amount::text || ' ' || u.symbol
		FROM valuations v JOIN accounts a ON a.id = v.account_id JOIN units u ON u.id = v.unit_id
		WHERE a.institution = 'test'
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for result.Next() {
		var row string
		if err := result.Scan(&row); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	return got
}

func TestASnapshotReplacesTheDaysAndPutsWhatTheAccountNoLongerHoldsAtZero(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := t.Context()
	store := func(on string, snapshot Snapshot) {
		t.Helper()
		if err := Store(ctx, pool, "test", day(on), snapshot, History{}); err != nil {
			t.Fatal(err)
		}
	}
	store("2026-11-08", Snapshot{Holdings: held("BTC", "0.1", "USDT", "500", "DUST", "3"), Valuation: &Value{Unit: "USDT", Amount: big.NewRat(9000, 1)}})
	// A later fetch the same day replaces the day's snapshot, valuation included.
	store("2026-11-08", Snapshot{Holdings: held("BTC", "0.2", "USDT", "500", "DUST", "3"), Valuation: &Value{Unit: "USDT", Amount: big.NewRat(18000, 1)}})
	// Sold BTC and DUST: both at 0 that day. No valuation that day.
	store("2026-11-09", Snapshot{Holdings: held("USDT", "600", "ETH", "0")})
	// Nothing to put at 0 again.
	store("2026-11-10", Snapshot{Holdings: held("USDT", "600")})

	want := []string{
		"2026-11-08 = 18000 USDT",
		"2026-11-08 BTC 0.2",
		"2026-11-08 DUST 3",
		"2026-11-08 USDT 500",
		"2026-11-09 BTC 0",
		"2026-11-09 DUST 0",
		"2026-11-09 USDT 600",
		"2026-11-10 USDT 600",
	}
	if got := rows(t, pool); !slices.Equal(got, want) {
		t.Fatalf("rows =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestAnExchangesPricesAreKeptAsOfTheFetchOnePerCoinAndDay(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := t.Context()
	price := func(s string) *big.Rat { r, _ := new(big.Rat).SetString(s); return r }
	at := func(hour int) time.Time { return time.Date(2026, 11, 8, hour, 0, 0, 0, time.UTC) }
	store := func(at time.Time, btc string) {
		t.Helper()
		snapshot := Snapshot{Holdings: held("BTC", "0.1", "BGB", "10", "USDT", "5")}
		snapshot.price("BTC", "USDT", price(btc))
		snapshot.price("BGB", "USDT", price("0.85"))
		snapshot.price("USDT", "USDT", price("1")) // never in itself
		if err := Store(ctx, pool, "binance", at, snapshot, History{}); err != nil {
			t.Fatal(err)
		}
	}
	store(at(1), "100000")
	store(at(7), "101000.5") // later the same day in Taipei: replaces the day's
	result, err := pool.Query(ctx, `
		SELECT u.symbol || ' ' || to_char(p.as_of AT TIME ZONE 'Asia/Taipei', 'YYYY-MM-DD HH24:MI') || ' ' || p.source || ': '
		       || p.amount::text || ' = ' || p.quote_amount::text || ' ' || q.symbol
		FROM prices p JOIN units u ON u.id = p.unit_id JOIN units q ON q.id = p.quote_unit_id
		WHERE p.source = 'binance'
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for result.Next() {
		var row string
		if err := result.Scan(&row); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	want := []string{
		"BGB 2026-11-08 15:00 binance: 1 = 0.85 USDT",
		"BTC 2026-11-08 15:00 binance: 1 = 101000.5 USDT",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("prices =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

type fake struct {
	snapshot Snapshot
	err      error
	moves    []Movement
	// Each span the job asked for.
	spans *[]string
}

func (fake) Institution() string { return "test" }

func (f fake) Fetch(context.Context) (Snapshot, error) { return f.snapshot, f.err }

func (f fake) Movements(_ context.Context, from, to time.Time) ([]Movement, error) {
	if f.spans != nil {
		*f.spans = append(*f.spans, from.UTC().Format(time.DateOnly)+".."+to.UTC().Format(time.DateOnly))
	}
	var in []Movement
	for _, m := range f.moves {
		if !m.At.Before(from) && m.At.Before(to) {
			in = append(in, m)
		}
	}
	return in, nil
}

func TestTheJobStoresTheSnapshotOnTheDayInTaipeiAndNothingWhenTheExchangeFails(t *testing.T) {
	pool := testdb.Pool(t)
	// 01:00 on the 10th in Taipei.
	now := func() time.Time { return time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC) }
	job := &river.Job[FetchArgs]{Args: FetchArgs{Institution: "test"}}

	failing := NewWorker(pool, []Exchange{fake{err: errors.New("funding: 503")}}, now)
	if err := failing.Work(t.Context(), job); err == nil {
		t.Fatal("want an error for River to retry")
	}
	if got := rows(t, pool); len(got) != 0 {
		t.Fatalf("a failed fetch stored %v", got)
	}

	working := NewWorker(pool, []Exchange{fake{snapshot: Snapshot{Holdings: held("USDT", "1.5")}}}, now)
	if err := working.Work(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if got := rows(t, pool); !slices.Equal(got, []string{"2026-11-10 USDT 1.5"}) {
		t.Fatalf("rows = %v", got)
	}

	// An exchange whose keys were removed after its job was queued: nothing to do.
	if err := working.Work(t.Context(), &river.Job[FetchArgs]{Args: FetchArgs{Institution: "gone"}}); err != nil {
		t.Fatal(err)
	}
}

func TestTheJobReadsDepositsAndWithdrawalsAMonthAtATimeFromTwoYearsBackAndWaitsOnPendingOnes(t *testing.T) {
	pool := testdb.Pool(t)
	now := time.Date(2026, 11, 14, 4, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	pending := Movement{ID: "p", At: now.Add(-48 * time.Hour), Unit: "USD", Amount: big.NewRat(100, 1), Pending: true}
	var spans []string
	job := &river.Job[FetchArgs]{Args: FetchArgs{Institution: "test"}}
	work := func(moves ...Movement) {
		t.Helper()
		worker := NewWorker(pool, []Exchange{fake{snapshot: Snapshot{Holdings: held("USDT", "1")}, moves: moves, spans: &spans}}, clock)
		if err := worker.Work(t.Context(), job); err != nil {
			t.Fatal(err)
		}
	}
	// 25 spans of 30 days from two years back; one still pending two days ago is in the last.
	for range 25 {
		work(pending)
	}
	if want := []string{"2024-11-14..2024-12-14", "2024-12-14..2025-01-13"}; !slices.Equal(spans[:2], want) {
		t.Fatalf("first spans = %v, want %v", spans[:2], want)
	}
	if last := spans[len(spans)-1]; last != "2026-11-04..2026-11-14" {
		t.Fatalf("last span = %s", last)
	}
	// It holds the next read back to it, until it settles; then what came since the last read.
	work(pending)
	if got := spans[len(spans)-1]; got != "2026-11-12..2026-11-14" {
		t.Fatalf("after a pending one: %s, want a read from it", got)
	}
	pending.Pending = false
	work(pending)
	work(pending)
	if got := spans[len(spans)-1]; got != "2026-11-14..2026-11-14" {
		t.Fatalf("after it settled: %s", got)
	}
}

func TestSettledMovementsAreLinesAndTheirFeesLinesOfTheirOwn(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := t.Context()
	at := time.Date(2026, 1, 26, 12, 0, 0, 0, time.UTC)
	history := History{From: at.Add(-time.Hour), To: at, Movements: []Movement{
		{ID: "withdrawal/w3", At: at.Add(-30 * time.Minute), Unit: "usdt", Amount: big.NewRat(-317713, 100), Fee: big.NewRat(1, 1), FeeUnit: "max", TxID: "0x35ab45a3"},
		{ID: "deposit/d1", At: at.Add(-20 * time.Minute), Unit: "TWD", Amount: big.NewRat(100000, 1), Memo: "12345678901123"},
		{ID: "deposit/p", At: at.Add(-10 * time.Minute), Unit: "TWD", Amount: big.NewRat(5, 1), Pending: true},
	}}
	for range 2 { // stored twice: the same lines
		if err := Store(ctx, pool, "test", at, Snapshot{Holdings: held("USDT", "1")}, history); err != nil {
			t.Fatal(err)
		}
	}
	result, err := pool.Query(ctx, `
		SELECT t.external_id || ' ' || t.description || ' ' || t.kind || ' ' || t.amount::text || ' ' || u.symbol || ' ' || coalesce(t.memo, '')
		FROM transactions t JOIN units u ON u.id = t.unit_id
		WHERE t.external_id IS NOT NULL
		ORDER BY t.external_id`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for result.Next() {
		var row string
		if err := result.Scan(&row); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	want := []string{
		"deposit/d1 入金 other 100000 TWD 12345678901123",
		"withdrawal/w3 提幣 other -3177.13 USDT 0x35ab45a3",
		"withdrawal/w3/fee 提幣手續費 fee -1 MAX 0x35ab45a3",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("lines =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
