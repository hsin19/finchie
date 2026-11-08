package ingest

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/genproto/googleapis/type/decimal"

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
	if err := pool.QueryRow(ctx, `SELECT t.kind, t.unit, t.amount::text, t.balance::text, t.memo, t.counterparty
		FROM transactions t JOIN accounts a ON a.id = t.account_id
		WHERE a.number = '1234567***890' AND t.line_no = 0`).Scan(
		&salary.kind, &salary.unit, &salary.amount, &salary.balance, &salary.memo, &salary.counterparty); err != nil {
		t.Fatal(err)
	}
	if salary.kind != "other" || salary.unit != "TWD" || salary.amount != "60000" || salary.balance != "61000" ||
		salary.memo != "範例公司" || salary.counterparty != "812 0000000000***123" {
		t.Fatalf("salary line = %+v", salary)
	}

	balances, err := pool.Query(ctx, `SELECT a.number, b.unit, b.as_of, b.amount::text FROM balances b
		JOIN accounts a ON a.id = b.account_id ORDER BY a.number, b.as_of`)
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
