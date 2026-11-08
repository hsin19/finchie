package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	parserv1 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v1"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

func newAPI(t *testing.T, maxUploadBytes int64) (*echo.Echo, *pgxpool.Pool) {
	t.Helper()
	return newAPIWithParser(t, maxUploadBytes, nil)
}

const testMailbox = "finchie@mailbox.example"

func newAPIWithParser(t *testing.T, maxUploadBytes int64, parser parserv1.ParserServiceClient) (*echo.Echo, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.Pool(t)
	// Insert-only client: jobs are queued but nothing works them in these tests.
	jobs, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	Register(e, NewServer(pool, jobs, parser, maxUploadBytes, testMailbox))
	return e, pool
}

func postFile(t *testing.T, e *echo.Echo, field, name string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	return postFileAs(t, e, field, name, "application/octet-stream", content)
}

// postFileAs uploads with the Content-Type a browser would declare for the file.
func postFileAs(t *testing.T, e *echo.Echo, field, name, declared string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, name))
	header.Set("Content-Type", declared)
	part, err := form.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/documents", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, request)
	return recorder
}

func get(t *testing.T, e *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	return recorder
}

func decode[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(recorder.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return value
}

func queuedJobs(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM river_job WHERE kind = 'parse_document'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestUploadStoresTheDocumentAndQueuesOneParse(t *testing.T) {
	e, pool := newAPI(t, 1<<20)

	first := postFile(t, e, "file", "statement.eml", []byte("From: bank\r\n\r\nhello"))
	if first.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", first.Code, first.Body)
	}
	doc := decode[Document](t, first)
	if doc.Status != Pending || doc.ContentType != "message/rfc822" || doc.SizeBytes != 19 || len(doc.Sha256) != 64 {
		t.Fatalf("document = %+v", doc)
	}

	again := postFile(t, e, "file", "renamed.eml", []byte("From: bank\r\n\r\nhello"))
	if again.Code != http.StatusOK || decode[Document](t, again).Id != doc.Id {
		t.Fatalf("re-upload: %d %s", again.Code, again.Body)
	}
	if n := queuedJobs(t, pool); n != 1 {
		t.Fatalf("queued jobs = %d, want 1", n)
	}
}

func TestUploadRefusesAnythingButAnEmailBeforeStoringIt(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	cases := map[string][]byte{
		"statement.pdf": []byte("%PDF-1.7 ..."),
		"renamed.eml":   []byte("%PDF-1.7 ..."),
		"image.png":     []byte("\x89PNG\r\n\x1a\n"),
		"notes.txt":     []byte("just some text"),
	}
	for name, content := range cases {
		recorder := postFile(t, e, "file", name, content)
		if recorder.Code != http.StatusUnsupportedMediaType || decode[Problem](t, recorder).Message == "" {
			t.Errorf("%s: %d %s", name, recorder.Code, recorder.Body)
		}
	}
	if docs := decode[[]Document](t, get(t, e, "/api/documents")); len(docs) != 0 {
		t.Fatalf("documents = %+v", docs)
	}
	if n := queuedJobs(t, pool); n != 0 {
		t.Fatalf("queued jobs = %d", n)
	}
}

func TestUploadRejectsBadRequests(t *testing.T) {
	e, _ := newAPI(t, 16)
	cases := map[string]struct {
		field   string
		content []byte
		want    int
	}{
		"missing file field": {"attachment", []byte("x"), http.StatusBadRequest},
		"empty file":         {"file", nil, http.StatusBadRequest},
		"too large":          {"file", bytes.Repeat([]byte("x"), 17), http.StatusRequestEntityTooLarge},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := postFile(t, e, tc.field, "a.eml", tc.content).Code; got != tc.want {
				t.Fatalf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestUploadThatIsNotAMultipartFormIsABadRequest(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	for _, contentType := range []string{"", "application/json", "multipart/form-data"} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/documents", strings.NewReader(`{"file": "x"}`))
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		recorder := httptest.NewRecorder()
		e.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest || decode[Problem](t, recorder).Message == "" {
			t.Errorf("Content-Type %q: %d %s", contentType, recorder.Code, recorder.Body)
		}
	}
	if n := queuedJobs(t, pool); n != 0 {
		t.Fatalf("queued jobs = %d", n)
	}
}

func TestReadEndpoints(t *testing.T) {
	e, _ := newAPI(t, 1<<20)
	postFile(t, e, "file", "a.eml", []byte("From: bank\r\n\r\none"))

	if docs := decode[[]Document](t, get(t, e, "/api/documents?limit=10")); len(docs) != 1 {
		t.Fatalf("documents = %+v", docs)
	}
	if statements := decode[[]Statement](t, get(t, e, "/api/statements")); len(statements) != 0 {
		t.Fatalf("statements = %+v", statements)
	}
	for _, path := range []string{"/api/documents/999", "/api/statements/999/transactions"} {
		if got := get(t, e, path).Code; got != http.StatusNotFound {
			t.Errorf("%s: status = %d", path, got)
		}
	}
}

func TestAnUploadedEmailIsStoredAsOneAndServedAsADownload(t *testing.T) {
	e, _ := newAPI(t, 1<<20)
	cases := []struct {
		name, declared string
		content        []byte
		stored, served string
		disposition    string
	}{
		{"statement.eml", "application/octet-stream", []byte("From: bank\r\n\r\nhello"), "message/rfc822", "application/octet-stream", "attachment; filename=statement.eml"},
		// What the browser declares is ignored: stored as declared, it would run as a page on the app's origin.
		{"page.html", "text/html", []byte("From: bank\r\n\r\n<script>alert(1)</script>"), "message/rfc822", "application/octet-stream", "attachment; filename=page.html"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := decode[Document](t, postFileAs(t, e, "file", tc.name, tc.declared, tc.content))
			if doc.ContentType != tc.stored {
				t.Fatalf("stored content type = %q, want %q", doc.ContentType, tc.stored)
			}
			recorder := get(t, e, fmt.Sprintf("/api/documents/%d/content", doc.Id))
			header := recorder.Header()
			if recorder.Code != http.StatusOK || header.Get("Content-Type") != tc.served ||
				header.Get("Content-Disposition") != tc.disposition || header.Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("content: %d %v", recorder.Code, header)
			}
		})
	}
}

func TestTheMailFilterForwardsToTheDeploymentsMailbox(t *testing.T) {
	e, _ := newAPI(t, 1<<20)
	recorder := get(t, e, "/api/mail-filters.xml")
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "application/xml" ||
		recorder.Header().Get("Content-Disposition") != `attachment; filename="finchie-mail-filters.xml"` {
		t.Fatalf("mail filter: %d %v", recorder.Code, recorder.Header())
	}
	if body := recorder.Body.String(); !strings.Contains(body, `name="forwardTo" value="`+testMailbox+`"`) {
		t.Fatalf("filter does not forward to %s:\n%s", testMailbox, body)
	}
}

func TestTheMailFilterIsMissingWithoutAMailboxAddress(t *testing.T) {
	e := echo.New()
	Register(e, NewServer(nil, nil, nil, 1<<20, ""))
	recorder := get(t, e, "/api/mail-filters.xml")
	if recorder.Code != http.StatusNotFound || !strings.Contains(decode[Problem](t, recorder).Message, "BACKEND_MAILBOX_ADDRESS") {
		t.Fatalf("mail filter: %d %s", recorder.Code, recorder.Body)
	}
}
