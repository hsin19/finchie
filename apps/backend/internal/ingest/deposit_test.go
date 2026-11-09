package ingest

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/genproto/googleapis/type/decimal"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	parserv1 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v1"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

// sampleDeposit is synthetic (ADR 0006): a combined statement with an NT$ account that has lines and a
// US$ account that only has a balance.
func sampleDeposit() *parserv1.DepositStatement {
	return &parserv1.DepositStatement{
		Institution: "esun",
		PeriodStart: &date.Date{Year: 2026, Month: 9, Day: 1},
		PeriodEnd:   &date.Date{Year: 2026, Month: 9, Day: 30},
		Accounts: []*parserv1.DepositAccount{
			{
				Number: "1234567***890", Name: "臺幣活存", Currency: "TWD",
				OpeningBalance: &decimal.Decimal{Value: "1000"},
				ClosingBalance: &decimal.Decimal{Value: "51000"},
				Lines: []*parserv1.DepositLine{
					{
						Date: &date.Date{Year: 2026, Month: 9, Day: 5}, Description: "薪資", Memo: "範例公司",
						Counterparty: "812 0000000000***123", Amount: &decimal.Decimal{Value: "60000"},
						Balance: &decimal.Decimal{Value: "61000"}, Kind: parserv1.DepositLineKind_DEPOSIT_LINE_KIND_OTHER,
					},
					{
						Date: &date.Date{Year: 2026, Month: 9, Day: 10}, Description: "卡款扣繳",
						Amount: &decimal.Decimal{Value: "-10000"}, Balance: &decimal.Decimal{Value: "51000"},
						Kind: parserv1.DepositLineKind_DEPOSIT_LINE_KIND_OTHER,
					},
				},
				Reconciliation: &parserv1.Reconciliation{
					Balanced: true, Computed: &decimal.Decimal{Value: "51000"}, Stated: &decimal.Decimal{Value: "51000"}, StatedLabel: "存款餘額",
				},
			},
			{Number: "7654321***098", Name: "外幣活存", Currency: "USD", ClosingBalance: &decimal.Decimal{Value: "12.34"}},
		},
	}
}

func storeDeposit(t *testing.T, pool *pgxpool.Pool, name string, statement *parserv1.DepositStatement) int64 {
	t.Helper()
	id := addDocument(t, pool, name)
	parser := &fakeParser{response: &parserv1.ParseDocumentResponse{Parser: "esun.deposit", ParserVersion: "1", DepositStatement: statement}}
	if err := NewWorker(pool, parser, time.Minute).Work(t.Context(), job(id, 1, 6)); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestADepositStatementIsStoredInTheAccountsItCovers(t *testing.T) {
	pool := testdb.Pool(t)
	id := storeDeposit(t, pool, "september.eml", sampleDeposit())
	if got := document(t, pool, id).Status; got != StatusParsed {
		t.Fatalf("status = %s", got)
	}
	ctx := t.Context()

	var kind string
	var periodStart, closing time.Time
	if err := pool.QueryRow(ctx, "SELECT kind, period_start, closing_date FROM statements").Scan(&kind, &periodStart, &closing); err != nil {
		t.Fatal(err)
	}
	if kind != KindDeposit || periodStart.Format(time.DateOnly) != "2026-09-01" || closing.Format(time.DateOnly) != "2026-09-30" {
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
	bill.ClosingDate = &date.Date{Year: 2026, Month: 9, Day: 30}
	if err := NewWorker(pool, parsed(bill), time.Minute).Work(t.Context(), job(addDocument(t, pool, "bill.eml"), 1, 6)); err != nil {
		t.Fatal(err)
	}
	id := storeDeposit(t, pool, "deposit.eml", sampleDeposit())
	if got := document(t, pool, id).Status; got != StatusParsed {
		t.Fatalf("deposit statement = %s, want parsed beside the bill", got)
	}
	if countRows(t, pool, "statements") != 2 {
		t.Fatal("both statements must be stored")
	}
}

// passbook is a synthetic (ADR 0006) on-demand export of one NT$ account, as Cathay's e-passbook is:
// its period runs from its first line to its last.
func passbook(from, to *date.Date, opening, closing string, lines ...*parserv1.DepositLine) *parserv1.DepositStatement {
	return &parserv1.DepositStatement{
		Institution: "cathay",
		PeriodStart: from,
		PeriodEnd:   to,
		Accounts: []*parserv1.DepositAccount{{
			Number: "123-45-678901-2", Name: "數位存款帳戶", Currency: "TWD",
			OpeningBalance: &decimal.Decimal{Value: opening},
			ClosingBalance: &decimal.Decimal{Value: closing},
			Lines:          lines,
			Reconciliation: &parserv1.Reconciliation{
				Balanced: true, Computed: &decimal.Decimal{Value: closing}, Stated: &decimal.Decimal{Value: closing}, StatedLabel: "餘額",
			},
		}},
	}
}

func on2026(month, day int32) *date.Date {
	return &date.Date{Year: 2026, Month: month, Day: day}
}

func passbookLine(month, day int32, description, amount, balance string) *parserv1.DepositLine {
	return &parserv1.DepositLine{
		Date: on2026(month, day), Description: description, Amount: &decimal.Decimal{Value: amount},
		Balance: &decimal.Decimal{Value: balance}, Kind: parserv1.DepositLineKind_DEPOSIT_LINE_KIND_OTHER,
	}
}

// May to October: 10,000 before it; -500 → 9,500; +1,000 → 10,500; -200 → 10,300.
func mayToOctober() *parserv1.DepositStatement {
	return passbook(on2026(5, 19), on2026(10, 29), "10000", "10300",
		passbookLine(5, 19, "五月卡款", "-500", "9500"),
		passbookLine(6, 19, "六月轉入(舊)", "1000", "10500"),
		passbookLine(10, 29, "十月轉出(舊)", "-200", "10300"),
	)
}

// June to November, sharing June to October with the export above: +1,000 → 10,500; -200 → 10,300;
// +3,000 → 13,300.
func juneToNovember() *parserv1.DepositStatement {
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
	gaps, err := dbq.New(pool).AccountGaps(ctx, nil)
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
	if document(t, pool, older).Status != StatusParsed || document(t, pool, newer).Status != StatusParsed {
		t.Fatal("both overlapping exports must be parsed")
	}
	checkLedger(t, pool, sharedLines, sharedBalances)

	// Re-parsing the export closing earlier changes nothing.
	parser := &fakeParser{response: &parserv1.ParseDocumentResponse{Parser: "cathay.deposit", ParserVersion: "1", DepositStatement: mayToOctober()}}
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
	if got := document(t, pool, whole).Status; got != StatusParsed {
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
	october.PeriodStart, october.PeriodEnd = &date.Date{Year: 2026, Month: 10, Day: 1}, &date.Date{Year: 2026, Month: 10, Day: 31}
	october.Accounts = october.Accounts[:1]
	october.Accounts[0].OpeningBalance = &decimal.Decimal{Value: "51000"}
	october.Accounts[0].Lines = nil
	october.Accounts[0].ClosingBalance = &decimal.Decimal{Value: "51000"}

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

func TestAnAccountWhoseLinesDoNotAddUpPutsTheStatementUpForReviewAndSaysWhich(t *testing.T) {
	pool := testdb.Pool(t)
	statement := sampleDeposit()
	statement.Accounts[0].Reconciliation = &parserv1.Reconciliation{
		Balanced: false, Computed: &decimal.Decimal{Value: "51000"}, Stated: &decimal.Decimal{Value: "50000"}, StatedLabel: "存款餘額",
	}
	doc := document(t, pool, storeDeposit(t, pool, "off.eml", statement))
	if doc.Status != StatusNeedsReview || len(doc.Warnings) != 1 ||
		doc.Warnings[0] != "臺幣活存 1234567***890 TWD: the lines come to 51000, 存款餘額 is 50000" {
		t.Fatalf("document = %s %q", doc.Status, doc.Warnings)
	}
}

func TestConvertRejectsDepositStatementsThatBreakTheContract(t *testing.T) {
	cases := map[string]func(*parserv1.DepositStatement){
		"missing period end":       func(d *parserv1.DepositStatement) { d.PeriodEnd = nil },
		"no accounts":              func(d *parserv1.DepositStatement) { d.Accounts = nil },
		"account without a number": func(d *parserv1.DepositStatement) { d.Accounts[0].Number = "" },
		"same account and currency twice": func(d *parserv1.DepositStatement) {
			d.Accounts[1].Number, d.Accounts[1].Currency = d.Accounts[0].Number, d.Accounts[0].Currency
		},
		"missing closing balance": func(d *parserv1.DepositStatement) { d.Accounts[1].ClosingBalance = nil },
		"unspecified kind": func(d *parserv1.DepositStatement) {
			d.Accounts[0].Lines[0].Kind = parserv1.DepositLineKind_DEPOSIT_LINE_KIND_UNSPECIFIED
		},
		"unparsable running balance": func(d *parserv1.DepositStatement) {
			d.Accounts[0].Lines[0].Balance = &decimal.Decimal{Value: "61,000"}
		},
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			d := sampleDeposit()
			breakIt(d)
			if _, err := convertDeposit(1, d); !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("err = %v, want ErrInvalidResponse", err)
			}
		})
	}
}

func TestAResponseMustCarryExactlyOneStatement(t *testing.T) {
	for name, response := range map[string]*parserv1.ParseDocumentResponse{
		"neither": {},
		"both":    {Statement: sampleStatement(true), DepositStatement: sampleDeposit()},
	} {
		if _, err := convertResponse(1, response); !errors.Is(err, ErrInvalidResponse) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
