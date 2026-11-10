package ingest

import (
	"testing"

	"google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/genproto/googleapis/type/decimal"

	parserv1 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v1"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

func TestStoringAStatementRelinksTheWholeLedger(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := t.Context()

	october := sampleStatement(true)
	november := sampleStatement(true)
	november.ClosingDate = &date.Date{Year: 2026, Month: 11, Day: 21}
	november.DueDate = &date.Date{Year: 2026, Month: 12, Day: 8}
	november.Transactions = []*parserv1.Transaction{{
		TransactionDate:  &date.Date{Year: 2026, Month: 11, Day: 2},
		Description:      "LEATHER SHOP FIRENZE",
		Kind:             parserv1.TransactionKind_TRANSACTION_KIND_REFUND,
		Amount:           &decimal.Decimal{Value: "-28009"},
		OriginalAmount:   &decimal.Decimal{Value: "-780.00"},
		OriginalCurrency: "EUR",
		Country:          "IT",
	}}
	store := func(documentID int64, statement *parserv1.Statement) {
		t.Helper()
		if _, err := Store(ctx, pool, documentID, "", &parserv1.ParseDocumentResponse{Statement: statement, Parser: "fake", ParserVersion: "1"}); err != nil {
			t.Fatal(err)
		}
	}
	refundParent := func() int64 {
		t.Helper()
		var parent *int64
		if err := pool.QueryRow(ctx, `SELECT parent_id FROM transactions WHERE kind = 'refund'`).Scan(&parent); err != nil {
			t.Fatal(err)
		}
		if parent == nil {
			return 0
		}
		return *parent
	}
	purchaseID := func() int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `SELECT id FROM transactions WHERE kind = 'purchase'`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	store(addDocument(t, pool, "november.eml"), november)
	if parent := refundParent(); parent != 0 {
		t.Fatalf("refund linked to %d before its purchase was stored", parent)
	}
	octoberDoc := addDocument(t, pool, "october.eml")
	store(octoberDoc, october)
	if parent, purchase := refundParent(), purchaseID(); parent != purchase {
		t.Fatalf("refund parent = %d, want the purchase %d", parent, purchase)
	}

	// Parsing October again replaces the purchase with a new row.
	before := purchaseID()
	store(octoberDoc, october)
	if parent, purchase := refundParent(), purchaseID(); purchase == before || parent != purchase {
		t.Fatalf("after re-parse: refund parent = %d, want the new purchase %d (was %d)", parent, purchase, before)
	}
}

// Two statements whose lines are linked, parsed again at the same time, over and over: neither may
// deadlock the other.
func TestConcurrentStoresDoNotDeadlock(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := t.Context()

	october := sampleStatement(true)
	november := sampleStatement(true)
	november.ClosingDate = &date.Date{Year: 2026, Month: 11, Day: 21}
	november.Transactions = []*parserv1.Transaction{{
		TransactionDate:  &date.Date{Year: 2026, Month: 11, Day: 2},
		Description:      "LEATHER SHOP FIRENZE",
		Kind:             parserv1.TransactionKind_TRANSACTION_KIND_REFUND,
		Amount:           &decimal.Decimal{Value: "-28009"},
		OriginalAmount:   &decimal.Decimal{Value: "-780.00"},
		OriginalCurrency: "EUR",
	}}
	documents := map[int64]*parserv1.Statement{
		addDocument(t, pool, "october.eml"):  october,
		addDocument(t, pool, "november.eml"): november,
	}
	store := func(id int64) error {
		_, err := Store(ctx, pool, id, "", &parserv1.ParseDocumentResponse{Statement: documents[id], Parser: "fake", ParserVersion: "1"})
		return err
	}
	for id := range documents {
		if err := store(id); err != nil {
			t.Fatal(err)
		}
	}

	for range 30 {
		errs := make(chan error, len(documents))
		for id := range documents {
			go func() { errs <- store(id) }()
		}
		for range documents {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
	}
}
