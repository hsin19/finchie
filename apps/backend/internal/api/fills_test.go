package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
	pt "github.com/hsin19/finchie/apps/backend/internal/parsertest"
)

// Synthetic figures (ADR 0006).

func postJSON(t *testing.T, e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, request)
	return recorder
}

// netOn is what the owner was worth at the end of a day.
func netOn(t *testing.T, e *echo.Echo, day string) string {
	t.Helper()
	return decode[NetWorth](t, get(t, e, "/api/net-worth?on="+day)).Net
}

const augustsGap = `{"unit": "TWD", "from": "2026-07-31", "to": "2026-09-30"}`

// storeJuly stores July's statement with a line, so the account is one whose statements show its lines:
// a balance that moves with none to show it is a gap.
func storeJuly(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	storeDeposit(t, pool, 7, "1000", depositLine{day: 10, amount: "200", after: "1000", description: "利息"})
}

func TestTheOwnerFillsAGapWhoseReportsAreLost(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	// August's statement never arrived: 500 came in that no line shows.
	storeJuly(t, pool)
	storeDeposit(t, pool, 9, "1500")
	account := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), savings)
	path := fmt.Sprintf("/api/accounts/%d", account.Id)
	if len(account.Gaps) != 1 || account.Gaps[0].Unseen != "500" {
		t.Fatalf("gaps = %+v", account.Gaps)
	}
	if got := netOn(t, e, "2026-09-15"); got != "1000.00" {
		t.Fatalf("net worth mid-September before the fill = %s, want July's balance", got)
	}

	recorder := postJSON(t, e, path+"/fills", augustsGap)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("fill: %d %s", recorder.Code, recorder.Body)
	}
	fill := decode[GapFill](t, recorder)
	// The last day no statement covers: September's starts on 9/1.
	if fill.Amount != "500" || fill.On.String() != "2026-08-31" || fill.From.String() != "2026-07-31" || fill.To.String() != "2026-09-30" {
		t.Fatalf("fill = %+v", fill)
	}

	detail := decode[AccountDetail](t, get(t, e, path))
	if len(detail.Gaps) != 0 || len(detail.Fills) != 1 || detail.Fills[0].Id != fill.Id {
		t.Fatalf("after the fill: gaps %+v, fills %+v", detail.Gaps, detail.Fills)
	}
	if len(accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), savings).Gaps) != 0 {
		t.Fatal("the account list still flags the gap")
	}
	// Counted from the day it moved: the rest of September is right, the day before it is as it was.
	if got := netOn(t, e, "2026-09-15"); got != "1500.00" {
		t.Fatalf("net worth mid-September = %s, want the fill counted", got)
	}
	if got := netOn(t, e, "2026-08-30"); got != "1000.00" {
		t.Fatalf("net worth the day before the fill = %s", got)
	}
	// Among the account's lines, one for the whole period, as a passbook prints what went unprinted: on
	// the last day no statement covers, with the balance it leaves, above July's line.
	lines := decode[[]AccountLine](t, get(t, e, path+"/lines"))
	if len(lines) != 2 || lines[0].Fill == nil || lines[0].Fill.Id != fill.Id || lines[0].Fill.From.String() != "2026-07-31" ||
		lines[0].StatementId != nil || lines[0].Date.String() != "2026-08-31" || lines[0].Amount != "500" ||
		lines[0].Balance == nil || *lines[0].Balance != "1500" || lines[1].Description != "利息" || lines[1].Fill != nil {
		t.Fatalf("lines = %+v", lines)
	}
	// It is no transaction: no search or total sees it.
	if lines := decode[TransactionSearch](t, get(t, e, "/api/transactions")); len(lines.Items) != 1 || lines.Items[0].Description != "利息" {
		t.Fatalf("transactions = %+v, want July's line alone", lines.Items)
	}
	if recorder := postJSON(t, e, path+"/fills", augustsGap); recorder.Code != http.StatusConflict {
		t.Fatalf("filling it again: %d %s", recorder.Code, recorder.Body)
	}

	// August's statement turns up after all: its lines show the 500, and the fill no longer counts.
	storeDeposit(t, pool, 8, "1500", depositLine{day: 15, amount: "500", after: "1500", description: "薪資"})
	detail = decode[AccountDetail](t, get(t, e, path))
	if len(detail.Gaps) != 0 || len(detail.Fills) != 0 {
		t.Fatalf("with August's statement: gaps %+v, fills %+v", detail.Gaps, detail.Fills)
	}
	for _, line := range decode[[]AccountLine](t, get(t, e, path+"/lines")) {
		if line.Fill != nil {
			t.Fatalf("the fill is still a line once August's statement covers its days: %+v", line)
		}
	}
	if got := netOn(t, e, "2026-09-15"); got != "1500.00" {
		t.Fatalf("net worth mid-September = %s, want the 500 counted once", got)
	}
}

func TestTakingAFillBackShowsTheGapAgain(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	storeJuly(t, pool)
	storeDeposit(t, pool, 9, "1500")
	account := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), savings)
	path := fmt.Sprintf("/api/accounts/%d", account.Id)
	fill := decode[GapFill](t, postJSON(t, e, path+"/fills", augustsGap))

	fillPath := fmt.Sprintf("%s/fills/%d", path, fill.Id)
	if recorder := del(t, e, fillPath); recorder.Code != http.StatusNoContent {
		t.Fatalf("take back: %d %s", recorder.Code, recorder.Body)
	}
	if detail := decode[AccountDetail](t, get(t, e, path)); len(detail.Gaps) != 1 || len(detail.Fills) != 0 {
		t.Fatalf("after taking it back: gaps %+v, fills %+v", detail.Gaps, detail.Fills)
	}
	if recorder := del(t, e, fillPath); recorder.Code != http.StatusNotFound {
		t.Fatalf("take back again: %d", recorder.Code)
	}
}

func TestOnlyAGapAnAccountHasCanBeFilled(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	storeJuly(t, pool)
	storeDeposit(t, pool, 8, "1000")
	account := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), savings)

	// July and August join up: nothing to fill.
	joined := `{"unit": "TWD", "from": "2026-07-31", "to": "2026-08-31"}`
	if recorder := postJSON(t, e, fmt.Sprintf("/api/accounts/%d/fills", account.Id), joined); recorder.Code != http.StatusConflict {
		t.Fatalf("fill a balance that joins: %d %s", recorder.Code, recorder.Body)
	}
	if recorder := postJSON(t, e, "/api/accounts/999/fills", augustsGap); recorder.Code != http.StatusNotFound {
		t.Fatalf("fill on no account: %d %s", recorder.Code, recorder.Body)
	}
}

func patchJSON(t *testing.T, e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, request)
	return recorder
}

func TestAFillCarriesTheOwnersMemo(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	storeJuly(t, pool)
	storeDeposit(t, pool, 9, "1500")
	account := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), savings)
	path := fmt.Sprintf("/api/accounts/%d", account.Id)

	// Filled in one click, with nothing to write; a memo is for later.
	fill := decode[GapFill](t, postJSON(t, e, path+"/fills", augustsGap))
	if fill.Memo != nil {
		t.Fatalf("memo of a fill made in one click = %q", *fill.Memo)
	}
	fillPath := fmt.Sprintf("%s/fills/%d", path, fill.Id)
	if recorder := patchJSON(t, e, fillPath, `{"memo": "  八月的信被刪了  "}`); recorder.Code != http.StatusNoContent {
		t.Fatalf("set memo: %d %s", recorder.Code, recorder.Body)
	}
	if fills := decode[AccountDetail](t, get(t, e, path)).Fills; len(fills) != 1 || fills[0].Memo == nil || *fills[0].Memo != "八月的信被刪了" {
		t.Fatalf("fills = %+v", fills)
	}
	if lines := decode[[]AccountLine](t, get(t, e, path+"/lines")); lines[0].Fill == nil || lines[0].Memo == nil || *lines[0].Memo != "八月的信被刪了" {
		t.Fatalf("the fill's line = %+v, want its memo", lines[0])
	}

	// September read differently: the fill is set aside and the gap shows again. Filled again, it keeps
	// what the owner wrote.
	storeDeposit(t, pool, 9, "1600")
	if gaps := decode[AccountDetail](t, get(t, e, path)).Gaps; len(gaps) != 1 || gaps[0].Unseen != "600" {
		t.Fatalf("gaps after September changed = %+v", gaps)
	}
	again := decode[GapFill](t, postJSON(t, e, path+"/fills", augustsGap))
	if again.Amount != "600" || again.Memo == nil || *again.Memo != "八月的信被刪了" {
		t.Fatalf("filled again = %+v", again)
	}

	// An empty one clears it.
	if recorder := patchJSON(t, e, fillPath, `{"memo": " "}`); recorder.Code != http.StatusNoContent {
		t.Fatalf("clear memo: %d %s", recorder.Code, recorder.Body)
	}
	if fills := decode[AccountDetail](t, get(t, e, path)).Fills; len(fills) != 1 || fills[0].Memo != nil {
		t.Fatalf("fills after clearing = %+v", fills)
	}
	if recorder := patchJSON(t, e, path+"/fills/999", `{"memo": "x"}`); recorder.Code != http.StatusNotFound {
		t.Fatalf("memo on no fill: %d", recorder.Code)
	}
}

func TestAFillIsNeverTakenForATransfer(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	// 6,350 left the account in August with no line to show it: just what a card bill paid that day.
	storeJuly(t, pool)
	storeDeposit(t, pool, 9, "-5350")
	account := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), savings)
	path := fmt.Sprintf("/api/accounts/%d", account.Id)
	if recorder := postJSON(t, e, path+"/fills", augustsGap); recorder.Code != http.StatusCreated {
		t.Fatalf("fill: %d %s", recorder.Code, recorder.Body)
	}
	store(t, pool, "bill.eml", pt.Response("esun.credit_card", pt.Bill("esun", pt.Day(9, 7), pt.Day(9, 22), "0",
		pt.CardLine(pt.Day(8, 31), "繳款", parserv2.LineKind_LINE_KIND_PAYMENT, "-6350"))))

	for _, line := range decode[[]AccountLine](t, get(t, e, path+"/lines")) {
		if line.Fill != nil && line.Counterpart != nil {
			t.Fatalf("the fill was linked to %+v: it is what moved over a month, net, not a payment", line.Counterpart)
		}
	}
}
