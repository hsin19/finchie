package rates

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

// The feed's shape; the figures are made up.
const sample = `{"date": "2026-11-07", "twd": {"twd": 1, "usd": 0.030503548, "USDT": 0.0305, "btc": 4.0260888e-7, "xyz": 0}}`

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

	worker := NewWorker(pool, mirror.Client(), []string{down.URL, mirror.URL})
	for range 2 { // a second run the same day replaces, not duplicates
		if err := worker.Work(t.Context(), &river.Job[FetchArgs]{}); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := pool.Query(t.Context(), "SELECT day, unit, per_twd::text FROM exchange_rates ORDER BY unit")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var day time.Time
		var unit, rate string
		if err := rows.Scan(&day, &unit, &rate); err != nil {
			t.Fatal(err)
		}
		got = append(got, day.Format(time.DateOnly)+" "+unit+" "+rate)
	}
	// Upper case, the rate as printed, a tiny one in exponent notation exactly, and no zero rate: nothing
	// could be divided by it.
	want := []string{"2026-11-07 BTC 0.00000040260888", "2026-11-07 TWD 1", "2026-11-07 USD 0.030503548", "2026-11-07 USDT 0.0305"}
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
