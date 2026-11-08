package quotes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

// The lists' shapes; names, codes, and prices are made up (ADR 0006).
const (
	twse = `[{"Date":"1151106","Code":"1234","Name":"範例電子","ClosingPrice":"50.50"},
	         {"Date":"1151106","Code":"9999","Name":"沒人持有","ClosingPrice":"10.00"},
	         {"Date":"1151106","Code":"00999A","Name":"範例⼀號","ClosingPrice":"8.00"}]`
	tpex = `[{"Date":"1151106","SecuritiesCompanyCode":"5678","CompanyName":"範例航運","Close":"25.05 "},
	         {"Date":"1151106","SecuritiesCompanyCode":"5679","CompanyName":"範例停牌","Close":"--"}]`
)

func serve(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
}

// hold records an account holding these securities, as a stored statement would.
func hold(t *testing.T, queries *dbq.Queries, names ...string) {
	t.Helper()
	ctx := t.Context()
	doc, err := queries.UpsertDocument(ctx, dbq.UpsertDocumentParams{Sha256: []byte("x"), Filename: "x.eml", ContentType: "message/rfc822", Content: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	day := pgtype.Date{Time: time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC), Valid: true}
	statement, err := queries.InsertStatement(ctx, dbq.InsertStatementParams{DocumentID: doc.ID, Institution: "skis", Kind: "securities", ClosingDate: day, Balanced: true})
	if err != nil {
		t.Fatal(err)
	}
	account, err := queries.UpsertAccount(ctx, dbq.UpsertAccountParams{Institution: "skis", Kind: "securities", Number: "12**345"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		var shares pgtype.Numeric
		_ = shares.Scan("1000")
		if err := queries.InsertBalance(ctx, dbq.InsertBalanceParams{AccountID: account, Unit: name, AsOf: day, Amount: shares, StatementID: statement}); err != nil {
			t.Fatal(err)
		}
	}
}

func stored(t *testing.T, worker *Worker) []string {
	t.Helper()
	pool := worker.pool
	rows, err := pool.Query(t.Context(), "SELECT to_char(day, 'YYYY-MM-DD'), name, code, market, close::text FROM quotes ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var day, name, code, market, closing string
		if err := rows.Scan(&day, &name, &code, &market, &closing); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%s %s %s %s %s", day, name, code, market, closing))
	}
	return got
}

func TestTheClosesOfHeldSecuritiesAreKeptByNameFromBothExchanges(t *testing.T) {
	pool := testdb.Pool(t)
	hold(t, dbq.New(pool), "範例電子", "範例一號", "範例航運", "範例停牌")
	listed, otc := serve(twse), serve(tpex)
	defer listed.Close()
	defer otc.Close()
	worker := NewWorker(pool, listed.Client(), []Feed{{Market: "twse", URL: listed.URL}, {Market: "tpex", URL: otc.URL}})
	for range 2 { // a second run the same day replaces, not duplicates
		if err := worker.Work(t.Context(), &river.Job[FetchArgs]{}); err != nil {
			t.Fatal(err)
		}
	}
	// Names as statements spell them (一, not ⼀), closes as printed; nothing for a name no one holds,
	// nor for one that did not trade.
	want := []string{
		"2026-11-06 範例一號 00999A twse 8.00",
		"2026-11-06 範例航運 5678 tpex 25.05",
		"2026-11-06 範例電子 1234 twse 50.50",
	}
	if got := stored(t, worker); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("quotes = %v, want %v", got, want)
	}
}

func TestOneExchangeDownKeepsTheOthersClosesAndFailsForARetry(t *testing.T) {
	pool := testdb.Pool(t)
	hold(t, dbq.New(pool), "範例電子")
	listed := serve(twse)
	defer listed.Close()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer down.Close()
	worker := NewWorker(pool, listed.Client(), []Feed{{Market: "tpex", URL: down.URL}, {Market: "twse", URL: listed.URL}})
	if err := worker.Work(t.Context(), &river.Job[FetchArgs]{}); err == nil {
		t.Fatal("no error for River to retry")
	}
	if got := stored(t, worker); len(got) != 1 {
		t.Fatalf("quotes = %v, want the listed one", got)
	}
}

func TestNothingHeldAsksNothing(t *testing.T) {
	pool := testdb.Pool(t)
	asked := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked = true }))
	defer server.Close()
	if err := NewWorker(pool, server.Client(), []Feed{{Market: "twse", URL: server.URL}}).Work(t.Context(), &river.Job[FetchArgs]{}); err != nil || asked {
		t.Fatalf("err = %v, asked = %v", err, asked)
	}
}
