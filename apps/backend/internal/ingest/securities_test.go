package ingest

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
	pt "github.com/hsin19/finchie/apps/backend/internal/parsertest"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

// Synthetic names and figures (ADR 0006).

type held struct{ name, shares string }

func holding(name, shares string) held { return held{name, shares} }

// traded is a trade of 2026, its shares as printed, positive: the line takes them off when sold.
func traded(month, day int32, name string, side parserv2.LineKind, shares, amount, net string) *parserv2.Line {
	if side == sell {
		shares = "-" + shares
	}
	return pt.Trade(pt.Day(month, day), name, side, shares, "10", amount, "20", "0", net)
}

// monthly is a statement for a month of 2026 whose holdings are as of the given day of the next month.
func monthly(month, holdingsDay int32, holdings []held, trades ...*parserv2.Line) *parserv2.Statement {
	on := pt.Day(month+1, holdingsDay)
	balances := make([]*parserv2.Balance, 0, len(holdings))
	for _, h := range holdings {
		balances = append(balances, pt.Balance(h.name, on, h.shares))
	}
	s := pt.Securities("skis", "12**345", pt.Day(month, 1), on, balances, trades...)
	s.Checks = []*parserv2.Check{pt.Check("應收付金額", "TWD", "0", "0")}
	return s
}

func storeSecurities(t *testing.T, pool *pgxpool.Pool, name string, statement *parserv2.Statement) {
	t.Helper()
	id := addDocument(t, pool, name)
	parser := &fakeParser{response: pt.Response("skis.securities", statement)}
	if err := NewWorker(pool, parser, time.Minute).Work(t.Context(), job(id, 1, 6)); err != nil {
		t.Fatal(err)
	}
}

func securitiesAccount(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(t.Context(), "SELECT id FROM accounts WHERE kind = 'securities'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// strings of each balance of the account, "unit as_of amount", in order.
func holdingsOf(t *testing.T, pool *pgxpool.Pool, account int64) []string {
	t.Helper()
	rows, err := pool.Query(t.Context(),
		`SELECT u.symbol || ' ' || to_char(b.as_of, 'MM-DD') || ' ' || b.amount::text
		 FROM balances b JOIN units u ON u.id = b.unit_id
		 WHERE b.account_id = $1 AND u.kind = 'security' ORDER BY b.as_of, u.symbol`, account)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		got = append(got, s)
	}
	return got
}

var (
	buy  = parserv2.LineKind_LINE_KIND_BUY
	sell = parserv2.LineKind_LINE_KIND_SELL
)

func TestASecuritiesStatementStoresTradesAsSharesAndHoldingsAsBalances(t *testing.T) {
	pool := testdb.Pool(t)
	storeSecurities(t, pool, "july.eml", monthly(7, 4,
		[]held{holding("範例一號", "1000"), holding("範例電子", "3500")},
		traded(7, 3, "範例電子", sell, "2000", "101000", "100554"),
		traded(7, 10, "範例一號", buy, "1000", "20000", "-20020"),
	))
	account := securitiesAccount(t, pool)

	rows, err := pool.Query(t.Context(), `
		SELECT u.symbol, t.kind, t.amount::text, tr.amount::text, tr.net::text
		FROM transactions t JOIN trades tr ON tr.transaction_id = t.id JOIN units u ON u.id = t.unit_id
		WHERE t.account_id = $1 ORDER BY t.line_no`, account)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for rows.Next() {
		var unit, kind, shares, amount, net string
		if err := rows.Scan(&unit, &kind, &shares, &amount, &net); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, fmt.Sprintf("%s %s %s %s %s", unit, kind, shares, amount, net))
	}
	// Shares sold leave the account; the money stays as the statement prints it.
	want := []string{"範例電子 sell -2000 101000 100554", "範例一號 buy 1000 20000 -20020"}
	if fmt.Sprint(lines) != fmt.Sprint(want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
	if got := holdingsOf(t, pool, account); fmt.Sprint(got) != fmt.Sprint([]string{"範例一號 08-04 1000", "範例電子 08-04 3500"}) {
		t.Fatalf("holdings = %v", got)
	}
}

func TestASecurityAStatementDoesNotListIsAtZeroWhicheverStatementArrivesFirst(t *testing.T) {
	pool := testdb.Pool(t)
	june := monthly(6, 2, []held{holding("範例電子", "5500"), holding("範例航運", "1000")})
	july := monthly(7, 4, []held{holding("範例一號", "1000"), holding("範例電子", "3500")},
		traded(7, 3, "範例電子", sell, "2000", "101000", "100554"),
		traded(7, 10, "範例一號", buy, "1000", "20000", "-20020"),
		traded(7, 15, "範例航運", sell, "1000", "30000", "29900"),
	)
	storeSecurities(t, pool, "july.eml", july)
	storeSecurities(t, pool, "june.eml", june)
	account := securitiesAccount(t, pool)

	want := []string{
		"範例一號 07-02 0", "範例航運 07-02 1000", "範例電子 07-02 5500",
		"範例一號 08-04 1000", "範例航運 08-04 0", "範例電子 08-04 3500",
	}
	if got := holdingsOf(t, pool, account); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("holdings = %v, want %v", got, want)
	}
	// Every change has its trade.
	if gaps, err := ledger.Gaps(t.Context(), dbq.New(pool), &account); err != nil || len(gaps) != 0 {
		t.Fatalf("gaps = %+v (%v)", gaps, err)
	}
}

func TestSharesNoTradeExplainsAreAGap(t *testing.T) {
	pool := testdb.Pool(t)
	storeSecurities(t, pool, "june.eml", monthly(6, 2, []held{holding("範例電子", "5500")}))
	// A stock dividend of 100 shares: no trade says so.
	storeSecurities(t, pool, "july.eml", monthly(7, 4, []held{holding("範例電子", "3600")},
		traded(7, 3, "範例電子", sell, "2000", "101000", "100554"),
	))
	account := securitiesAccount(t, pool)
	gaps, err := ledger.Gaps(t.Context(), dbq.New(pool), &account)
	if err != nil || len(gaps) == 0 {
		t.Fatalf("gaps = %+v (%v), want one", gaps, err)
	}
	if len(gaps) != 1 || gaps[0].Unit != "範例電子" || gaps[0].Unseen.RatString() != "100" {
		t.Fatalf("gaps = %+v", gaps)
	}
}
