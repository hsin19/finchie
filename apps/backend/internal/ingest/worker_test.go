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
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

// fakeParser answers ParseDocument and DescribeParsers; the worker never calls the embedded (nil)
// OpenDocument.
type fakeParser struct {
	parserv2.ParserServiceClient
	response *parserv2.ParseDocumentResponse
	err      error
	requests []*parserv2.ParseDocumentRequest
	// What DescribeParsers says: the parser of response at its version, and this instance.
	instance string
}

func (f *fakeParser) DescribeParsers(context.Context, *parserv2.DescribeParsersRequest, ...grpc.CallOption) (*parserv2.DescribeParsersResponse, error) {
	described := &parserv2.DescribeParsersResponse{Instance: f.instance}
	if f.response != nil {
		described.Parsers = []*parserv2.ParserVersion{{Name: f.response.GetParser(), Version: f.response.GetParserVersion()}}
	}
	return described, nil
}

func (f *fakeParser) ParseDocument(_ context.Context, in *parserv2.ParseDocumentRequest, _ ...grpc.CallOption) (*parserv2.ParseDocumentResponse, error) {
	f.requests = append(f.requests, in)
	return f.response, f.err
}

func parsed(statement *parserv2.Statement, warnings ...string) *fakeParser {
	return &fakeParser{instance: "a", response: &parserv2.ParseDocumentResponse{
		Parser: "hsbc.credit_card", ParserVersion: "1", Statement: statement, Warnings: warnings,
	}}
}

func failing(code codes.Code) *fakeParser {
	return &fakeParser{instance: "a", err: status.Error(code, "parser says no")}
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
	if request.GetDocumentId() != "1" || string(request.GetContent()) != "raw october.eml" {
		t.Fatalf("request = %v", request)
	}
	doc := document(t, pool, id)
	if doc.Status != domain.Parsed || *doc.Parser != "hsbc.credit_card" || doc.Warnings[0] != "minor OCR doubt" || !doc.ParsedAt.Valid {
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

func TestABillsLinesAndWhatItOwesGoToItsBanksCardAccount(t *testing.T) {
	pool := testdb.Pool(t)
	store := func(name string, statement *parserv2.Statement) {
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
	if kind != string(domain.CreditCard) || number != "" {
		t.Fatalf("account = %s %q", kind, number)
	}
	var lines int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM transactions t JOIN units u ON u.id = t.unit_id WHERE t.account_id = $1 AND u.symbol = 'TWD' AND u.kind = 'money'", account).Scan(&lines); err != nil || lines != 2 {
		t.Fatalf("lines in the account = %d (%v)", lines, err)
	}

	november := sampleStatement(true)
	november.ClosingDate = &date.Date{Year: 2026, Month: 11, Day: 21}
	store("november.eml", november)
	other := sampleStatement(true)
	other.Institution = "taishin"
	store("taishin.eml", other)
	// One account per bank, a liability whose balance on each closing date is what that bill owes (ADR 0022).
	if countRows(t, pool, "accounts") != 2 || countRows(t, pool, "balances") != 3 {
		t.Fatalf("accounts = %d, balances = %d", countRows(t, pool, "accounts"), countRows(t, pool, "balances"))
	}
	var owed string
	if err := pool.QueryRow(t.Context(), "SELECT amount::text FROM balances WHERE account_id = $1 ORDER BY as_of DESC LIMIT 1", account).Scan(&owed); err != nil || owed != "-40010" {
		t.Fatalf("newest balance = %s (%v), want the bill's total due as printed", owed, err)
	}
}

func TestReparsingKeepsTheAccount(t *testing.T) {
	pool := testdb.Pool(t)
	id := addDocument(t, pool, "again.eml")
	for range 2 {
		if err := NewWorker(pool, parsed(sampleStatement(true)), time.Minute).Work(t.Context(), job(id, 1, 6)); err != nil {
			t.Fatal(err)
		}
	}
	if countRows(t, pool, "accounts") != 1 || countRows(t, pool, "transactions") != 2 {
		t.Fatalf("accounts = %d, lines = %d", countRows(t, pool, "accounts"), countRows(t, pool, "transactions"))
	}
}

func TestWorkerFlagsAStatementThatDoesNotReconcile(t *testing.T) {
	pool := testdb.Pool(t)
	id := addDocument(t, pool, "unbalanced.eml")
	if err := NewWorker(pool, parsed(sampleStatement(false)), time.Minute).Work(t.Context(), job(id, 1, 6)); err != nil {
		t.Fatal(err)
	}
	if got := document(t, pool, id).Status; got != domain.NeedsReview {
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
	if doc := document(t, pool, pdf); doc.Status != domain.Duplicate || doc.DuplicateOf == nil || *doc.DuplicateOf != email {
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
	before := statementOf(t, pool, id)

	fixed := sampleStatement(true)
	fixed.Accounts[0].Lines = fixed.Accounts[0].Lines[:1]
	if err := NewWorker(pool, parsed(fixed), time.Minute).Work(t.Context(), job(id, 1, 6)); err != nil {
		t.Fatal(err)
	}
	if countRows(t, pool, "statements") != 1 || countRows(t, pool, "transactions") != 1 {
		t.Fatal("re-parse must replace, not append")
	}
	// Its page and the links to it keep working.
	if after := statementOf(t, pool, id); after != before {
		t.Fatalf("statement id = %d after re-parse, want %d as before", after, before)
	}
	// A statement stored for the first time after that still gets an id of its own.
	other := addDocument(t, pool, "other.eml")
	next := sampleStatement(true)
	next.ClosingDate.Month++
	if err := NewWorker(pool, parsed(next), time.Minute).Work(t.Context(), job(other, 1, 6)); err != nil {
		t.Fatal(err)
	}
	if got := statementOf(t, pool, other); got == before {
		t.Fatalf("a new statement took id %d", got)
	}
}

func statementOf(t *testing.T, pool *pgxpool.Pool, documentID int64) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(t.Context(), "SELECT id FROM statements WHERE document_id = $1", documentID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
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
	if doc.Status != domain.Failed || doc.Failure == nil || *doc.Failure != domain.Locked || doc.Error == nil || *doc.Error != "parser says no" {
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
	if got := document(t, pool, id).Status; got != domain.Pending {
		t.Fatalf("status after a retryable failure = %s", got)
	}

	if err := worker.Work(t.Context(), job(id, 6, 6)); !errors.As(err, &cancel) {
		t.Fatalf("last attempt: err = %v, want a cancelled job", err)
	}
	// Failed for now, for Retry to take up again: the parser being out of reach says nothing of the document.
	if doc := document(t, pool, id); doc.Status != domain.Failed || doc.Failure == nil || *doc.Failure != domain.Unavailable {
		t.Fatalf("after the last attempt = %+v", doc)
	}
}

func TestStoreErrorsFailTheDocumentOnTheLastAttempt(t *testing.T) {
	pool := testdb.Pool(t)
	id := addDocument(t, pool, "rejected.eml")
	statement := sampleStatement(true)
	// Valid for the parser contract, but Postgres rejects it: currency is CHAR(3).
	statement.CardBill.Currency = "TWDX"
	worker := NewWorker(pool, parsed(statement), time.Minute)

	err := worker.Work(t.Context(), job(id, 1, 6))
	var cancel *rivertype.JobCancelError
	if err == nil || errors.As(err, &cancel) {
		t.Fatalf("first attempt: err = %v, want a plain error so River retries", err)
	}
	if got := document(t, pool, id).Status; got != domain.Pending {
		t.Fatalf("status after the first attempt = %s", got)
	}

	if err := worker.Work(t.Context(), job(id, 6, 6)); !errors.As(err, &cancel) {
		t.Fatalf("last attempt: err = %v, want a cancelled job", err)
	}
	if doc := document(t, pool, id); doc.Status != domain.Failed || doc.Error == nil || *doc.Error == "" {
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
	statement.Accounts[0].Lines[0].Amount.Value = "NaN"

	err := NewWorker(pool, parsed(statement), time.Minute).Work(t.Context(), job(id, 1, 6))

	var cancel *rivertype.JobCancelError
	if !errors.As(err, &cancel) {
		t.Fatalf("err = %v, want a cancelled job", err)
	}
	if doc := document(t, pool, id); doc.Status != domain.Failed || doc.Error == nil {
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

func (p stoppingParser) ParseDocument(ctx context.Context, in *parserv2.ParseDocumentRequest, opts ...grpc.CallOption) (*parserv2.ParseDocumentResponse, error) {
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
	if doc := document(t, pool, id); doc.Status != domain.Pending || doc.Error != nil {
		t.Fatalf("document = %+v, want it left pending for the next attempt", doc)
	}
}
