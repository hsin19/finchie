package ingest

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	parserv1 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v1"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

// fakeParser answers ParseDocument; the worker never calls the embedded (nil) OpenDocument.
type fakeParser struct {
	parserv1.ParserServiceClient
	response *parserv1.ParseDocumentResponse
	err      error
	requests []*parserv1.ParseDocumentRequest
}

func (f *fakeParser) ParseDocument(_ context.Context, in *parserv1.ParseDocumentRequest, _ ...grpc.CallOption) (*parserv1.ParseDocumentResponse, error) {
	f.requests = append(f.requests, in)
	return f.response, f.err
}

func parsed(statement *parserv1.Statement, warnings ...string) *fakeParser {
	return &fakeParser{response: &parserv1.ParseDocumentResponse{
		Parser: "hsbc.credit_card", ParserVersion: "1", Statement: statement, Warnings: warnings,
	}}
}

func failing(code codes.Code) *fakeParser {
	return &fakeParser{err: status.Error(code, "parser says no")}
}

func addDocument(t *testing.T, pool *pgxpool.Pool, name string) int64 {
	t.Helper()
	sum := sha256.Sum256([]byte(name))
	row, err := dbq.New(pool).UpsertDocument(t.Context(), dbq.UpsertDocumentParams{
		Sha256: sum[:], Filename: name, ContentType: "message/rfc822", Content: []byte("raw " + name),
	})
	if err != nil {
		t.Fatal(err)
	}
	return row.ID
}

func job(documentID int64, attempt, maxAttempts int) *river.Job[ParseDocumentArgs] {
	return &river.Job[ParseDocumentArgs]{
		JobRow: &rivertype.JobRow{Attempt: attempt, MaxAttempts: maxAttempts},
		Args:   ParseDocumentArgs{DocumentID: documentID},
	}
}

func document(t *testing.T, pool *pgxpool.Pool, id int64) dbq.GetDocumentRow {
	t.Helper()
	row, err := dbq.New(pool).GetDocument(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func countRows(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestWorkerStoresAParsedStatement(t *testing.T) {
	pool := testdb.Pool(t)
	id := addDocument(t, pool, "october.eml")
	parser := parsed(sampleStatement(true), "minor OCR doubt")

	if err := NewWorker(pool, parser, time.Minute).Work(t.Context(), job(id, 1, 6)); err != nil {
		t.Fatal(err)
	}

	request := parser.requests[0]
	if request.GetDocumentId() != "1" || request.GetFilename() != "october.eml" || string(request.GetContent()) != "raw october.eml" {
		t.Fatalf("request = %v", request)
	}
	doc := document(t, pool, id)
	if doc.Status != StatusParsed || *doc.Parser != "hsbc.credit_card" || doc.Warnings[0] != "minor OCR doubt" || !doc.ParsedAt.Valid {
		t.Fatalf("document = %+v", doc)
	}
	rows, err := dbq.New(pool).ListTransactions(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Description != "LEATHER SHOP FIRENZE" {
		t.Fatalf("transactions = %+v", rows)
	}
	if value, _ := rows[0].OriginalAmount.Value(); value != "780.00" {
		t.Fatalf("original amount = %v, want the exact decimal back", value)
	}
}

func TestABillIsStoredInItsBanksCardAccountWithWhatItSaysIsOwed(t *testing.T) {
	pool := testdb.Pool(t)
	store := func(name string, statement *parserv1.Statement) {
		t.Helper()
		if err := NewWorker(pool, parsed(statement), time.Minute).Work(t.Context(), job(addDocument(t, pool, name), 1, 6)); err != nil {
			t.Fatal(err)
		}
	}
	store("october.eml", sampleStatement(true))

	var account int64
	var kind, number string
	if err := pool.QueryRow(t.Context(), "SELECT id, kind, number FROM accounts WHERE institution = 'hsbc'").Scan(&account, &kind, &number); err != nil {
		t.Fatal(err)
	}
	if kind != KindCreditCard || number != "" {
		t.Fatalf("account = %s %q", kind, number)
	}
	var lines int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM transactions WHERE account_id = $1 AND unit = 'TWD' AND status = 'active'", account).Scan(&lines); err != nil || lines != 2 {
		t.Fatalf("lines in the account = %d (%v)", lines, err)
	}
	var asOf time.Time
	var amount string
	if err := pool.QueryRow(t.Context(), "SELECT as_of, amount::text FROM balances WHERE account_id = $1 AND unit = 'TWD'", account).Scan(&asOf, &amount); err != nil {
		t.Fatal(err)
	}
	// The sample bill's total due is -40010, a credit: the owner is owed 40010.
	if asOf.Format(time.DateOnly) != "2026-10-21" || amount != "40010" {
		t.Fatalf("balance = %s on %s", amount, asOf.Format(time.DateOnly))
	}

	november := sampleStatement(true)
	november.ClosingDate = &date.Date{Year: 2026, Month: 11, Day: 21}
	store("november.eml", november)
	other := sampleStatement(true)
	other.Institution = "taishin"
	store("taishin.eml", other)
	if countRows(t, pool, "accounts") != 2 || countRows(t, pool, "balances") != 3 {
		t.Fatalf("accounts = %d, balances = %d: one account per bank's bill, one balance per bill",
			countRows(t, pool, "accounts"), countRows(t, pool, "balances"))
	}
}

func TestReparsingKeepsTheAccountAndReplacesTheBalance(t *testing.T) {
	pool := testdb.Pool(t)
	id := addDocument(t, pool, "again.eml")
	for range 2 {
		if err := NewWorker(pool, parsed(sampleStatement(true)), time.Minute).Work(t.Context(), job(id, 1, 6)); err != nil {
			t.Fatal(err)
		}
	}
	if countRows(t, pool, "accounts") != 1 || countRows(t, pool, "balances") != 1 {
		t.Fatalf("accounts = %d, balances = %d", countRows(t, pool, "accounts"), countRows(t, pool, "balances"))
	}
}

func TestWorkerFlagsAStatementThatDoesNotReconcile(t *testing.T) {
	pool := testdb.Pool(t)
	id := addDocument(t, pool, "unbalanced.eml")
	if err := NewWorker(pool, parsed(sampleStatement(false)), time.Minute).Work(t.Context(), job(id, 1, 6)); err != nil {
		t.Fatal(err)
	}
	if got := document(t, pool, id).Status; got != StatusNeedsReview {
		t.Fatalf("status = %s", got)
	}
	if countRows(t, pool, "statements") != 1 {
		t.Fatal("an unbalanced statement is still stored")
	}
}

func TestWorkerMarksASecondCopyOfAStatementAsDuplicate(t *testing.T) {
	pool := testdb.Pool(t)
	email := addDocument(t, pool, "statement.eml")
	pdf := addDocument(t, pool, "statement.pdf")
	worker := NewWorker(pool, parsed(sampleStatement(true)), time.Minute)

	for _, id := range []int64{email, pdf} {
		if err := worker.Work(t.Context(), job(id, 1, 6)); err != nil {
			t.Fatal(err)
		}
	}
	if doc := document(t, pool, pdf); doc.Status != StatusDuplicate || doc.Error == nil {
		t.Fatalf("second copy = %+v", doc)
	}
	if countRows(t, pool, "statements") != 1 || countRows(t, pool, "transactions") != 2 {
		t.Fatal("the duplicate must not add rows")
	}
}

func TestReparsingReplacesThePreviousResult(t *testing.T) {
	pool := testdb.Pool(t)
	id := addDocument(t, pool, "again.eml")
	if err := NewWorker(pool, parsed(sampleStatement(true)), time.Minute).Work(t.Context(), job(id, 1, 6)); err != nil {
		t.Fatal(err)
	}

	fixed := sampleStatement(true)
	fixed.Transactions = fixed.Transactions[:1]
	if err := NewWorker(pool, parsed(fixed), time.Minute).Work(t.Context(), job(id, 1, 6)); err != nil {
		t.Fatal(err)
	}
	if countRows(t, pool, "statements") != 1 || countRows(t, pool, "transactions") != 1 {
		t.Fatal("re-parse must replace, not append")
	}
}

func TestPermanentParserErrorsFailTheDocumentWithoutRetry(t *testing.T) {
	pool := testdb.Pool(t)
	id := addDocument(t, pool, "locked.eml")

	err := NewWorker(pool, failing(codes.FailedPrecondition), time.Minute).Work(t.Context(), job(id, 1, 6))

	var cancel *rivertype.JobCancelError
	if !errors.As(err, &cancel) {
		t.Fatalf("err = %v, want a cancelled job", err)
	}
	doc := document(t, pool, id)
	if doc.Status != StatusFailed || doc.Error == nil || *doc.Error != "FailedPrecondition: parser says no" {
		t.Fatalf("document = %+v", doc)
	}
}

func TestTransientParserErrorsRetryUntilAttemptsRunOut(t *testing.T) {
	pool := testdb.Pool(t)
	id := addDocument(t, pool, "later.eml")
	worker := NewWorker(pool, failing(codes.Unavailable), time.Minute)

	err := worker.Work(t.Context(), job(id, 1, 6))
	var cancel *rivertype.JobCancelError
	if err == nil || errors.As(err, &cancel) {
		t.Fatalf("first attempt: err = %v, want a plain error so River retries", err)
	}
	if got := document(t, pool, id).Status; got != StatusPending {
		t.Fatalf("status after a retryable failure = %s", got)
	}

	if err := worker.Work(t.Context(), job(id, 6, 6)); !errors.As(err, &cancel) {
		t.Fatalf("last attempt: err = %v, want a cancelled job", err)
	}
	if got := document(t, pool, id).Status; got != StatusFailed {
		t.Fatalf("status after the last attempt = %s", got)
	}
}

func TestStoreErrorsFailTheDocumentOnTheLastAttempt(t *testing.T) {
	pool := testdb.Pool(t)
	id := addDocument(t, pool, "rejected.eml")
	statement := sampleStatement(true)
	// Valid for the parser contract, but Postgres rejects it: currency is CHAR(3).
	statement.Currency = "TWDX"
	worker := NewWorker(pool, parsed(statement), time.Minute)

	err := worker.Work(t.Context(), job(id, 1, 6))
	var cancel *rivertype.JobCancelError
	if err == nil || errors.As(err, &cancel) {
		t.Fatalf("first attempt: err = %v, want a plain error so River retries", err)
	}
	if got := document(t, pool, id).Status; got != StatusPending {
		t.Fatalf("status after the first attempt = %s", got)
	}

	if err := worker.Work(t.Context(), job(id, 6, 6)); !errors.As(err, &cancel) {
		t.Fatalf("last attempt: err = %v, want a cancelled job", err)
	}
	if doc := document(t, pool, id); doc.Status != StatusFailed || doc.Error == nil || *doc.Error == "" {
		t.Fatalf("document after the last attempt = %+v", doc)
	}
	if countRows(t, pool, "statements") != 0 {
		t.Fatal("a rejected statement must not be stored")
	}
}

func TestWorkerFailsAResponseWithANonFiniteAmount(t *testing.T) {
	pool := testdb.Pool(t)
	id := addDocument(t, pool, "nan.eml")
	statement := sampleStatement(true)
	statement.Transactions[0].Amount.Value = "NaN"

	err := NewWorker(pool, parsed(statement), time.Minute).Work(t.Context(), job(id, 1, 6))

	var cancel *rivertype.JobCancelError
	if !errors.As(err, &cancel) {
		t.Fatalf("err = %v, want a cancelled job", err)
	}
	if doc := document(t, pool, id); doc.Status != StatusFailed || doc.Error == nil {
		t.Fatalf("document = %+v", doc)
	}
	if countRows(t, pool, "statements") != 0 {
		t.Fatal("a statement with a NaN amount must not be stored")
	}
}

// stoppingParser is a parse interrupted because the backend is stopping: River cancels the job's
// context mid-call, and the call fails with CANCELLED.
type stoppingParser struct {
	*fakeParser
	stop context.CancelFunc
}

func (p stoppingParser) ParseDocument(ctx context.Context, in *parserv1.ParseDocumentRequest, opts ...grpc.CallOption) (*parserv1.ParseDocumentResponse, error) {
	p.stop()
	return p.fakeParser.ParseDocument(ctx, in, opts...)
}

func TestAParseCancelledOnShutdownIsRetried(t *testing.T) {
	pool := testdb.Pool(t)
	id := addDocument(t, pool, "interrupted.eml")
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	err := NewWorker(pool, stoppingParser{failing(codes.Canceled), stop}, time.Minute).Work(ctx, job(id, 1, 6))

	var cancel *rivertype.JobCancelError
	if err == nil || errors.As(err, &cancel) {
		t.Fatalf("err = %v, want a plain error so River retries", err)
	}
	if doc := document(t, pool, id); doc.Status != StatusPending || doc.Error != nil {
		t.Fatalf("document = %+v, want it left pending for the next attempt", doc)
	}
}
