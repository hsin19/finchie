package ingest

import (
	"errors"
	"testing"

	"google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hsin19/finchie/apps/backend/internal/domain"
	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
	pt "github.com/hsin19/finchie/apps/backend/internal/parsertest"
)

func TestABillIsItsCardAccountsLinesWhatItOwesAsTheirBalanceAndItsChecks(t *testing.T) {
	result, err := convertResponse(7, pt.Response("hsbc.credit_card", sampleStatement(true)))
	if err != nil {
		t.Fatal(err)
	}
	statement := result.statement
	if statement.DocumentID != 7 || statement.Institution != "hsbc" || statement.Kind != domain.CreditCard || !statement.Balanced {
		t.Fatalf("statement = %+v", statement)
	}
	if got := statement.ClosingDate.Time.Format("2006-01-02"); got != "2026-10-21" {
		t.Fatalf("closing date = %s", got)
	}
	if len(result.accounts) != 1 || len(result.accounts[0].lines) != 2 {
		t.Fatalf("accounts = %+v", result.accounts)
	}
	card := result.accounts[0]
	shop, payment := card.lines[0], card.lines[1]
	if shop.Kind != domain.Purchase || shop.LineNo != 0 || *shop.OriginalCurrency != "EUR" || *shop.Country != "IT" {
		t.Fatalf("shop = %+v", shop)
	}
	if !shop.OriginalAmount.Valid || !shop.PostingDate.Valid {
		t.Fatal("shop should keep its original amount and posting date")
	}
	if payment.Kind != domain.Payment || payment.OriginalAmount.Valid || payment.PostingDate.Valid || payment.Country != nil {
		t.Fatalf("payment = %+v", payment)
	}
	// What the bill owes is its card account's balance on the closing date (ADR 0022).
	if len(card.balances) != 1 || card.balances[0].unit != "TWD" || !card.balances[0].AsOf.Time.Equal(statement.ClosingDate.Time) {
		t.Fatalf("balances = %+v", card.balances)
	}
	if len(result.checks.Names) != 1 || result.checks.Names[0] != "new charges" || !result.checks.Balanced[0] {
		t.Fatalf("checks = %+v", result.checks)
	}
}

func TestAStatementIsBalancedOnlyWhenEveryCheckIs(t *testing.T) {
	s := sampleStatement(true)
	s.Checks = append(s.Checks, pt.Check("payments", "TWD", "68019", "68000"))
	result, err := convertResponse(1, pt.Response("hsbc.credit_card", s))
	if err != nil {
		t.Fatal(err)
	}
	if result.statement.Balanced || len(result.checks.Names) != 2 {
		t.Fatalf("statement balanced = %v with checks %+v", result.statement.Balanced, result.checks)
	}
}

func TestConvertRejectsResponsesThatBreakTheContract(t *testing.T) {
	first := func(s *parserv2.Statement) *parserv2.Line { return s.Accounts[0].Lines[0] }
	cases := map[string]func(*parserv2.Statement){
		"missing closing date": func(s *parserv2.Statement) { s.ClosingDate = nil },
		"unparsable amount":    func(s *parserv2.Statement) { first(s).Amount = pt.Dec("12,000") },
		"unspecified kind":     func(s *parserv2.Statement) { first(s).Kind = parserv2.LineKind_LINE_KIND_UNSPECIFIED },
		"a line of another kind of account": func(s *parserv2.Statement) {
			first(s).Kind = parserv2.LineKind_LINE_KIND_INTEREST
		},
		"a check without its figure": func(s *parserv2.Statement) { s.Checks[0].Stated = nil },
		"NaN amount":                 func(s *parserv2.Statement) { first(s).Amount = pt.Dec("NaN") },
		"infinite total due":         func(s *parserv2.Statement) { s.CardBill.TotalDue = pt.Dec("Infinity") },
		"negative infinite original amount": func(s *parserv2.Statement) {
			first(s).GetCard().OriginalAmount = pt.Dec("-Infinity")
		},
		"a foreign amount without its currency": func(s *parserv2.Statement) { first(s).GetCard().OriginalCurrency = "" },
		"month 13":                              func(s *parserv2.Statement) { s.ClosingDate = &date.Date{Year: 2026, Month: 13, Day: 1} },
		"day 0":                                 func(s *parserv2.Statement) { s.CardBill.DueDate = &date.Date{Year: 2026, Month: 11} },
		"February 30":                           func(s *parserv2.Statement) { first(s).Date = &date.Date{Year: 2026, Month: 2, Day: 30} },
		"posting date April 31": func(s *parserv2.Statement) {
			first(s).GetCard().PostingDate = &date.Date{Year: 2026, Month: 4, Day: 31}
		},
		"posting date without a year": func(s *parserv2.Statement) { first(s).GetCard().PostingDate = &date.Date{Month: 10, Day: 9} },
		"a bill without its card account": func(s *parserv2.Statement) {
			s.Accounts[0].Kind = parserv2.AccountKind_ACCOUNT_KIND_DEPOSIT
			s.Accounts[0].Lines = nil
		},
		"an account reported twice": func(s *parserv2.Statement) { s.Accounts = append(s.Accounts, s.Accounts[0]) },
		"no accounts":               func(s *parserv2.Statement) { s.Accounts = nil },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			s := sampleStatement(true)
			breakIt(s)
			if _, err := convertResponse(1, pt.Response("hsbc.credit_card", s)); !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("err = %v, want ErrInvalidResponse", err)
			}
		})
	}
}

func TestTheParsersStatusSaysWhatHasToChangeAndOnlyAnUnreachableParserIsRetried(t *testing.T) {
	for code, want := range map[codes.Code]domain.Failure{
		codes.Unavailable:        domain.Unavailable,
		codes.DeadlineExceeded:   domain.Unavailable,
		codes.ResourceExhausted:  domain.Unavailable,
		codes.Canceled:           domain.Unavailable,
		codes.Unimplemented:      domain.Unsupported,
		codes.FailedPrecondition: domain.Locked,
		codes.InvalidArgument:    domain.Unreadable,
		codes.Internal:           domain.Layout,
		codes.Unknown:            domain.OtherFailure,
	} {
		if got, message := classify(status.Error(code, "x")); got != want || message == "" {
			t.Errorf("%s: %s %q, want %s", code, got, message, want)
		}
	}
	if domain.Unsupported.Status() != domain.Skipped || domain.Layout.Status() != domain.Failed {
		t.Fatal("a document no parser handles is skipped, any other failure failed")
	}
}

// sampleStatement is synthetic (ADR 0006): a bill with a foreign purchase and a payment.
func sampleStatement(balanced bool) *parserv2.Statement {
	stated := "28009"
	if !balanced {
		stated = "30000"
	}
	s := pt.Bill("hsbc", pt.Day(10, 21), pt.Day(11, 8), "-40010",
		pt.CardLine(pt.Day(10, 8), "LEATHER SHOP FIRENZE", parserv2.LineKind_LINE_KIND_PURCHASE, "28009",
			pt.Posted(pt.Day(10, 9)), pt.Foreign("EUR", "780.00"), pt.Country("IT")),
		pt.CardLine(pt.Day(9, 23), "PAYMENT", parserv2.LineKind_LINE_KIND_PAYMENT, "-68019"),
	)
	s.Checks = []*parserv2.Check{pt.Check("new charges", "TWD", "28009", stated)}
	return s
}
