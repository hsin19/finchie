package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
	pt "github.com/hsin19/finchie/apps/backend/internal/parsertest"
)

func send(t *testing.T, e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, request)
	return recorder
}

func accountOf(t *testing.T, e *echo.Echo, id int64) AccountDetail {
	t.Helper()
	return decode[AccountDetail](t, get(t, e, fmt.Sprintf("/api/accounts/%d", id)))
}

// open creates an account the owner keeps and returns its id.
func open(t *testing.T, e *echo.Echo, kind AccountKind, name string) int64 {
	t.Helper()
	created := send(t, e, http.MethodPost, "/api/accounts", fmt.Sprintf(`{"kind": %q, "name": %q}`, kind, name))
	if created.Code != http.StatusCreated {
		t.Fatalf("create %s: %d %s", name, created.Code, created.Body)
	}
	return decode[AccountCreated](t, created).Id
}

// hold reports what an account the owner keeps holds today.
func hold(t *testing.T, e *echo.Echo, id int64, holdings string) {
	t.Helper()
	if got := send(t, e, http.MethodPut, fmt.Sprintf("/api/accounts/%d/holdings", id), `{"holdings": `+holdings+`}`); got.Code != http.StatusNoContent {
		t.Fatalf("holdings %s: %d %s", holdings, got.Code, got.Body)
	}
}

func heldOf(a AccountDetail) map[string]string {
	held := map[string]string{}
	for _, b := range a.Balances {
		held[b.Unit] = b.Amount
	}
	return held
}

func TestTheOwnerKeepsAnAccountOfAnyKindHoldingWhatTheyReport(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	id := open(t, e, AccountKindPoints, "飯店會員")
	// No unit to begin with: it holds nothing yet.
	// The name it is opened with is its own, as a bank's account has the bank's.
	if a := accountOf(t, e, id); a.Institution != "owner" || *a.Label != "飯店會員" || a.Name != nil || a.Number != "" || len(a.Balances) != 0 || a.Worth != nil {
		t.Fatalf("new account = %+v", a)
	}

	// Several programs, each with what will lapse when.
	hold(t, e, id, `[{"unit": "飯店點數", "amount": "40,000", "expiries": [{"amount": "40000", "by": "2099-02-01"}]},
		{"unit": "免費房券", "amount": "2", "expiries": []}]`)
	a := accountOf(t, e, id)
	if held := heldOf(a); len(held) != 2 || held["飯店點數"] != "40000" || held["免費房券"] != "2" || a.Balances[0].StatementId != nil {
		t.Fatalf("balances = %+v", a.Balances)
	}
	// Snapshots the owner gives have no lines to join them.
	if a.Continuity != Unknown || len(a.Expiries) != 1 || a.Expiries[0].Unit != "飯店點數" || a.Expiries[0].By.String() != "2099-02-01" {
		t.Fatalf("continuity %s, expiries %+v", a.Continuity, a.Expiries)
	}

	// Later the same day the day's snapshot is replaced whole: the program no longer listed is gone.
	hold(t, e, id, `[{"unit": "飯店點數", "amount": "45000", "expiries": []}]`)
	if a := accountOf(t, e, id); len(a.Balances) != 1 || a.Balances[0].Amount != "45000" || len(a.History) != 1 || len(a.Expiries) != 0 {
		t.Fatalf("after update: balances %+v, history %+v, expiries %+v", a.Balances, a.History, a.Expiries)
	}

	// Of any kind but a card's; its units are the kind's: money upper case, like a source's.
	cash := open(t, e, AccountKindDeposit, "現金")
	hold(t, e, cash, `[{"unit": "twd", "amount": "1500", "expiries": []}, {"unit": "JPY", "amount": "3000", "expiries": []}]`)
	if held := heldOf(accountOf(t, e, cash)); held["TWD"] != "1500" || held["JPY"] != "3000" {
		t.Fatalf("cash = %v", held)
	}
	if got := send(t, e, http.MethodPost, "/api/accounts", `{"kind": "credit_card", "name": "卡"}`).Code; got != http.StatusBadRequest {
		t.Errorf("a card account: %d", got)
	}

	for body, want := range map[string]int{
		`[{"unit": " ", "amount": "1", "expiries": []}]`:                                               http.StatusBadRequest,
		`[{"unit": "A", "amount": "many", "expiries": []}]`:                                            http.StatusBadRequest,
		`[{"unit": "A", "amount": "1", "expiries": []}, {"unit": "A", "amount": "2", "expiries": []}]`: http.StatusBadRequest,
	} {
		if got := send(t, e, http.MethodPut, fmt.Sprintf("/api/accounts/%d/holdings", id), `{"holdings": `+body+`}`).Code; got != want {
			t.Errorf("%s: %d, want %d", body, got, want)
		}
	}
	// What a source reports is not kept by hand.
	storeDeposit(t, pool, 9, "1000")
	deposit := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), savings)
	if got := send(t, e, http.MethodPut, fmt.Sprintf("/api/accounts/%d/holdings", deposit.Id), `{"holdings": []}`).Code; got != http.StatusBadRequest {
		t.Errorf("a statement's account: %d", got)
	}
	if got := send(t, e, http.MethodPut, "/api/accounts/999999/holdings", `{"holdings": []}`).Code; got != http.StatusNotFound {
		t.Errorf("no such account: %d", got)
	}
}

func TestNoTwoAccountsHaveOneNameAndOneWithoutAPrintedNumberTakesAnyAsItsOwn(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	mine := open(t, e, AccountKindPoints, "我的會員")
	theirs := open(t, e, AccountKindPoints, "家人的會員")
	// One program in both, each with its own points.
	hold(t, e, mine, `[{"unit": "飯店點數", "amount": "1000", "expiries": []}]`)
	hold(t, e, theirs, `[{"unit": "飯店點數", "amount": "250", "expiries": []}]`)
	if m, o := accountOf(t, e, mine), accountOf(t, e, theirs); m.Balances[0].Amount != "1000" || o.Balances[0].Amount != "250" ||
		*m.Balances[0].UnitId != *o.Balances[0].UnitId {
		t.Fatalf("mine %+v, theirs %+v", m.Balances, o.Balances)
	}

	if got := send(t, e, http.MethodPost, "/api/accounts", `{"kind": "points", "name": " 我的會員 "}`).Code; got != http.StatusConflict {
		t.Errorf("a name taken: %d", got)
	}
	// Any account's name, a source's included.
	storeDeposit(t, pool, 9, "1000")
	deposit := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), savings)
	path := fmt.Sprintf("/api/accounts/%d", theirs)
	for body, want := range map[string]int{
		`{"name": "我的會員"}`: http.StatusConflict,
		`{"name": "家人"}`:   http.StatusNoContent,
		// No source prints its number: the owner's membership number, as typed.
		`{"fullNumber": " MB-1234 "}`: http.StatusNoContent,
	} {
		if got := send(t, e, http.MethodPatch, path, body).Code; got != want {
			t.Errorf("%s: %d, want %d", body, got, want)
		}
	}
	if got := send(t, e, http.MethodPatch, fmt.Sprintf("/api/accounts/%d", deposit.Id), `{"name": "家人"}`).Code; got != http.StatusConflict {
		t.Errorf("a source's account taking the name: %d", got)
	}
	if a := accountOf(t, e, theirs); *a.Name != "家人" || *a.FullNumber != "MB-1234" {
		t.Fatalf("renamed: %v %v", *a.Name, a.FullNumber)
	}

	// Cleared, an account goes by its own name again, unless another has taken it meanwhile.
	if got := send(t, e, http.MethodPatch, fmt.Sprintf("/api/accounts/%d", deposit.Id), `{"name": "家人的會員"}`).Code; got != http.StatusNoContent {
		t.Fatalf("a source's account taking the name let go: %d", got)
	}
	if got := send(t, e, http.MethodPatch, path, `{"name": ""}`).Code; got != http.StatusConflict {
		t.Errorf("cleared to a name taken: %d", got)
	}
	if got := send(t, e, http.MethodPatch, fmt.Sprintf("/api/accounts/%d", deposit.Id), `{"name": ""}`).Code; got != http.StatusNoContent {
		t.Fatalf("clearing the source's account's name: %d", got)
	}
	if got := send(t, e, http.MethodPatch, path, `{"name": ""}`).Code; got != http.StatusNoContent {
		t.Fatalf("cleared: %d", got)
	}
	if a := accountOf(t, e, theirs); a.Name != nil || *a.Label != "家人的會員" {
		t.Fatalf("cleared: name %v, label %v", a.Name, a.Label)
	}
}

// storeDollars stores a deposit statement holding US$10 in 7777777***777.
func storeDollars(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	store(t, pool, "dollars", pt.Response("esun.deposit", pt.Deposit("esun", pt.Day(10, 1), pt.Day(10, 31),
		pt.DepositAccount("7777777***777", "外幣活存", []*parserv2.Balance{pt.Balance("USD", pt.Day(10, 31), "10")}))))
}

func pointsOf(t *testing.T, e *echo.Echo) Account {
	t.Helper()
	for _, a := range decode[[]Account](t, get(t, e, "/api/accounts")) {
		if a.Kind == AccountKindPoints {
			return a
		}
	}
	t.Fatal("no points account")
	return Account{}
}
