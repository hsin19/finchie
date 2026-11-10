package ingest

import (
	"errors"
	"testing"

	"google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/genproto/googleapis/type/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hsin19/finchie/apps/backend/internal/domain"
	parserv1 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v1"
)

func TestConvertMapsStatementAndTransactions(t *testing.T) {
	statement, transactions, err := convert(7, sampleStatement(true))
	if err != nil {
		t.Fatal(err)
	}
	if statement.DocumentID != 7 || statement.Institution != "hsbc" || !statement.Balanced {
		t.Fatalf("statement = %+v", statement)
	}
	if got := statement.ClosingDate.Time.Format("2006-01-02"); got != "2026-10-21" {
		t.Fatalf("closing date = %s", got)
	}
	if len(transactions) != 2 {
		t.Fatalf("got %d transactions", len(transactions))
	}
	shop, payment := transactions[0], transactions[1]
	if shop.Kind != "purchase" || shop.LineNo != 0 || *shop.OriginalCurrency != "EUR" || *shop.Country != "IT" {
		t.Fatalf("shop = %+v", shop)
	}
	if !shop.OriginalAmount.Valid || !shop.PostingDate.Valid {
		t.Fatal("shop should keep its original amount and posting date")
	}
	if payment.Kind != "payment" || payment.OriginalAmount.Valid || payment.PostingDate.Valid || payment.Country != nil {
		t.Fatalf("payment = %+v", payment)
	}
}

func TestConvertRejectsResponsesThatBreakTheContract(t *testing.T) {
	cases := map[string]func(*parserv1.Statement){
		"missing closing date": func(s *parserv1.Statement) { s.ClosingDate = nil },
		"unparsable amount":    func(s *parserv1.Statement) { s.Transactions[0].Amount = &decimal.Decimal{Value: "12,000"} },
		"unspecified kind": func(s *parserv1.Statement) {
			s.Transactions[0].Kind = parserv1.TransactionKind_TRANSACTION_KIND_UNSPECIFIED
		},
		"missing reconciliation": func(s *parserv1.Statement) { s.Reconciliation = nil },
		"NaN amount":             func(s *parserv1.Statement) { s.Transactions[0].Amount = &decimal.Decimal{Value: "NaN"} },
		"infinite total due":     func(s *parserv1.Statement) { s.TotalDue = &decimal.Decimal{Value: "Infinity"} },
		"negative infinite original amount": func(s *parserv1.Statement) {
			s.Transactions[0].OriginalAmount = &decimal.Decimal{Value: "-Infinity"}
		},
		"month 13": func(s *parserv1.Statement) { s.ClosingDate = &date.Date{Year: 2026, Month: 13, Day: 1} },
		"day 0":    func(s *parserv1.Statement) { s.DueDate = &date.Date{Year: 2026, Month: 11} },
		"February 30": func(s *parserv1.Statement) {
			s.Transactions[0].TransactionDate = &date.Date{Year: 2026, Month: 2, Day: 30}
		},
		"posting date April 31": func(s *parserv1.Statement) {
			s.Transactions[0].PostingDate = &date.Date{Year: 2026, Month: 4, Day: 31}
		},
		"posting date without a year": func(s *parserv1.Statement) { s.Transactions[0].PostingDate = &date.Date{Month: 10, Day: 9} },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			s := sampleStatement(true)
			breakIt(s)
			if _, _, err := convert(1, s); !errors.Is(err, ErrInvalidResponse) {
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

// sampleStatement is synthetic (ADR 0006).
func sampleStatement(balanced bool) *parserv1.Statement {
	stated := "28009"
	if !balanced {
		stated = "30000"
	}
	return &parserv1.Statement{
		Institution: "hsbc",
		ClosingDate: &date.Date{Year: 2026, Month: 10, Day: 21},
		DueDate:     &date.Date{Year: 2026, Month: 11, Day: 8},
		Currency:    "TWD",
		TotalDue:    &decimal.Decimal{Value: "-40010"},
		Transactions: []*parserv1.Transaction{
			{
				TransactionDate:  &date.Date{Year: 2026, Month: 10, Day: 8},
				PostingDate:      &date.Date{Year: 2026, Month: 10, Day: 9},
				Description:      "LEATHER SHOP FIRENZE",
				Kind:             parserv1.TransactionKind_TRANSACTION_KIND_PURCHASE,
				Amount:           &decimal.Decimal{Value: "28009"},
				OriginalAmount:   &decimal.Decimal{Value: "780.00"},
				OriginalCurrency: "EUR",
				Country:          "IT",
			},
			{
				TransactionDate: &date.Date{Year: 2026, Month: 9, Day: 23},
				Description:     "PAYMENT",
				Kind:            parserv1.TransactionKind_TRANSACTION_KIND_PAYMENT,
				Amount:          &decimal.Decimal{Value: "-68019"},
			},
		},
		Reconciliation: &parserv1.Reconciliation{
			Balanced:    balanced,
			Computed:    &decimal.Decimal{Value: "28009"},
			Stated:      &decimal.Decimal{Value: stated},
			StatedLabel: "new charges",
		},
	}
}
