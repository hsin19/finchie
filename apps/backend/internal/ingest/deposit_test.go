package ingest

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/genproto/googleapis/type/date"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
	pt "github.com/hsin19/finchie/apps/backend/internal/parsertest"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

// sampleDeposit is synthetic (ADR 0006): a combined statement with an NT$ account that has lines and a
// US$ account that only has a balance.
func sampleDeposit() *parserv2.Statement {
	s := pt.Deposit("esun", pt.Day(9, 1), pt.Day(9, 30),
		pt.DepositAccount("1234567***890", "臺幣活存",
			[]*parserv2.Balance{pt.Balance("TWD", pt.Day(8, 31), "1000"), pt.Balance("TWD", pt.Day(9, 30), "51000")},
			pt.DepositLine(pt.Day(9, 5), "薪資", "TWD", "60000", "61000", pt.Memo("範例公司"), pt.Counterparty("812 0000000000***123")),
			pt.DepositLine(pt.Day(9, 10), "卡款扣繳", "TWD", "-10000", "51000"),
		),
		pt.DepositAccount("7654321***098", "外幣活存", []*parserv2.Balance{pt.Balance("USD", pt.Day(9, 30), "12.34")}),
	)
	s.Checks = []*parserv2.Check{pt.Check("臺幣活存 1234567***890 存款餘額", "TWD", "51000", "51000")}
	return s
}

func storeDeposit(t *testing.T, pool *pgxpool.Pool, name string, statement *parserv2.Statement) int64 {
	t.Helper()
	id := addDocument(t, pool, name)
	parser := &fakeParser{response: pt.Response("esun.deposit", statement)}
	if err := NewWorker(pool, parser, time.Minute).Work(t.Context(), job(id, 1, 6)); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestADepositStatementIsStoredInTheAccountsItCovers(t *testing.T) {
	pool := testdb.Pool(t)
	id := storeDeposit(t, pool, "september.eml", sampleDeposit())
	if got := document(t, pool, id).Status; got != domain.Parsed {
		t.Fatalf("status = %s", got)
	}
	ctx := t.Context()

	var kind string
	var periodStart, closing time.Time
	if err := pool.QueryRow(ctx, "SELECT kind, period_start, closing_date FROM statements").Scan(&kind, &periodStart, &closing); err != nil {
		t.Fatal(err)
	}
	if kind != string(domain.Deposit) || periodStart.Format(time.DateOnly) != "2026-09-01" || closing.Format(time.DateOnly) != "2026-09-30" {
		t.Fatalf("statement = %s %s..%s", kind, periodStart, closing)
	}

	rows, err := pool.Query(ctx, "SELECT kind, number, label FROM accounts ORDER BY number")
	if err != nil {
		t.Fatal(err)
	}
	var accounts []string
	for rows.Next() {
		var kind, number, label string
		if err := rows.Scan(&kind, &number, &label); err != nil {
			t.Fatal(err)
		}
		accounts = append(accounts, kind+" "+number+" "+label)
	}
	if strings.Join(accounts, "; ") != "deposit 1234567***890 臺幣活存; deposit 7654321***098 外幣活存" {
		t.Fatalf("accounts = %v", accounts)
	}

	var salary struct{ kind, unit, amount, balance, memo, counterparty string }
	if err := pool.QueryRow(ctx, `SELECT t.kind, u.symbol, t.amount::text, t.balance::text, t.memo, t.counterparty
		FROM transactions t JOIN accounts a ON a.id = t.account_id JOIN units u ON u.id = t.unit_id
		WHERE a.number = '1234567***890' AND t.line_no = 0`).Scan(
		&salary.kind, &salary.unit, &salary.amount, &salary.balance, &salary.memo, &salary.counterparty); err != nil {
		t.Fatal(err)
	}
	if salary.kind != "other" || salary.unit != "TWD" || salary.amount != "60000" || salary.balance != "61000" ||
		salary.memo != "範例公司" || salary.counterparty != "812 0000000000***123" {
		t.Fatalf("salary line = %+v", salary)
	}

	balances, err := pool.Query(ctx, `SELECT a.number, u.symbol, b.as_of, b.amount::text FROM balances b
		JOIN accounts a ON a.id = b.account_id JOIN units u ON u.id = b.unit_id ORDER BY a.number, b.as_of`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for balances.Next() {
		var number, unit, amount string
		var asOf time.Time
		if err := balances.Scan(&number, &unit, &asOf, &amount); err != nil {
			t.Fatal(err)
		}
		got = append(got, number+" "+unit+" "+asOf.Format(time.DateOnly)+" "+amount)
	}
	// The opening balance is the end of the day before the period; the US$ account has only its closing one.
	want := "1234567***890 TWD 2026-08-31 1000; 1234567***890 TWD 2026-09-30 51000; 7654321***098 USD 2026-09-30 12.34"
	if strings.Join(got, "; ") != want {
		t.Fatalf("balances = %v", got)
	}
}

func TestABanksCardBillAndDepositStatementClosingTheSameDayAreBothStored(t *testing.T) {
	pool := testdb.Pool(t)
	bill := sampleStatement(true)
	bill.Institution = "esun"
	bill.ClosingDate = pt.Day(9, 30)
	if err := NewWorker(pool, parsed(bill), time.Minute).Work(t.Context(), job(addDocument(t, pool, "bill.eml"), 1, 6)); err != nil {
		t.Fatal(err)
	}
	id := storeDeposit(t, pool, "deposit.eml", sampleDeposit())
	if got := document(t, pool, id).Status; got != domain.Parsed {
		t.Fatalf("deposit statement = %s, want parsed beside the bill", got)
	}
	if countRows(t, pool, "statements") != 2 {
		t.Fatal("both statements must be stored")
	}
}

// passbook is a synthetic (ADR 0006) on-demand export of one NT$ account, as Cathay's e-passbook is:
// its period runs from its first line to its last, its opening balance on the day before.
func passbook(from, to *date.Date, opening, closing string, lines ...*parserv2.Line) *parserv2.Statement {
	before := time.Date(int(from.GetYear()), time.Month(from.GetMonth()), int(from.GetDay())-1, 0, 0, 0, 0, time.UTC)
	dayBefore := &date.Date{Year: int32(before.Year()), Month: int32(before.Month()), Day: int32(before.Day())} //nolint:gosec // a date
	s := pt.Deposit("cathay", from, to, pt.DepositAccount("123-45-678901-2", "數位存款帳戶",
		[]*parserv2.Balance{pt.Balance("TWD", dayBefore, opening), pt.Balance("TWD", to, closing)}, lines...))
	s.Checks = []*parserv2.Check{pt.Check("餘額", "TWD", closing, closing)}
	return s
}

func on2026(month, day int32) *date.Date { return pt.Day(month, day) }

func passbookLine(month, day int32, description, amount, balance string) *parserv2.Line {
	return pt.DepositLine(on2026(month, day), description, "TWD", amount, balance)
}

// May to October: 10,000 before it; -500 → 9,500; +1,000 → 10,500; -200 → 10,300.
func mayToOctober() *parserv2.Statement {
	return passbook(on2026(5, 19), on2026(10, 29), "10000", "10300",
		passbookLine(5, 19, "五月卡款", "-500", "9500"),
		passbookLine(6, 19, "六月轉入(舊)", "1000", "10500"),
		passbookLine(10, 29, "十月轉出(舊)", "-200", "10300"),
	)
}

// June to November, sharing June to October with the export above: +1,000 → 10,500; -200 → 10,300;
// +3,000 → 13,300.
func juneToNovember() *parserv2.Statement {
	return passbook(on2026(6, 19), on2026(11, 19), "9500", "13300",
		passbookLine(6, 19, "六月轉入(新)", "1000", "10500"),
		passbookLine(10, 29, "十月轉出(新)", "-200", "10300"),
		passbookLine(11, 19, "十一月薪資", "3000", "13300"),
	)
}

// checkLedger compares every line and balance stored, oldest first, and requires no continuity gap.
func checkLedger(t *testing.T, pool *pgxpool.Pool, wantLines, wantBalances string) {
	t.Helper()
	ctx := t.Context()
	scan := func(query string) string {
		t.Helper()
		rows, err := pool.Query(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var day time.Time
			var rest string
			if err := rows.Scan(&day, &rest); err != nil {
				t.Fatal(err)
			}
			got = append(got, day.Format(time.DateOnly)+" "+rest)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return strings.Join(got, "; ")
	}
	if got := scan("SELECT transaction_date, description || ' ' || amount::text FROM transactions ORDER BY transaction_date, id"); got != wantLines {
		t.Fatalf("lines = %q, want %q", got, wantLines)
	}
	if got := scan("SELECT as_of, amount::text FROM balances ORDER BY as_of, id"); got != wantBalances {
		t.Fatalf("balances = %q, want %q", got, wantBalances)
	}
	gaps, err := ledger.Gaps(ctx, dbq.New(pool), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 0 {
		t.Fatalf("gaps = %+v, want none", gaps)
	}
}

// Over the days two exports share, the one closing later stands: May stays from the first, June to
// November comes from the second.
const (
	sharedLines    = "2026-05-19 五月卡款 -500; 2026-06-19 六月轉入(新) 1000; 2026-10-29 十月轉出(新) -200; 2026-11-19 十一月薪資 3000"
	sharedBalances = "2026-05-18 10000; 2026-06-18 9500; 2026-11-19 13300"
)

func TestOverlappingExportsKeepTheSharedDaysOfTheOneClosingLater(t *testing.T) {
	pool := testdb.Pool(t)
	older := storeDeposit(t, pool, "may-oct.eml", mayToOctober())
	newer := storeDeposit(t, pool, "jun-nov.eml", juneToNovember())
	if document(t, pool, older).Status != domain.Parsed || document(t, pool, newer).Status != domain.Parsed {
		t.Fatal("both overlapping exports must be parsed")
	}
	checkLedger(t, pool, sharedLines, sharedBalances)

	// Re-parsing the export closing earlier changes nothing.
	parser := &fakeParser{response: pt.Response("cathay.deposit", mayToOctober())}
	if err := NewWorker(pool, parser, time.Minute).Work(t.Context(), job(older, 1, 6)); err != nil {
		t.Fatal(err)
	}
	checkLedger(t, pool, sharedLines, sharedBalances)

	// One closing the same day but starting earlier is no duplicate, and as the later document it keeps
	// every day it covers, its opening balance with them.
	whole := storeDeposit(t, pool, "may-nov.eml", passbook(on2026(5, 19), on2026(11, 19), "10000", "13300",
		passbookLine(5, 19, "五月卡款", "-500", "9500"),
		passbookLine(6, 19, "六月轉入(全)", "1000", "10500"),
		passbookLine(10, 29, "十月轉出(全)", "-200", "10300"),
		passbookLine(11, 19, "十一月薪資", "3000", "13300"),
	))
	if got := document(t, pool, whole).Status; got != domain.Parsed {
		t.Fatalf("status = %s, want parsed for a wider period ending on the same day", got)
	}
	checkLedger(t, pool,
		"2026-05-19 五月卡款 -500; 2026-06-19 六月轉入(全) 1000; 2026-10-29 十月轉出(全) -200; 2026-11-19 十一月薪資 3000",
		"2026-05-18 10000; 2026-11-19 13300",
	)
}

func TestOverlappingExportsStoredTheOtherWayRoundEndTheSame(t *testing.T) {
	pool := testdb.Pool(t)
	storeDeposit(t, pool, "jun-nov.eml", juneToNovember())
	storeDeposit(t, pool, "may-oct.eml", mayToOctober())
	checkLedger(t, pool, sharedLines, sharedBalances)
}

func TestAdjoiningStatementsKeepEachOthersBalancesWhicheverArrivesFirst(t *testing.T) {
	pool := testdb.Pool(t)
	september := sampleDeposit()
	october := sampleDeposit()
	october.PeriodStart, october.ClosingDate = pt.Day(10, 1), pt.Day(10, 31)
	october.Accounts = october.Accounts[:1]
	october.Accounts[0].Balances = []*parserv2.Balance{pt.Balance("TWD", pt.Day(9, 30), "51000"), pt.Balance("TWD", pt.Day(10, 31), "51000")}
	october.Accounts[0].Lines = nil

	// The older one arrives last, as a backfill does: October's opening balance, on September's last
	// day, is October's and stays.
	storeDeposit(t, pool, "october.eml", october)
	storeDeposit(t, pool, "september.eml", september)
	var n int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM balances b JOIN units u ON u.id = b.unit_id WHERE b.as_of = '2026-09-30' AND u.symbol = 'TWD'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("balances on 2026-09-30 = %d, want September's closing and October's opening", n)
	}
	if countRows(t, pool, "transactions") != 2 {
		t.Fatal("September's lines must stay")
	}
}

func TestAnAccountWhoseLinesDoNotAddUpPutsTheStatementUpForReviewWithTheCheckThatFailed(t *testing.T) {
	pool := testdb.Pool(t)
	statement := sampleDeposit()
	statement.Checks[0] = pt.Check("臺幣活存 1234567***890 存款餘額", "TWD", "51000", "50000")
	doc := document(t, pool, storeDeposit(t, pool, "off.eml", statement))
	if doc.Status != domain.NeedsReview || len(doc.Warnings) != 0 {
		t.Fatalf("document = %s %q", doc.Status, doc.Warnings)
	}
	var name, computed, stated string
	var balanced bool
	if err := pool.QueryRow(t.Context(), "SELECT name, computed::text, stated::text, balanced FROM statement_checks").Scan(&name, &computed, &stated, &balanced); err != nil {
		t.Fatal(err)
	}
	if name != "臺幣活存 1234567***890 存款餘額" || computed != "51000" || stated != "50000" || balanced {
		t.Fatalf("check = %s %s %s %v", name, computed, stated, balanced)
	}
}

func TestConvertRejectsDepositStatementsThatBreakTheContract(t *testing.T) {
	cases := map[string]func(*parserv2.Statement){
		"missing closing date":         func(d *parserv2.Statement) { d.ClosingDate = nil },
		"no accounts":                  func(d *parserv2.Statement) { d.Accounts = nil },
		"account without a number":     func(d *parserv2.Statement) { d.Accounts[0].Number = "" },
		"same account twice":           func(d *parserv2.Statement) { d.Accounts[1].Number = d.Accounts[0].Number },
		"a balance without its unit":   func(d *parserv2.Statement) { d.Accounts[1].Balances[0].Unit = "" },
		"a balance without its amount": func(d *parserv2.Statement) { d.Accounts[1].Balances[0].Amount = nil },
		"unspecified kind": func(d *parserv2.Statement) {
			d.Accounts[0].Lines[0].Kind = parserv2.LineKind_LINE_KIND_UNSPECIFIED
		},
		"a purchase on a deposit account": func(d *parserv2.Statement) {
			d.Accounts[0].Lines[0].Kind = parserv2.LineKind_LINE_KIND_PURCHASE
		},
		"unparsable running balance": func(d *parserv2.Statement) {
			d.Accounts[0].Lines[0].GetDeposit().Balance = pt.Dec("61,000")
		},
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			d := sampleDeposit()
			breakIt(d)
			if _, err := convertResponse(1, pt.Response("esun.deposit", d)); !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("err = %v, want ErrInvalidResponse", err)
			}
		})
	}
}

func TestAResponseMustCarryAStatement(t *testing.T) {
	if _, err := convertResponse(1, &parserv2.ParseDocumentResponse{Parser: "esun.deposit"}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("err = %v", err)
	}
}
