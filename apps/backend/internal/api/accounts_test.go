package api

import (
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	gdate "google.golang.org/genproto/googleapis/type/date"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/exchanges"
	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
	"github.com/hsin19/finchie/apps/backend/internal/ingest"
	pt "github.com/hsin19/finchie/apps/backend/internal/parsertest"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// Synthetic figures (ADR 0006).
const savings = "1234567***890"

type depositLine struct {
	day            int32
	amount, after  string
	description    string
	memo, opposite string
}

// storeDeposit stores one month's statement of the savings account the way the ingest worker would.
func storeDeposit(t *testing.T, pool *pgxpool.Pool, month int32, closing string, lines ...depositLine) {
	t.Helper()
	doc, err := dbq.New(pool).UpsertDocument(t.Context(), dbq.UpsertDocumentParams{
		Sha256: fmt.Appendf(nil, "deposit-%d", month), Filename: "deposit.eml", ContentType: "message/rfc822", Content: []byte("From: bank"),
	})
	if err != nil {
		t.Fatal(err)
	}
	last := map[int32]int32{4: 30, 6: 30, 9: 30, 11: 30}[month]
	if last == 0 {
		last = 31
	}
	account := pt.DepositAccount(savings, "臺幣活存", []*parserv2.Balance{pt.Balance("TWD", pt.Day(month, last), closing)})
	for _, l := range lines {
		account.Lines = append(account.Lines, pt.DepositLine(pt.Day(month, l.day), l.description, "TWD", l.amount, l.after,
			pt.Memo(l.memo), pt.Counterparty(l.opposite)))
	}
	statement := pt.Deposit("esun", pt.Day(month, 1), pt.Day(month, last), account)
	if _, err := ingest.Store(t.Context(), pool, doc.ID, "", pt.Response("esun.deposit", statement)); err != nil {
		t.Fatal(err)
	}
}

func accountNamed(t *testing.T, accounts []Account, number string) Account {
	t.Helper()
	for _, a := range accounts {
		if a.Number == number {
			return a
		}
	}
	t.Fatalf("no account %q in %+v", number, accounts)
	return Account{}
}

func TestAMonthSkippedWithoutActivityIsNoGap(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	storeDeposit(t, pool, 7, "1000", depositLine{day: 5, amount: "1000", after: "1000", description: "薪資"})
	// No statement for August: the bank sends none for a month without activity.
	storeDeposit(t, pool, 9, "1000")

	account := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), savings)
	if account.Continuity != Checked || len(account.Gaps) != 0 {
		t.Fatalf("account = %+v, want checked and no gap", account)
	}
}

func TestAnAccountItsStatementsListOnlyTheBalanceOfIsNotChecked(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	line := func(month int32, amount string) *parserv2.Line {
		return pt.DepositLine(pt.Day(month, 1), "轉入", "TWD", amount, "")
	}
	// As CTBC's: a passbook savings account (活儲有摺) by its balance only, beside an account with lines
	// and one no line has moved yet.
	statement := func(month int32, passbook, current string, lines ...*parserv2.Line) *parserv2.ParseDocumentResponse {
		closing := pt.Day(month, 12)
		return pt.Response("ctbc.deposit", pt.Deposit("ctbc", pt.Day(month-1, 13), closing,
			pt.DepositAccount("11111**2222*", "活儲有摺", []*parserv2.Balance{pt.Balance("TWD", closing, passbook)}),
			pt.DepositAccount("33333**4444*", "台幣數二", []*parserv2.Balance{pt.Balance("TWD", closing, current)}, lines...),
			pt.DepositAccount("55555**6666*", "外幣活期", []*parserv2.Balance{pt.Balance("USD", closing, "12.34")}),
		))
	}
	store(t, pool, "june.eml", statement(6, "1000", "500", line(6, "500")))
	// The passbook account earned 26 no statement lists; 100 came into the other one unseen too.
	store(t, pool, "july.eml", statement(7, "1026", "800", line(7, "200")))

	accounts := decode[[]Account](t, get(t, e, "/api/accounts"))
	if passbook := accountNamed(t, accounts, "11111**2222*"); passbook.Continuity != Unknown || len(passbook.Gaps) != 0 {
		t.Fatalf("passbook account = %+v, want unknown and no gap", passbook)
	}
	if current := accountNamed(t, accounts, "33333**4444*"); current.Continuity != Checked || len(current.Gaps) != 1 || current.Gaps[0].Unseen != "100" {
		t.Fatalf("account with lines = %+v, want checked with a gap of 100", current)
	}
	// A balance that never moved joins up whether or not lines would have shown a move.
	if quiet := accountNamed(t, accounts, "55555**6666*"); quiet.Continuity != Checked || len(quiet.Gaps) != 0 {
		t.Fatalf("quiet account = %+v, want checked and no gap", quiet)
	}
}

func TestAccountsShowTheirNewestBalanceAndWhereMoneyMovedUnseen(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	storeDeposit(t, pool, 8, "1000", depositLine{day: 5, amount: "1000", after: "1000", description: "薪資"})
	storeDeposit(t, pool, 9, "1500", depositLine{day: 5, amount: "500", after: "1500", description: "ATM跨行轉", memo: "永*銀行", opposite: "807 0000000000***123"})
	// October's statement is missing, and 800 came in that month: November starts from 2300, not 1500.
	storeDeposit(t, pool, 11, "2500", depositLine{day: 3, amount: "200", after: "2500", description: "利息"})
	bill := pt.Bill("esun", pt.Day(10, 7), pt.Day(10, 22), "300",
		pt.CardLine(pt.Day(9, 30), "SHOP", parserv2.LineKind_LINE_KIND_PURCHASE, "300"))
	bill.Checks = []*parserv2.Check{pt.Check("x", "TWD", "300", "300")}
	doc, err := dbq.New(pool).UpsertDocument(t.Context(), dbq.UpsertDocumentParams{Sha256: []byte("bill"), Filename: "bill.eml", ContentType: "message/rfc822", Content: []byte("From: bank")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.Store(t.Context(), pool, doc.ID, "", pt.Response("esun.credit_card", bill)); err != nil {
		t.Fatal(err)
	}

	accounts := decode[[]Account](t, get(t, e, "/api/accounts"))
	deposit := accountNamed(t, accounts, savings)
	if deposit.Kind != AccountKindDeposit || *deposit.Label != "臺幣活存" || deposit.Continuity != Checked {
		t.Fatalf("deposit account = %+v", deposit)
	}
	if len(deposit.Balances) != 1 || deposit.Balances[0].Amount != "2500" || deposit.Balances[0].AsOf.String() != "2026-11-30" {
		t.Fatalf("newest balance = %+v", deposit.Balances)
	}
	if len(deposit.Gaps) != 1 {
		t.Fatalf("gaps = %+v", deposit.Gaps)
	}
	if gap := deposit.Gaps[0]; gap.From.String() != "2026-09-30" || gap.To.String() != "2026-11-30" || gap.Unseen != "800" || gap.Unit != "TWD" {
		t.Fatalf("gap = %+v", gap)
	}
	// The bank's card account is a liability: what its bill owes, taken off (ADR 0022).
	var card Account
	for _, a := range accounts {
		if a.Kind == AccountKindCreditCard {
			card = a
		}
	}
	if card.Side != Liability || card.Worth == nil || *card.Worth != "-300.00" || len(card.Balances) != 1 || card.Balances[0].Amount != "300" {
		t.Fatalf("card account = %+v", card)
	}
	if deposit.Side != Asset {
		t.Fatalf("deposit side = %s", deposit.Side)
	}
	net := decode[NetWorth](t, get(t, e, "/api/net-worth?on=2026-11-30"))
	if net.Assets != "2500.00" || net.Liabilities != "-300.00" || net.Net != "2200.00" || net.Unvalued != 0 || len(net.ByKind) != 2 ||
		net.ByKind[0].Kind != AccountKindDeposit || net.ByKind[1].Kind != AccountKindCreditCard {
		t.Fatalf("net worth = %+v", net)
	}
	// The bill closed on 10/7, the deposit statement on 11/30: exact up to 10/7, and the card is behind after it.
	if net.CompleteAsOf == nil || net.CompleteAsOf.String() != "2026-10-07" || len(net.Behind) != 1 || net.Behind[0] != card.Id {
		t.Fatalf("complete as of %v, behind %v", net.CompleteAsOf, net.Behind)
	}

	path := fmt.Sprintf("/api/accounts/%d", deposit.Id)
	detail := decode[AccountDetail](t, get(t, e, path))
	if len(detail.History) != 3 || detail.History[0].AsOf.String() != "2026-11-30" || len(detail.Gaps) != 1 {
		t.Fatalf("detail = %+v", detail)
	}
	lines := decode[[]AccountLine](t, get(t, e, path+"/lines"))
	if len(lines) != 3 || lines[0].Description != "利息" || *lines[0].Balance != "2500" {
		t.Fatalf("lines = %+v", lines)
	}
	if transfer := lines[1]; *transfer.Memo != "永*銀行" || *transfer.Counterparty != "807 0000000000***123" || transfer.Amount != "500" {
		t.Fatalf("transfer = %+v", transfer)
	}

	for _, missing := range []string{"/api/accounts/999", "/api/accounts/999/lines"} {
		if got := get(t, e, missing).Code; got != http.StatusNotFound {
			t.Errorf("%s: status = %d", missing, got)
		}
	}
}

func TestBalancesAreWorthTheirAmountAtTheNewestRateAndAccountsAddThemUp(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	ctx := t.Context()
	queries := dbq.New(pool)
	rate(t, pool, "2026-11-06", "USD", "0.025")
	rate(t, pool, "2026-11-07", "USD", "0.03125") // the newest: 1 USD is NT$32
	doc, err := queries.UpsertDocument(ctx, dbq.UpsertDocumentParams{Sha256: []byte("fx"), Filename: "fx.eml", ContentType: "message/rfc822", Content: []byte("From: bank")})
	if err != nil {
		t.Fatal(err)
	}
	end := pt.Day(10, 31)
	balance := func(number string, balances ...*parserv2.Balance) *parserv2.AccountReport {
		return pt.DepositAccount(number, "外幣活存", balances)
	}
	statement := pt.Deposit("esun", pt.Day(10, 1), end,
		balance(savings, pt.Balance("TWD", end, "1000")),
		balance("7777777***777", pt.Balance("USD", end, "10.50")),
		balance("8888888***888", pt.Balance("USD", end, "100"), pt.Balance("JPY", end, "500")),
	)
	if _, err := ingest.Store(ctx, pool, doc.ID, "", pt.Response("esun.deposit", statement)); err != nil {
		t.Fatal(err)
	}

	accounts := decode[[]Account](t, get(t, e, "/api/accounts"))
	// NT$ is worth itself, with no price to name.
	if twd := accountNamed(t, accounts, savings); *twd.Worth != "1000.00" || *twd.Balances[0].Worth != "1000.00" || twd.Balances[0].Price != nil {
		t.Fatalf("NT$ account = %+v", twd)
	}
	dollars := accountNamed(t, accounts, "7777777***777")
	if *dollars.Balances[0].Worth != "336.00" || *dollars.Balances[0].Price.Twd != "32" || *dollars.Worth != "336.00" {
		t.Fatalf("US$ account = %+v", dollars)
	}
	// No rate for JPY: its balance has no worth, and so neither has the account.
	mixed := accountNamed(t, accounts, "8888888***888")
	if mixed.Worth != nil {
		t.Fatalf("an account with a unit without a rate has worth %s", *mixed.Worth)
	}
	detail := decode[AccountDetail](t, get(t, e, fmt.Sprintf("/api/accounts/%d", dollars.Id)))
	if detail.Worth == nil || *detail.Worth != "336.00" {
		t.Fatalf("detail worth = %v", detail.Worth)
	}
}

func patch(t *testing.T, e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, request)
	return recorder
}

// store stores a parsed document the way the ingest worker would, and returns the document's ID.
func store(t *testing.T, pool *pgxpool.Pool, name string, response *parserv2.ParseDocumentResponse) int64 {
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
	return doc.ID
}

func septemberLine(day int32, amount, description, counterparty string) *parserv2.Line {
	return pt.DepositLine(pt.Day(9, day), description, "TWD", amount, "", pt.Counterparty(counterparty))
}

// septemberDeposit is one account's statement for September 2026.
func septemberDeposit(institution, number, closing string, lines ...*parserv2.Line) *parserv2.ParseDocumentResponse {
	return pt.Response(institution+".deposit", pt.Deposit(institution, pt.Day(9, 1), pt.Day(9, 30),
		pt.DepositAccount(number, "活存", []*parserv2.Balance{pt.Balance("TWD", pt.Day(9, 30), closing)}, lines...)))
}

// septemberBill is E.SUN's card bill closing 9/7, due 9/22.
func septemberBill(totalDue string, lines ...*parserv2.Line) *parserv2.ParseDocumentResponse {
	return pt.Response("esun.credit_card", pt.Bill("esun", pt.Day(9, 7), pt.Day(9, 22), totalDue, lines...))
}

func cardLine(day int32, kind parserv2.LineKind, amount, description, card string) *parserv2.Line {
	return pt.CardLine(pt.Day(9, day), description, kind, amount, pt.OnCard(card))
}

func TestTheOwnerNamesAnAccountAndGivesItsFullNumberWhichMustFit(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	storeDeposit(t, pool, 9, "1000")
	id := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), savings).Id
	path := fmt.Sprintf("/api/accounts/%d", id)

	// savings is 1234567***890: the full number must keep those digits where they are.
	for _, misfit := range []string{"1234567000891", "123456700089", "12345670008900", "1234567abc890"} {
		if got := patch(t, e, path, `{"fullNumber": "`+misfit+`"}`); got.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d", misfit, got.Code)
		}
	}
	if got := patch(t, e, path, `{"name": " 薪轉 ", "fullNumber": "1234567-000-890"}`); got.Code != http.StatusNoContent {
		t.Fatalf("update: %d %s", got.Code, got.Body)
	}
	account := decode[AccountDetail](t, get(t, e, path))
	if *account.Name != "薪轉" || *account.FullNumber != "1234567000890" || account.Number != savings {
		t.Fatalf("account = name %v, full %v, number %s", account.Name, account.FullNumber, account.Number)
	}
	// A field left out stays; an empty one clears.
	if got := patch(t, e, path, `{"name": ""}`); got.Code != http.StatusNoContent {
		t.Fatalf("clear: %d", got.Code)
	}
	account = decode[AccountDetail](t, get(t, e, path))
	if account.Name != nil || account.FullNumber == nil {
		t.Fatalf("after clearing the name: name %v, full %v", account.Name, account.FullNumber)
	}
	if got := patch(t, e, "/api/accounts/999", `{"name": "x"}`); got.Code != http.StatusNotFound {
		t.Fatalf("unknown account: %d", got.Code)
	}
}

func TestTheOwnerSaysWhenToLookAgainWhetherAQuietAccountIsCurrent(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	storeDeposit(t, pool, 8, "1000")
	id := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), savings).Id
	path := fmt.Sprintf("/api/accounts/%d", id)
	checkAgainOn := func() string {
		t.Helper()
		if on := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), savings).CheckAgainOn; on != nil {
			return on.String()
		}
		return ""
	}
	today := taipei.Today(time.Now())
	later := today.AddDate(3, 0, 0).Format(time.DateOnly)

	if got := patch(t, e, path, `{"checkAgainOn": "`+later+`"}`); got.Code != http.StatusNoContent {
		t.Fatalf("set: %d %s", got.Code, got.Body)
	}
	if got := checkAgainOn(); got != later {
		t.Fatalf("checkAgainOn = %q, want %s", got, later)
	}
	// Today or before would be no reminder at all; nor is a day misspelt. Neither changes it.
	for _, wrong := range []string{today.Format(time.DateOnly), "2020-01-01", "2026/12/31", "soon"} {
		if got := patch(t, e, path, `{"checkAgainOn": "`+wrong+`"}`); got.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d", wrong, got.Code)
		}
	}
	// Other details leave it; so does parsing the same statement again, which brings nothing newer.
	if got := patch(t, e, path, `{"name": "備用"}`); got.Code != http.StatusNoContent || checkAgainOn() != later {
		t.Fatalf("a name took it with it: %d, %q", got.Code, checkAgainOn())
	}
	storeDeposit(t, pool, 8, "1000")
	if got := checkAgainOn(); got != later {
		t.Fatalf("after a re-parse: checkAgainOn = %q", got)
	}
	// A statement with a newer balance clears it, however far off.
	storeDeposit(t, pool, 9, "1000")
	if got := checkAgainOn(); got != "" {
		t.Fatalf("after a newer statement: checkAgainOn = %q", got)
	}
	// Empty clears it too.
	if got := patch(t, e, path, `{"checkAgainOn": "`+later+`"}`); got.Code != http.StatusNoContent || checkAgainOn() != later {
		t.Fatalf("set again: %d, %q", got.Code, checkAgainOn())
	}
	if got := patch(t, e, path, `{"checkAgainOn": ""}`); got.Code != http.StatusNoContent || checkAgainOn() != "" {
		t.Fatalf("cleared: %d, %q", got.Code, checkAgainOn())
	}
}

func TestLinesShowWhereTheirMoneyWentAndBillsWhenTheyWerePaid(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	// The bill closing 9/7 is paid on 9/22 from E.SUN; 5,000 goes to Taishin, whose number the line prints.
	store(t, pool, "esun.eml", septemberDeposit("esun", savings, "5000",
		septemberLine(22, "-6350", "玉山卡款扣繳", ""),
		septemberLine(25, "-5000", "行銀跨行轉優", "812 0087654321***234"),
	))
	store(t, pool, "taishin.eml", septemberDeposit("taishin", "876543****9234", "5000", septemberLine(25, "5000", "CD轉入", "")))
	store(t, pool, "bill.eml", septemberBill("6350",
		cardLine(22, parserv2.LineKind_LINE_KIND_PAYMENT, "-6350", "感謝您辦理本行自動轉帳繳款", "")))

	accounts := decode[[]Account](t, get(t, e, "/api/accounts"))
	esun, taishin := accountNamed(t, accounts, savings), accountNamed(t, accounts, "876543****9234")
	lines := decode[[]AccountLine](t, get(t, e, fmt.Sprintf("/api/accounts/%d/lines", esun.Id)))
	transfer, card := lines[0], lines[1]
	if transfer.Counterpart == nil || transfer.Counterpart.AccountId == nil || *transfer.Counterpart.AccountId != taishin.Id ||
		*transfer.Counterpart.AccountNumber != "876543****9234" {
		t.Fatalf("transfer counterpart = %+v", transfer.Counterpart)
	}
	// The bank's card account, a liability like any other account (ADR 0022).
	if card.Counterpart == nil || card.Counterpart.AccountKind != AccountKindCreditCard || card.Counterpart.AccountId == nil || card.Counterpart.Institution != "esun" {
		t.Fatalf("card payment counterpart = %+v", card.Counterpart)
	}
	statements := decode[[]Statement](t, get(t, e, "/api/statements"))
	if len(statements) != 1 || statements[0].PaidOn == nil || statements[0].PaidOn.String() != "2026-09-22" {
		t.Fatalf("bill = %+v", statements)
	}
}

func TestASecuritiesAccountIsWorthItsHoldingsAtTheNewestCloseAndItsTradesAreNoSpending(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	sold := func(day int32, name, shares, amount, net string) *parserv2.Line {
		return pt.Trade(pt.Day(7, day), name, parserv2.LineKind_LINE_KIND_SELL, "-"+shares, "50", amount, "143", "303", net)
	}
	// Synthetic (ADR 0006): 範例航運 sold out, so not held.
	store(t, pool, "skis.eml", pt.Response("skis.securities", pt.Securities("skis", "12**345", pt.Day(7, 1), pt.Day(8, 4),
		[]*parserv2.Balance{pt.Balance("範例電子", pt.Day(8, 4), "3500")},
		sold(3, "範例電子", "2000", "101000", "100554"),
		sold(15, "範例航運", "1000", "30000", "29554"),
	)))
	closed := func(d int) time.Time { return taipei.At(time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC), 13, 30) }
	price(t, pool, "security", "範例電子", "twse", closed(5), "1", "TWD", "49.00")
	price(t, pool, "security", "範例電子", "twse", closed(6), "1", "TWD", "50.50")
	if _, err := pool.Exec(t.Context(), "UPDATE units SET code = '1234', market = 'twse' WHERE kind = 'security' AND symbol = '範例電子'"); err != nil {
		t.Fatal(err)
	}

	account := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), "12**345")
	if account.Kind != AccountKindSecurities || account.Continuity != Checked || len(account.Balances) != 1 {
		t.Fatalf("account = %+v", account)
	}
	held := account.Balances[0]
	if held.Unit != "範例電子" || *held.Worth != "176750.00" || *held.Code != "1234" || held.Price.Quoted != "50.5" || !held.Price.AsOf.Equal(closed(6)) {
		t.Fatalf("holding = %+v, worth %v", held, *held.Worth)
	}
	// Held before this, the oldest statement: what either cost is not known.
	detail := decode[AccountDetail](t, get(t, e, fmt.Sprintf("/api/accounts/%d", account.Id)))
	if detail.Positions == nil || len(*detail.Positions) != 2 {
		t.Fatalf("positions = %+v", detail.Positions)
	}
	if p := (*detail.Positions)[0]; p.Unit != "範例電子" || p.Shares != "3500" || p.Worth == nil || p.Cost != nil || p.Gain != nil {
		t.Fatalf("held = %+v", p)
	}
	if p := (*detail.Positions)[1]; p.Unit != "範例航運" || p.Shares != "0" || p.Worth != nil || p.Realized != nil {
		t.Fatalf("sold out = %+v", p)
	}
	lines := decode[[]AccountLine](t, get(t, e, fmt.Sprintf("/api/accounts/%d/lines", account.Id)))
	if len(lines) != 2 || lines[0].Amount != "-1000" || lines[0].Trade == nil || lines[0].Trade.Net != "29554" {
		t.Fatalf("lines = %+v", lines)
	}
	// Shares are not money: listed, but neither spent nor received.
	search := decode[TransactionSearch](t, get(t, e, "/api/transactions"))
	if search.Total != 2 || len(search.Flows) != 0 || search.Items[0].Trade == nil {
		t.Fatalf("search = total %d, flows %+v", search.Total, search.Flows)
	}
}

func TestAnExchangeIsWorthItsHoldingsElseItsValuationOfTheSameDay(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	ctx := t.Context()
	rate(t, pool, "2026-11-07", "USDT", "0.03125")     // 1 USDT is NT$32
	rate(t, pool, "2026-11-07", "BTC", "0.0000003125") // 1 BTC NT$3,200,000
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	snapshot := func(on string, valuation string, pairs ...string) {
		t.Helper()
		s := exchanges.Snapshot{Holdings: map[string]*big.Rat{}}
		for i := 0; i < len(pairs); i += 2 {
			s.Holdings[pairs[i]], _ = new(big.Rat).SetString(pairs[i+1])
		}
		if valuation != "" {
			amount, _ := new(big.Rat).SetString(valuation)
			s.Valuation = &exchanges.Value{Unit: "USDT", Amount: amount}
		}
		if err := exchanges.Store(ctx, pool, "binance", day(on), s); err != nil {
			t.Fatal(err)
		}
	}
	exchange := func() AccountDetail {
		t.Helper()
		accounts := decode[[]Account](t, get(t, e, "/api/accounts"))
		if len(accounts) != 1 || accounts[0].Kind != AccountKindExchange || accounts[0].Continuity != Unknown {
			t.Fatalf("accounts = %+v", accounts)
		}
		detail := decode[AccountDetail](t, get(t, e, fmt.Sprintf("/api/accounts/%d", accounts[0].Id)))
		if deref(detail.Worth) != deref(accounts[0].Worth) {
			t.Fatalf("list worth %v, detail worth %v", accounts[0].Worth, detail.Worth)
		}
		return detail
	}

	// Every coin has a rate: the holdings, whatever the exchange says.
	snapshot("2026-11-08", "11000", "BTC", "0.1", "USDT", "100")
	if a := exchange(); *a.Worth != "323200.00" || *a.WorthFrom != WorthFromHoldings || a.Valuation.Amount != "11000" {
		t.Fatalf("priced holdings: worth %v from %v, valuation %+v", a.Worth, a.WorthFrom, a.Valuation)
	}

	// DUST has no rate: the exchange's valuation of the day, at USDT's rate. BTC is sold out: not held,
	// but in the history at 0, with no statement.
	snapshot("2026-11-09", "200", "USDT", "100", "DUST", "5")
	a := exchange()
	if *a.Worth != "6400.00" || *a.WorthFrom != WorthFromValuation || *a.Valuation.Worth != "6400.00" || len(a.Balances) != 2 {
		t.Fatalf("unpriced dust: worth %v from %v, balances %+v", a.Worth, a.WorthFrom, a.Balances)
	}
	if a.History[0].StatementId != nil || len(a.History) != 5 {
		t.Fatalf("history = %+v", a.History)
	}

	// No valuation that day: the one before is not the same holdings, so no worth at all.
	snapshot("2026-11-10", "", "USDT", "100", "DUST", "5")
	if a := exchange(); a.Worth != nil || a.WorthFrom != nil {
		t.Fatalf("no valuation of the day: worth %v from %v", a.Worth, a.WorthFrom)
	}
}

// cathayBill is a Cathay card bill closing on the first of the month, with its points programs.
// A points program on a bill: its closing, its period's movements, and what will lapse (ADR 0019).
type pointsProgram struct {
	name, closing string
	moves         []pointsMove
	lapses        []lapse
}

type pointsMove struct {
	label  string
	kind   parserv2.LineKind
	amount string
}

type lapse struct {
	amount string
	by     *gdate.Date
}

func cathayBill(month int32, programs ...pointsProgram) *parserv2.ParseDocumentResponse {
	closing := pt.Day(month, 1)
	bill := pt.Bill("cathay", closing, pt.Day(month, 17), "0")
	if len(programs) == 0 {
		return pt.Response("cathay.credit_card", bill)
	}
	var balances []*parserv2.Balance
	var lines []*parserv2.Line
	var expiries []*parserv2.Expiry
	for _, p := range programs {
		balances = append(balances, pt.Balance(p.name, closing, p.closing))
		for _, m := range p.moves {
			lines = append(lines, pt.PointsLine(closing, p.name, m.label, m.kind, m.amount))
		}
		for _, l := range p.lapses {
			expiries = append(expiries, pt.Expiry(p.name, l.amount, l.by))
		}
	}
	return pt.Response("cathay.credit_card", pt.Points(bill, balances, lines, expiries...))
}

func program(name, closing string, lapses ...lapse) pointsProgram {
	return pointsProgram{name: name, closing: closing, lapses: lapses}
}

// withLines gives a program its period's movements.
func withLines(p pointsProgram, moves ...pointsMove) pointsProgram {
	p.moves = moves
	return p
}

func pointsLine(label string, kind parserv2.LineKind, amount string) pointsMove {
	return pointsMove{label: label, kind: kind, amount: amount}
}

func lapsing(amount string, year, month, day int32) lapse {
	return lapse{amount: amount, by: &gdate.Date{Year: year, Month: month, Day: day}}
}

func TestACardBillsPointsAreAPointsAccountWithWhatTheNewestBillSaysWillLapse(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	// Stored newest first: the older bill's expiries must not come back.
	store(t, pool, "november", cathayBill(11,
		withLines(program("小樹點(信用卡)", "3253", lapsing("0", 2026, 11, 30), lapsing("669", 2099, 10, 31)),
			pointsLine("本期新增/扣除", parserv2.LineKind_LINE_KIND_EARN, "64"),
			pointsLine("本期兌/轉換", parserv2.LineKind_LINE_KIND_REDEEM, "-1")),
		program("亞洲萬里通里數", "0")))
	store(t, pool, "october", cathayBill(10,
		program("小樹點(信用卡)", "3190", lapsing("88", 2099, 10, 31), lapsing("700", 2099, 9, 30)),
		program("亞洲萬里通里數", "500")))
	// A bill gone by, and an amount of 0, are left out; so is the program the bank now reports at 0.
	accounts := decode[[]Account](t, get(t, e, "/api/accounts"))
	var points *Account
	for i := range accounts {
		if accounts[i].Kind == AccountKindPoints {
			points = &accounts[i]
		}
	}
	// The bills list every movement but points that lapse: 小樹點 joins up, the miles that went without a
	// line are a gap, points lost (ADR 0019).
	if points == nil || points.Institution != "cathay" || points.Continuity != Checked {
		t.Fatalf("accounts = %+v", accounts)
	}
	if len(points.Gaps) != 1 || points.Gaps[0].Unit != "亞洲萬里通里數" || points.Gaps[0].Unseen != "-500" {
		t.Fatalf("gaps = %+v", points.Gaps)
	}
	if len(points.Balances) != 1 || points.Balances[0].Unit != "小樹點(信用卡)" || points.Balances[0].Amount != "3253" {
		t.Fatalf("balances = %+v", points.Balances)
	}
	if len(points.Expiries) != 1 || points.Expiries[0].Amount != "669" || points.Expiries[0].By.String() != "2099-10-31" {
		t.Fatalf("expiries = %+v", points.Expiries)
	}
	// At the owner's NT$1 a point the migration gives 小樹點 (ADR 0020).
	if points.Worth == nil || *points.Worth != "3253.00" {
		t.Fatalf("worth = %v", points.Worth)
	}
	detail := decode[AccountDetail](t, get(t, e, fmt.Sprintf("/api/accounts/%d", points.Id)))
	if len(detail.Expiries) != 1 || len(detail.History) != 4 {
		t.Fatalf("detail expiries %+v, history %+v", detail.Expiries, detail.History)
	}
	lines := decode[[]AccountLine](t, get(t, e, fmt.Sprintf("/api/accounts/%d/lines", points.Id)))
	if len(lines) != 2 || lines[0].Kind != string(TransactionKindRedeem) || lines[0].Amount != "-1" || lines[1].Description != "本期新增/扣除" || lines[1].Date.String() != "2026-11-01" {
		t.Fatalf("lines = %+v", lines)
	}
	// Listed in search, but points are not money: neither spent nor received.
	search := decode[TransactionSearch](t, get(t, e, "/api/transactions"))
	if search.Total != 2 || len(search.Flows) != 0 {
		t.Fatalf("search: total %d, flows %+v", search.Total, search.Flows)
	}

	// Re-parsing the newest bill without its points takes them with it: the older bill's report stands.
	store(t, pool, "november", cathayBill(11))
	points2 := decode[AccountDetail](t, get(t, e, fmt.Sprintf("/api/accounts/%d", points.Id)))
	held := map[string]string{}
	for _, b := range points2.Balances {
		held[b.Unit] = b.Amount
	}
	// Soonest first.
	if len(points2.Expiries) != 2 || points2.Expiries[0].Amount != "700" || held["小樹點(信用卡)"] != "3190" || held["亞洲萬里通里數"] != "500" {
		t.Fatalf("after re-parse: expiries %+v, balances %+v", points2.Expiries, points2.Balances)
	}
}

func TestANewerBillThatPrintsNothingLapsingSaysNothingWill(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	store(t, pool, "october", cathayBill(10, program("小樹點(信用卡)", "3190", lapsing("88", 2099, 10, 31))))
	store(t, pool, "november", cathayBill(11, program("小樹點(信用卡)", "0")))
	if p := pointsOf(t, e); len(p.Expiries) != 0 {
		t.Fatalf("expiries = %+v", p.Expiries)
	}
}

func TestAnAccountWhoseSourcesSayItHoldsNothingIsWorthNothing(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	store(t, pool, "empty", cathayBill(10, program("小樹點(信用卡)", "0")))
	points := pointsOf(t, e)
	if len(points.Balances) != 0 || points.Worth == nil || *points.Worth != "0.00" {
		t.Fatalf("points account = %+v", points)
	}
}

func TestMoneySentToAnExchangeIsMovedNotSpentAndEachSideShowsWhereItWent(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	ctx := t.Context()
	storeDeposit(t, pool, 1, "0", depositLine{day: 21, amount: "-100000", after: "0", description: "行銀非約跨優", memo: "遠*銀行", opposite: "805 1234567890***123"})
	// 13:07 in Taipei on the 21st. Fetched twice: one line.
	deposited := exchanges.Movement{ID: "deposit/abc", At: time.Date(2026, 1, 21, 5, 7, 0, 0, time.UTC), Unit: "TWD", Amount: big.NewRat(100000, 1), Memo: "12345678901123"}
	for range 2 {
		snapshot := exchanges.Snapshot{Holdings: map[string]*big.Rat{"TWD": big.NewRat(100000, 1)}, Movements: []exchanges.Movement{deposited}}
		if err := exchanges.Store(ctx, pool, "max", time.Date(2026, 1, 22, 0, 0, 0, 0, time.UTC), snapshot); err != nil {
			t.Fatal(err)
		}
	}

	search := decode[TransactionSearch](t, get(t, e, "/api/transactions?from=2026-01-01&to=2026-01-31&limit=0"))
	if len(search.Flows) != 1 || mustRat(t, search.Flows[0].Spent).Sign() != 0 || mustRat(t, search.Flows[0].Moved).Cmp(big.NewRat(100000, 1)) != 0 {
		t.Fatalf("flows = %+v, want the 100,000 moved, not spent", search.Flows)
	}

	accounts := decode[[]Account](t, get(t, e, "/api/accounts"))
	bank := accountNamed(t, accounts, savings)
	var exchange Account
	for _, a := range accounts {
		if a.Kind == AccountKindExchange {
			exchange = a
		}
	}
	sent := decode[[]AccountLine](t, get(t, e, fmt.Sprintf("/api/accounts/%d/lines", bank.Id)))[0]
	if c := sent.Counterpart; c == nil || c.AccountKind != AccountKindExchange || c.Institution != "max" || c.StatementId != nil {
		t.Fatalf("the bank's line goes to %+v, want the exchange's deposit", sent.Counterpart)
	}
	received := decode[[]AccountLine](t, get(t, e, fmt.Sprintf("/api/accounts/%d/lines", exchange.Id)))
	if len(received) != 1 || received[0].Description != "入金" || received[0].Date.String() != "2026-01-21" || received[0].StatementId != nil ||
		received[0].Counterpart == nil || *received[0].Counterpart.AccountId != bank.Id {
		t.Fatalf("exchange lines = %+v", received)
	}
}

func mustRat(t *testing.T, s Decimal) *big.Rat {
	t.Helper()
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		t.Fatalf("%q is not a number", s)
	}
	return r
}

// withRule gives a bill's points program the rule its parser reports: lapsing at the end of the month
// `months` after the bill's closing month.
func withRule(response *parserv2.ParseDocumentResponse, unit string, months int32) *parserv2.ParseDocumentResponse {
	for _, a := range response.GetStatement().GetAccounts() {
		if a.GetKind() == parserv2.AccountKind_ACCOUNT_KIND_POINTS {
			a.LapseRules = append(a.LapseRules, &parserv2.LapseRule{Unit: unit, Rule: &parserv2.LapseRule_MonthsAfterClosing{MonthsAfterClosing: months}})
		}
	}
	return response
}

func TestPointsThatLapseByTheirProgramsRuleLeaveNoGapAndTheRestAreLots(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	const tree = "小樹點(信用卡)"
	earn := func(n string) pointsMove {
		return pointsLine("本期新增/扣除", parserv2.LineKind_LINE_KIND_EARN, n)
	}
	// Each bill's points lapse at the end of the second month after its closing month.
	store(t, pool, "january", withRule(cathayBill(1, withLines(program(tree, "110"), earn("10"))), tree, 2))
	store(t, pool, "february", withRule(cathayBill(2, withLines(program(tree, "80"), earn("20"),
		pointsLine("本期兌/轉換", parserv2.LineKind_LINE_KIND_REDEEM, "-30"))), tree, 2))
	store(t, pool, "april", withRule(cathayBill(4, withLines(program(tree, "25"), earn("5"))), tree, 2))
	store(t, pool, "may", withRule(cathayBill(5, program(tree, "3")), tree, 2))

	var points Account
	for _, a := range decode[[]Account](t, get(t, e, "/api/accounts")) {
		if a.Kind == AccountKindPoints {
			points = a
		}
	}
	// 20 and then 50 of the 100 held before January's bill; January's 10 by March; February's 20 by April.
	// Two more vanished by May that no rule explains: that much is still a gap.
	if len(points.Gaps) != 1 || points.Gaps[0].Unseen != "-2" || points.Gaps[0].From.String() != "2026-04-01" {
		t.Fatalf("gaps = %+v", points.Gaps)
	}
	var lapsed []string
	for _, l := range orNone(points.Lapsed) {
		lapsed = append(lapsed, fmt.Sprintf("%s..%s %s", l.From, l.To, l.Amount))
	}
	if want := []string{"2026-01-01..2026-02-01 20", "2026-02-01..2026-04-01 60", "2026-04-01..2026-05-01 20"}; !slices.Equal(lapsed, want) {
		t.Fatalf("lapsed = %v, want %v", lapsed, want)
	}
	// April's 3 left, lapsing by June's end.
	if lots := orNone(points.Lots); len(lots) != 1 || lots[0].By.String() != "2026-06-30" || lots[0].Amount != "3" || lots[0].Unit != tree {
		t.Fatalf("lots = %+v", lots)
	}
	detail := decode[AccountDetail](t, get(t, e, fmt.Sprintf("/api/accounts/%d", points.Id)))
	if detail.Lots == nil || detail.Lapsed == nil || len(detail.Gaps) != 1 {
		t.Fatalf("detail: lots %v, lapsed %v, gaps %v", detail.Lots, detail.Lapsed, detail.Gaps)
	}
}

func orNone[T any](items *[]T) []T {
	if items == nil {
		return nil
	}
	return *items
}
