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
		SELECT to_char(b.as_of, 'YYYY-MM-DD') || ' ' || b.unit || ' ' || b.amount::text
		FROM balances b JOIN accounts a ON a.id = b.account_id
		WHERE a.institution = 'test' AND a.kind = 'exchange' AND b.statement_id IS NULL
		UNION ALL
		SELECT to_char(v.as_of, 'YYYY-MM-DD') || ' = ' || v.amount::text || ' ' || v.unit
		FROM valuations v JOIN accounts a ON a.id = v.account_id
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
		if err := Store(ctx, pool, "test", day(on), snapshot); err != nil {
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

type fake struct {
	snapshot Snapshot
	err      error
}

func (fake) Institution() string { return "test" }

func (f fake) Fetch(context.Context) (Snapshot, error) { return f.snapshot, f.err }

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
