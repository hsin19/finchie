package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
)

// Synthetic names and figures (ADR 0006).

func del(t *testing.T, e *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodDelete, path, nil))
	return recorder
}

func documentAt(t *testing.T, e *echo.Echo, id int64) Document {
	t.Helper()
	recorder := get(t, e, documentPath(id))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET document %d: %d %s", id, recorder.Code, recorder.Body)
	}
	return decode[Document](t, recorder)
}

func documentPath(id int64) string {
	return "/api/documents/" + itoa(id)
}

func rows(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func fail(t *testing.T, pool *pgxpool.Pool, id int64, failure domain.Failure) {
	t.Helper()
	queries := dbq.New(pool)
	// As the worker fails one: what it held goes first.
	if _, err := queries.DeleteStatementsForDocument(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	detail := "synthetic"
	if err := queries.FinishDocument(t.Context(), dbq.FinishDocumentParams{
		ID: id, Status: failure.Status(), Failure: &failure, Error: &detail, Warnings: []string{},
	}); err != nil {
		t.Fatal(err)
	}
}

func septemberShop() *parserv2.ParseDocumentResponse {
	return septemberBill("300", cardLine(1, parserv2.LineKind_LINE_KIND_PURCHASE, "300", "SHOP", "1234"))
}

func TestOnlyADocumentThatHoldsNothingOfTheLedgerCanBeDeleted(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	holder := store(t, pool, "bill.eml", septemberShop())
	duplicate := store(t, pool, "bill-forwarded.eml", septemberShop())
	failed := store(t, pool, "broken.eml", septemberShop())
	fail(t, pool, failed, domain.Layout)
	skipped := store(t, pool, "promo.eml", septemberShop())
	fail(t, pool, skipped, domain.Unsupported)
	pending := decode[Document](t, postFile(t, e, "file", "new.eml", []byte("From: bank\r\n\r\nhello")))

	for id, want := range map[int64]bool{holder: false, pending.Id: false, duplicate: true, failed: true, skipped: true} {
		if got := documentAt(t, e, id).Deletable; got != want {
			t.Errorf("document %d deletable = %v, want %v", id, got, want)
		}
	}

	// What holds a statement, and what is being parsed, stays.
	for _, id := range []int64{holder, pending.Id} {
		if recorder := del(t, e, documentPath(id)); recorder.Code != http.StatusConflict || decode[Problem](t, recorder).Message == "" {
			t.Fatalf("DELETE document %d: %d %s", id, recorder.Code, recorder.Body)
		}
		documentAt(t, e, id)
	}

	for _, id := range []int64{duplicate, failed, skipped} {
		if recorder := del(t, e, documentPath(id)); recorder.Code != http.StatusNoContent {
			t.Fatalf("DELETE document %d: %d %s", id, recorder.Code, recorder.Body)
		}
		if recorder := get(t, e, documentPath(id)); recorder.Code != http.StatusNotFound {
			t.Fatalf("GET deleted document %d: %d", id, recorder.Code)
		}
		if recorder := get(t, e, documentPath(id)+"/content"); recorder.Code != http.StatusNotFound {
			t.Fatalf("GET deleted document %d's original: %d", id, recorder.Code)
		}
	}
	if recorder := del(t, e, documentPath(duplicate)); recorder.Code != http.StatusNotFound {
		t.Fatalf("DELETE again: %d", recorder.Code)
	}

	// The statement the duplicate repeated is still the holder's, with its line.
	if n := rows(t, pool, "statements"); n != 1 {
		t.Fatalf("statements = %d, want the holder's", n)
	}
	if n := rows(t, pool, "transactions"); n != 1 {
		t.Fatalf("lines = %d, want the holder's one", n)
	}
	if doc := documentAt(t, e, holder); doc.Status != Parsed {
		t.Fatalf("holder = %+v", doc)
	}
}

func TestDeletingADocumentParsesTheDuplicatesOfItAgain(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	holder := store(t, pool, "bill.eml", septemberShop())
	duplicate := store(t, pool, "bill-forwarded.eml", septemberShop())
	// The holder no longer reads: its statement is gone, and the duplicate still names it.
	fail(t, pool, holder, domain.Layout)
	before := queuedJobs(t, pool)

	if recorder := del(t, e, documentPath(holder)); recorder.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d %s", recorder.Code, recorder.Body)
	}
	doc := documentAt(t, e, duplicate)
	if doc.Status != Pending || doc.DuplicateOf != nil {
		t.Fatalf("duplicate after its holder was deleted = %+v, want pending, parsed again to hold the statement", doc)
	}
	if n := queuedJobs(t, pool) - before; n != 1 {
		t.Fatalf("parse jobs queued = %d, want 1", n)
	}
}

func TestDeletingADocumentThatDoesNotExistIsNotFound(t *testing.T) {
	e, _ := newAPI(t, 1<<20)
	if recorder := del(t, e, "/api/documents/99"); recorder.Code != http.StatusNotFound {
		t.Fatalf("DELETE: %d %s", recorder.Code, recorder.Body)
	}
}
