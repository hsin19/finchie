package ingest

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/genproto/googleapis/type/decimal"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	parserv1 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v1"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

// Synthetic names and figures (ADR 0006).

func dec(s string) *decimal.Decimal { return &decimal.Decimal{Value: s} }

func holding(name, shares string) *parserv1.SecuritiesHolding {
	return &parserv1.SecuritiesHolding{Name: name, Shares: dec(shares)}
}

func trade(month, day int32, name string, side parserv1.TradeSide, shares, amount, net string) *parserv1.SecuritiesTrade {
	return &parserv1.SecuritiesTrade{
		Date: &date.Date{Year: 2026, Month: month, Day: day}, Name: name, Side: side, Shares: dec(shares),
		Price: dec("10"), Category: "集買", Amount: dec(amount), Fee: dec("20"), Tax: dec("0"), Net: dec(net),
	}
}

// monthly is a statement for a month of 2026 whose holdings are as of the given day of the next month.
func monthly(month, holdingsDay int32, holdings []*parserv1.SecuritiesHolding, trades ...*parserv1.SecuritiesTrade) *parserv1.SecuritiesStatement {
	return &parserv1.SecuritiesStatement{
		Institution: "skis", AccountNumber: "12**345",
		PeriodStart:  &date.Date{Year: 2026, Month: month, Day: 1},
		PeriodEnd:    &date.Date{Year: 2026, Month: month, Day: 28},
		HoldingsDate: &date.Date{Year: 2026, Month: month + 1, Day: holdingsDay},
		Holdings:     holdings, Trades: trades,
		Reconciliation: &parserv1.Reconciliation{Balanced: true, Computed: dec("0"), Stated: dec("0"), StatedLabel: "應收付金額"},
	}
}

func storeSecurities(t *testing.T, pool *pgxpool.Pool, name string, statement *parserv1.SecuritiesStatement) {
	t.Helper()
	id := addDocument(t, pool, name)
	parser := &fakeParser{response: &parserv1.ParseDocumentResponse{Parser: "skis.securities", ParserVersion: "1", SecuritiesStatement: statement}}
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
		"SELECT unit || ' ' || to_char(as_of, 'MM-DD') || ' ' || amount::text FROM balances WHERE account_id = $1 ORDER BY as_of, unit", account)
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
	buy  = parserv1.TradeSide_TRADE_SIDE_BUY
	sell = parserv1.TradeSide_TRADE_SIDE_SELL
)

func TestASecuritiesStatementStoresTradesAsSharesAndHoldingsAsBalances(t *testing.T) {
	pool := testdb.Pool(t)
	storeSecurities(t, pool, "july.eml", monthly(7, 4,
		[]*parserv1.SecuritiesHolding{holding("範例一號", "1000"), holding("範例電子", "3500")},
		trade(7, 3, "範例電子", sell, "2000", "101000", "100554"),
		trade(7, 10, "範例一號", buy, "1000", "20000", "-20020"),
	))
	account := securitiesAccount(t, pool)

	rows, err := pool.Query(t.Context(), `
		SELECT t.unit, t.kind, t.amount::text, tr.amount::text, tr.net::text
		FROM transactions t JOIN trades tr ON tr.transaction_id = t.id
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
	june := monthly(6, 2, []*parserv1.SecuritiesHolding{holding("範例電子", "5500"), holding("範例航運", "1000")})
	july := monthly(7, 4, []*parserv1.SecuritiesHolding{holding("範例一號", "1000"), holding("範例電子", "3500")},
		trade(7, 3, "範例電子", sell, "2000", "101000", "100554"),
		trade(7, 10, "範例一號", buy, "1000", "20000", "-20020"),
		trade(7, 15, "範例航運", sell, "1000", "30000", "29900"),
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
	if gaps, err := dbq.New(pool).AccountGaps(t.Context(), &account); err != nil || len(gaps) != 0 {
		t.Fatalf("gaps = %+v (%v)", gaps, err)
	}
}

func TestSharesNoTradeExplainsAreAGap(t *testing.T) {
	pool := testdb.Pool(t)
	storeSecurities(t, pool, "june.eml", monthly(6, 2, []*parserv1.SecuritiesHolding{holding("範例電子", "5500")}))
	// A stock dividend of 100 shares: no trade says so.
	storeSecurities(t, pool, "july.eml", monthly(7, 4, []*parserv1.SecuritiesHolding{holding("範例電子", "3600")},
		trade(7, 3, "範例電子", sell, "2000", "101000", "100554"),
	))
	account := securitiesAccount(t, pool)
	gaps, err := dbq.New(pool).AccountGaps(t.Context(), &account)
	if err != nil || len(gaps) == 0 {
		t.Fatalf("gaps = %+v (%v), want one", gaps, err)
	}
	if unseen, _ := gaps[0].Unseen.Value(); len(gaps) != 1 || gaps[0].Unit != "範例電子" || unseen != "100" {
		t.Fatalf("gaps = %+v", gaps)
	}
}
