package rates

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/riverqueue/river"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

// The feed's shape; the figures are made up.
const sample = `{"date": "2026-11-07", "twd": {"twd": 1, "usd": 0.030503548, "USDT": 0.0305, "btc": 4.0260888e-7, "xyz": 0, "eur": 0.027}}`

func TestRatesComeFromTheMirrorWhenTheFirstFeedFailsAndAreKeptExactly(t *testing.T) {
	pool := testdb.Pool(t)
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer down.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sample))
	}))
	defer mirror.Close()

	// The units some account holds or held; not EUR.
	if _, err := dbq.New(pool).EnsureUnits(t.Context(), dbq.EnsureUnitsParams{Kind: "money", Symbols: []string{"USD", "USDT", "BTC", "XYZ"}}); err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(pool, mirror.Client(), []string{down.URL, mirror.URL})
	for range 2 { // a second run the same day replaces, not duplicates
		if err := worker.Work(t.Context(), &river.Job[FetchArgs]{}); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := pool.Query(t.Context(), `
		SELECT to_char(p.as_of AT TIME ZONE 'Asia/Taipei', 'YYYY-MM-DD HH24:MI') || ' ' || p.source || ' ' || p.amount::text || ' '
		       || u.symbol || ' = ' || p.quote_amount::text || ' ' || q.symbol
		FROM prices p JOIN units u ON u.id = p.unit_id JOIN units q ON q.id = p.quote_unit_id
		WHERE u.kind = 'money'
		ORDER BY u.symbol`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	// Upper case, the rate as printed, a tiny one in exponent notation exactly, as of the start of the
	// feed's day in Taipei; no zero rate, since nothing could be divided by it, no NT$ in itself, and no
	// unit no account holds.
	want := []string{
		"2026-11-07 00:00 fawaz 0.00000040260888 BTC = 1 TWD",
		"2026-11-07 00:00 fawaz 0.030503548 USD = 1 TWD",
		"2026-11-07 00:00 fawaz 0.0305 USDT = 1 TWD",
	}
	if len(got) != len(want) {
		t.Fatalf("rates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rates = %v, want %v", got, want)
		}
	}
}

func TestNoFeedMeansAnErrorForRiverToRetry(t *testing.T) {
	pool := testdb.Pool(t)
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"date": "not a date"}`))
	}))
	defer broken.Close()
	if err := NewWorker(pool, broken.Client(), []string{broken.URL}).Work(t.Context(), &river.Job[FetchArgs]{}); err == nil {
		t.Fatal("want an error")
	}
}
