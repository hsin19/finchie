package notify_test

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
	"github.com/hsin19/finchie/apps/backend/internal/ingest"
	"github.com/hsin19/finchie/apps/backend/internal/notify"
	pt "github.com/hsin19/finchie/apps/backend/internal/parsertest"
	"github.com/hsin19/finchie/apps/backend/internal/push"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

// Synthetic figures (ADR 0006).

func store(t *testing.T, pool *pgxpool.Pool, name string, response *parserv2.ParseDocumentResponse) {
	t.Helper()
	doc, err := dbq.New(pool).UpsertDocument(t.Context(), dbq.UpsertDocumentParams{
		Sha256: []byte(name), Filename: name, ContentType: "message/rfc822", Content: []byte("From: bank"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.Store(t.Context(), pool, doc.ID, "", response); err != nil {
		t.Fatal(err)
	}
}

// storeAutopay stores E.SUN's savings, which paid September's bill on 9/22 and held 5,000 at 9/30, and
// October's bill of 9,000, due 10/22.
func storeAutopay(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	paid := pt.DepositLine(pt.Day(9, 22), "玉山卡款扣繳", "TWD", "-6350", "")
	store(t, pool, "deposit.eml", pt.Response("esun.deposit", pt.Deposit("esun", pt.Day(9, 1), pt.Day(9, 30),
		pt.DepositAccount("1234567***890", "臺幣活存", []*parserv2.Balance{pt.Balance("TWD", pt.Day(9, 30), "5000")}, paid))))
	store(t, pool, "september.eml", pt.Response("esun.credit_card", pt.Bill("esun", pt.Day(9, 7), pt.Day(9, 22), "6350")))
	store(t, pool, "october.eml", pt.Response("esun.credit_card", pt.Bill("esun", pt.Day(10, 7), pt.Day(10, 22), "9000")))
}

// evaluate works notifications out in the morning of a day in Taipei, and lists the shortfalls.
func evaluate(t *testing.T, pool *pgxpool.Pool, day string) []dbq.ListNotificationsRow {
	t.Helper()
	on, err := time.Parse(time.DateOnly, day)
	if err != nil {
		t.Fatal(err)
	}
	if err := notify.Evaluate(t.Context(), pool, taipei.At(on, 9, 0)); err != nil {
		t.Fatal(err)
	}
	rows, err := dbq.New(pool).ListNotifications(t.Context(), dbq.ListNotificationsParams{RowLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var shortfalls []dbq.ListNotificationsRow
	for _, r := range rows {
		if r.Kind == domain.NoticeShortfall {
			shortfalls = append(shortfalls, r)
		}
	}
	return shortfalls
}

func describe(rows []dbq.ListNotificationsRow) string {
	var out []string
	for _, r := range rows {
		out = append(out, fmt.Sprintf("#%d handled %v resolved %v", r.ID, r.HandledAt.Valid, r.ResolvedAt.Valid))
	}
	return fmt.Sprint(out)
}

func TestASituationIsRaisedOnceKeepsWhatTheOwnerDidAndResolvesByItself(t *testing.T) {
	pool := testdb.Pool(t)
	storeAutopay(t, pool)

	raised := evaluate(t, pool, "2026-10-15")
	if len(raised) != 1 || raised[0].ResolvedAt.Valid || raised[0].ReadAt.Valid {
		t.Fatalf("raised = %s, want one open, unread", describe(raised))
	}
	var facts notify.Facts
	if err := json.Unmarshal(raised[0].Facts, &facts); err != nil {
		t.Fatal(err)
	}
	if s := facts.Shortfall; s == nil || s.Lacks != "4000" || s.Balance != "5000" || s.AsOf != "2026-09-30" || len(s.Debits) != 1 ||
		s.Debits[0].DueDate != "2026-10-22" || raised[0].AccountID == nil {
		t.Fatalf("facts = %+v, want 4,000 short of October's bill", facts.Shortfall)
	}
	id := raised[0].ID

	// Worked out again, the same situation is the same notification; the owner's word on it stays.
	if err := dbq.New(pool).HandleNotifications(t.Context(), dbq.HandleNotificationsParams{Ids: []int64{id}, Handled: true}); err != nil {
		t.Fatal(err)
	}
	if again := evaluate(t, pool, "2026-10-16"); len(again) != 1 || again[0].ID != id || !again[0].HandledAt.Valid || !again[0].ReadAt.Valid {
		t.Fatalf("again = %s, want #%d handled and read", describe(again), id)
	}

	// Once the bill is due, nothing is still to come out of the account: resolved, nobody clearing it.
	if due := evaluate(t, pool, "2026-10-23"); len(due) != 1 || !due[0].ResolvedAt.Valid {
		t.Fatalf("past the due date = %s, want resolved", describe(due))
	}

	// So again is news: raised anew, as the owner has not seen it.
	if back := evaluate(t, pool, "2026-10-20"); len(back) != 1 || back[0].ID != id || back[0].ResolvedAt.Valid || back[0].HandledAt.Valid || back[0].ReadAt.Valid {
		t.Fatalf("so again = %s, want #%d open and new", describe(back), id)
	}
}

func TestANewerBalanceThatStillFallsShortIsANewSituation(t *testing.T) {
	pool := testdb.Pool(t)
	storeAutopay(t, pool)
	first := evaluate(t, pool, "2026-10-15")

	// October's statement shows the bill not taken yet and the balance still short of it.
	store(t, pool, "deposit-october.eml", pt.Response("esun.deposit", pt.Deposit("esun", pt.Day(10, 1), pt.Day(10, 20),
		pt.DepositAccount("1234567***890", "臺幣活存", []*parserv2.Balance{pt.Balance("TWD", pt.Day(10, 20), "6000")},
			pt.DepositLine(pt.Day(10, 5), "薪資", "TWD", "1000", "")))))
	rows := evaluate(t, pool, "2026-10-21")
	if len(rows) != 2 || rows[0].ID == first[0].ID || rows[0].ResolvedAt.Valid || !rows[1].ResolvedAt.Valid {
		t.Fatalf("rows = %s, want a new one open and #%d resolved", describe(rows), first[0].ID)
	}
}

// subscribed subscribes a device whose push service counts what it is sent.
func subscribed(t *testing.T, pool *pgxpool.Pool) *atomic.Int32 {
	t.Helper()
	var received atomic.Int32
	service := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(service.Close)
	previous := push.Client
	push.Client = service.Client()
	t.Cleanup(func() { push.Client = previous })

	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encode := base64.RawURLEncoding.EncodeToString
	if err := push.Subscribe(t.Context(), dbq.New(pool), service.URL+"/device", encode(key.PublicKey().Bytes()), encode(make([]byte, 16))); err != nil {
		t.Fatal(err)
	}
	return &received
}

func TestWhatIsRaisedIsPushedAsItHappensAndNeverAgain(t *testing.T) {
	pool := testdb.Pool(t)
	storeAutopay(t, pool)
	received := subscribed(t, pool)

	evaluate(t, pool, "2026-10-15")
	if got := received.Load(); got != 1 {
		t.Fatalf("pushes after the shortfall was raised = %d, want 1", got)
	}
	evaluate(t, pool, "2026-10-16")
	if got := received.Load(); got != 1 {
		t.Fatalf("pushes after working it out again = %d, want still 1", got)
	}
	// Resolved, and so again: news, pushed again.
	evaluate(t, pool, "2026-10-23")
	evaluate(t, pool, "2026-10-20")
	if got := received.Load(); got != 2 {
		t.Fatalf("pushes after it came back = %d, want 2", got)
	}
}

func TestNothingIsPushedWhileADocumentIsBeingParsed(t *testing.T) {
	pool := testdb.Pool(t)
	storeAutopay(t, pool)
	received := subscribed(t, pool)
	// A document just uploaded is pending until its parse job is done.
	if _, err := dbq.New(pool).UpsertDocument(t.Context(), dbq.UpsertDocumentParams{
		Sha256: []byte("arriving"), Filename: "arriving.eml", ContentType: "message/rfc822", Content: []byte("From: bank"),
	}); err != nil {
		t.Fatal(err)
	}

	if rows := evaluate(t, pool, "2026-10-15"); len(rows) != 1 || rows[0].ResolvedAt.Valid {
		t.Fatalf("rows = %s, want the shortfall raised all the same", describe(rows))
	}
	if got := received.Load(); got != 0 {
		t.Fatalf("pushes while parsing = %d, want none", got)
	}
}
