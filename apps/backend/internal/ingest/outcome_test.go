package ingest

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertest"
	"google.golang.org/grpc/codes"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	parserv1 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v1"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

// Synthetic names and figures (ADR 0006).

func TestTwoAccountsStatementsForTheSameMonthAreTwoStatementsNotDuplicates(t *testing.T) {
	pool := testdb.Pool(t)
	first := monthly(10, 30, []*parserv1.SecuritiesHolding{holding("範例電子", "1000")})
	second := monthly(10, 30, []*parserv1.SecuritiesHolding{holding("範例航運", "500")})
	second.AccountNumber = "98**765"

	storeSecurities(t, pool, "first.eml", first)
	storeSecurities(t, pool, "second.eml", second)

	if n := countRows(t, pool, "statements"); n != 2 {
		t.Fatalf("statements = %d, want one per account", n)
	}
	var duplicates int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM documents WHERE status = 'duplicate'").Scan(&duplicates); err != nil {
		t.Fatal(err)
	}
	if duplicates != 0 {
		t.Fatalf("%d document(s) taken for a duplicate", duplicates)
	}

	// The same account's statement forwarded again still is one.
	storeSecurities(t, pool, "first-forwarded.eml", monthly(10, 30, []*parserv1.SecuritiesHolding{holding("範例電子", "1000")}))
	if n := countRows(t, pool, "statements"); n != 2 {
		t.Fatalf("statements = %d after a copy, want 2", n)
	}
}

func TestADocumentThatFailsWhenParsedAgainNoLongerHoldsItsStatement(t *testing.T) {
	pool := testdb.Pool(t)
	id := addDocument(t, pool, "statement.eml")
	if err := NewWorker(pool, parsed(sampleStatement(true)), time.Minute).Work(t.Context(), job(id, 1, 6)); err != nil {
		t.Fatal(err)
	}
	if countRows(t, pool, "statements") != 1 {
		t.Fatal("the first parse stored nothing")
	}

	// The parser no longer reads its layout: nothing it once read stays counted.
	_ = NewWorker(pool, failing(codes.Internal), time.Minute).Work(t.Context(), job(id, 1, 6))
	if doc := document(t, pool, id); doc.Status != domain.Failed || doc.Failure == nil || *doc.Failure != domain.Layout {
		t.Fatalf("document = %+v", doc)
	}
	if n := countRows(t, pool, "statements") + countRows(t, pool, "transactions"); n != 0 {
		t.Fatalf("%d rows left of a document that failed", n)
	}
}

func TestADocumentNoParserHandlesIsSkipped(t *testing.T) {
	pool := testdb.Pool(t)
	id := addDocument(t, pool, "promo.eml")
	_ = NewWorker(pool, failing(codes.Unimplemented), time.Minute).Work(t.Context(), job(id, 1, 6))
	if doc := document(t, pool, id); doc.Status != domain.Skipped || doc.Failure == nil || *doc.Failure != domain.Unsupported {
		t.Fatalf("document = %+v", doc)
	}
}

// retried runs Retry twice, the second time before the jobs are worked, against a parser that describes
// itself as hsbc.credit_card version 1, instance "a", and returns how many parse jobs each document has.
func retried(t *testing.T, pool *pgxpool.Pool) map[int64]int {
	t.Helper()
	// Insert-only client: the jobs Retry queues are counted, not worked.
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := rivertest.WorkContext(t.Context(), client)
	for range 2 { // run again before the jobs are worked, it queues nothing more
		if err := NewRetryWorker(pool, parsed(sampleStatement(true))).Work(ctx, &river.Job[RetryArgs]{}); err != nil {
			t.Fatal(err)
		}
	}
	queued := map[int64]int{}
	jobs, err := pool.Query(t.Context(), "SELECT (args->>'document_id')::bigint FROM river_job WHERE kind = 'parse_document'")
	if err != nil {
		t.Fatal(err)
	}
	for jobs.Next() {
		var id int64
		if err := jobs.Scan(&id); err != nil {
			t.Fatal(err)
		}
		queued[id]++
	}
	return queued
}

func finish(t *testing.T, pool *pgxpool.Pool, id int64, params dbq.FinishDocumentParams) {
	t.Helper()
	params.ID = id
	if params.Warnings == nil {
		params.Warnings = []string{}
	}
	if err := dbq.New(pool).FinishDocument(t.Context(), params); err != nil {
		t.Fatal(err)
	}
}

func failed(failure domain.Failure, instance string) dbq.FinishDocumentParams {
	return dbq.FinishDocumentParams{Status: failure.Status(), Failure: &failure, Error: new(string), ParserInstance: &instance}
}

func TestRetryQueuesDocumentsThatStalledButNotOnesThatFailedForGood(t *testing.T) {
	pool := testdb.Pool(t)
	lost := addDocument(t, pool, "lost.eml") // pending, its job gone
	unreachable := addDocument(t, pool, "unreachable.eml")
	finish(t, pool, unreachable, failed(domain.Unavailable, "a"))
	locked := addDocument(t, pool, "locked.eml")
	finish(t, pool, locked, failed(domain.Locked, "a"))

	queued := retried(t, pool)
	if queued[lost] != 1 || queued[unreachable] != 1 || queued[locked] != 0 {
		t.Fatalf("parse jobs by document = %v; want lost %d and unreachable %d once, locked %d never", queued, lost, unreachable, locked)
	}
	if got := document(t, pool, unreachable).Status; got != domain.Pending {
		t.Fatalf("unreachable is %s, want pending again", got)
	}
}

func TestRetryQueuesWhatAnotherParserVersionReadOrAnEarlierInstanceFailed(t *testing.T) {
	pool := testdb.Pool(t)
	parsedBy := func(name, version string) dbq.FinishDocumentParams {
		return dbq.FinishDocumentParams{Status: domain.Parsed, Parser: &name, ParserVersion: &version}
	}
	current := addDocument(t, pool, "current.eml")
	finish(t, pool, current, parsedBy("hsbc.credit_card", "1"))
	older := addDocument(t, pool, "older.eml")
	finish(t, pool, older, parsedBy("hsbc.credit_card", "0"))
	gone := addDocument(t, pool, "gone.eml") // its parser is no more
	finish(t, pool, gone, parsedBy("retired.credit_card", "3"))
	lockedBefore := addDocument(t, pool, "locked-before.eml") // a password may have been set since
	finish(t, pool, lockedBefore, failed(domain.Locked, "an earlier instance"))
	skippedBefore := addDocument(t, pool, "skipped-before.eml") // a parser may have been added since
	finish(t, pool, skippedBefore, failed(domain.Unsupported, "an earlier instance"))
	skippedNow := addDocument(t, pool, "skipped-now.eml")
	finish(t, pool, skippedNow, failed(domain.Unsupported, "a"))

	queued := retried(t, pool)
	for id, want := range map[int64]int{current: 0, older: 1, gone: 1, lockedBefore: 1, skippedBefore: 1, skippedNow: 0} {
		if queued[id] != want {
			t.Errorf("document %d: %d jobs, want %d", id, queued[id], want)
		}
	}
}
