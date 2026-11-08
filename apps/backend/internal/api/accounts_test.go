package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	gdate "google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/genproto/googleapis/type/decimal"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	parserv1 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v1"
	"github.com/hsin19/finchie/apps/backend/internal/ingest"
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
	account := &parserv1.DepositAccount{Number: savings, Name: "臺幣活存", Currency: "TWD", ClosingBalance: &decimal.Decimal{Value: closing}}
	for _, l := range lines {
		account.Lines = append(account.Lines, &parserv1.DepositLine{
			Date: &gdate.Date{Year: 2026, Month: month, Day: l.day}, Description: l.description, Memo: l.memo, Counterparty: l.opposite,
			Amount: &decimal.Decimal{Value: l.amount}, Balance: &decimal.Decimal{Value: l.after},
			Kind: parserv1.DepositLineKind_DEPOSIT_LINE_KIND_OTHER,
		})
	}
	last := map[int32]int32{4: 30, 6: 30, 9: 30, 11: 30}[month]
	if last == 0 {
		last = 31
	}
	statement := &parserv1.DepositStatement{
		Institution: "esun",
		PeriodStart: &gdate.Date{Year: 2026, Month: month, Day: 1},
		PeriodEnd:   &gdate.Date{Year: 2026, Month: month, Day: last},
		Accounts:    []*parserv1.DepositAccount{account},
	}
	if _, err := ingest.Store(t.Context(), pool, doc.ID, &parserv1.ParseDocumentResponse{Parser: "esun.deposit", DepositStatement: statement}); err != nil {
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
	line := func(month int32, amount string) *parserv1.DepositLine {
		return &parserv1.DepositLine{
			Date: &gdate.Date{Year: 2026, Month: month, Day: 1}, Description: "轉入",
			Amount: &decimal.Decimal{Value: amount}, Kind: parserv1.DepositLineKind_DEPOSIT_LINE_KIND_OTHER,
		}
	}
	// As CTBC's: a passbook savings account (活儲有摺) by its balance only, beside an account with lines
	// and one no line has moved yet.
	statement := func(month int32, passbook, current string, lines ...*parserv1.DepositLine) *parserv1.ParseDocumentResponse {
		return &parserv1.ParseDocumentResponse{Parser: "ctbc.deposit", DepositStatement: &parserv1.DepositStatement{
			Institution: "ctbc",
			PeriodStart: &gdate.Date{Year: 2026, Month: month - 1, Day: 13},
			PeriodEnd:   &gdate.Date{Year: 2026, Month: month, Day: 12},
			Accounts: []*parserv1.DepositAccount{
				{Number: "11111**2222*", Name: "活儲有摺", Currency: "TWD", ClosingBalance: &decimal.Decimal{Value: passbook}},
				{Number: "33333**4444*", Name: "台幣數二", Currency: "TWD", ClosingBalance: &decimal.Decimal{Value: current}, Lines: lines},
				{Number: "55555**6666*", Name: "外幣活期", Currency: "USD", ClosingBalance: &decimal.Decimal{Value: "12.34"}},
			},
		}}
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
	bill := &parserv1.Statement{
		Institution: "esun", ClosingDate: &gdate.Date{Year: 2026, Month: 10, Day: 7}, DueDate: &gdate.Date{Year: 2026, Month: 10, Day: 22},
		Currency: "TWD", TotalDue: &decimal.Decimal{Value: "300"},
		Transactions: []*parserv1.Transaction{{
			TransactionDate: &gdate.Date{Year: 2026, Month: 9, Day: 30}, Description: "SHOP",
			Kind: parserv1.TransactionKind_TRANSACTION_KIND_PURCHASE, Amount: &decimal.Decimal{Value: "300"},
		}},
		Reconciliation: &parserv1.Reconciliation{Balanced: true, Computed: &decimal.Decimal{Value: "300"}, Stated: &decimal.Decimal{Value: "300"}, StatedLabel: "x"},
	}
	doc, err := dbq.New(pool).UpsertDocument(t.Context(), dbq.UpsertDocumentParams{Sha256: []byte("bill"), Filename: "bill.eml", ContentType: "message/rfc822", Content: []byte("From: bank")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.Store(t.Context(), pool, doc.ID, &parserv1.ParseDocumentResponse{Parser: "esun.credit_card", Statement: bill}); err != nil {
		t.Fatal(err)
	}

	accounts := decode[[]Account](t, get(t, e, "/api/accounts"))
	deposit := accountNamed(t, accounts, savings)
	if deposit.Kind != Deposit || *deposit.Label != "臺幣活存" || deposit.Continuity != Checked {
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
	// A card bill holds no balance and is no account the owner has: it is not listed (ADR 0013).
	for _, a := range accounts {
		if a.Kind == CreditCard {
			t.Fatalf("card account listed: %+v", a)
		}
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
	rate := func(day, unit, perTWD string) {
		t.Helper()
		var n pgtype.Numeric
		if err := n.Scan(perTWD); err != nil {
			t.Fatal(err)
		}
		d, _ := time.Parse(time.DateOnly, day)
		if err := queries.UpsertRates(ctx, dbq.UpsertRatesParams{Day: pgtype.Date{Time: d, Valid: true}, Units: []string{unit}, PerTwd: []pgtype.Numeric{n}}); err != nil {
			t.Fatal(err)
		}
	}
	rate("2026-11-06", "USD", "0.025")
	rate("2026-11-07", "USD", "0.03125") // the newest: 1 USD is NT$32
	doc, err := queries.UpsertDocument(ctx, dbq.UpsertDocumentParams{Sha256: []byte("fx"), Filename: "fx.eml", ContentType: "message/rfc822", Content: []byte("From: bank")})
	if err != nil {
		t.Fatal(err)
	}
	balance := func(number, currency, closing string) *parserv1.DepositAccount {
		return &parserv1.DepositAccount{Number: number, Name: "外幣活存", Currency: currency, ClosingBalance: &decimal.Decimal{Value: closing}}
	}
	statement := &parserv1.DepositStatement{
		Institution: "esun",
		PeriodStart: &gdate.Date{Year: 2026, Month: 10, Day: 1},
		PeriodEnd:   &gdate.Date{Year: 2026, Month: 10, Day: 31},
		Accounts: []*parserv1.DepositAccount{
			balance(savings, "TWD", "1000"),
			balance("7777777***777", "USD", "10.50"),
			balance("8888888***888", "USD", "100"),
			balance("8888888***888", "JPY", "500"),
		},
	}
	if _, err := ingest.Store(ctx, pool, doc.ID, &parserv1.ParseDocumentResponse{Parser: "esun.deposit", DepositStatement: statement}); err != nil {
		t.Fatal(err)
	}

	accounts := decode[[]Account](t, get(t, e, "/api/accounts"))
	// NT$ is worth itself, with no rate to name.
	if twd := accountNamed(t, accounts, savings); *twd.Worth != "1000.00" || *twd.Balances[0].Worth != "1000" || twd.Balances[0].RateDay != nil {
		t.Fatalf("NT$ account = %+v", twd)
	}
	dollars := accountNamed(t, accounts, "7777777***777")
	if *dollars.Balances[0].Worth != "336.00" || dollars.Balances[0].RateDay.String() != "2026-11-07" || *dollars.Worth != "336.00" {
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
func store(t *testing.T, pool *pgxpool.Pool, name string, response *parserv1.ParseDocumentResponse) int64 {
	t.Helper()
	doc, err := dbq.New(pool).UpsertDocument(t.Context(), dbq.UpsertDocumentParams{
		Sha256: []byte(name), Filename: name, ContentType: "message/rfc822", Content: []byte("From: bank"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.Store(t.Context(), pool, doc.ID, response); err != nil {
		t.Fatal(err)
	}
	return doc.ID
}

func septemberLine(day int32, amount, description, counterparty string) *parserv1.DepositLine {
	return &parserv1.DepositLine{
		Date: &gdate.Date{Year: 2026, Month: 9, Day: day}, Description: description, Counterparty: counterparty,
		Amount: &decimal.Decimal{Value: amount}, Kind: parserv1.DepositLineKind_DEPOSIT_LINE_KIND_OTHER,
	}
}

// septemberDeposit is one account's statement for September 2026.
func septemberDeposit(institution, number, closing string, lines ...*parserv1.DepositLine) *parserv1.ParseDocumentResponse {
	return &parserv1.ParseDocumentResponse{Parser: institution + ".deposit", DepositStatement: &parserv1.DepositStatement{
		Institution: institution,
		PeriodStart: &gdate.Date{Year: 2026, Month: 9, Day: 1},
		PeriodEnd:   &gdate.Date{Year: 2026, Month: 9, Day: 30},
		Accounts: []*parserv1.DepositAccount{{
			Number: number, Name: "活存", Currency: "TWD", ClosingBalance: &decimal.Decimal{Value: closing}, Lines: lines,
		}},
	}}
}

// septemberBill is E.SUN's card bill closing 9/7, due 9/22.
func septemberBill(totalDue string, lines ...*parserv1.Transaction) *parserv1.ParseDocumentResponse {
	return &parserv1.ParseDocumentResponse{Parser: "esun.credit_card", Statement: &parserv1.Statement{
		Institution: "esun", ClosingDate: &gdate.Date{Year: 2026, Month: 9, Day: 7}, DueDate: &gdate.Date{Year: 2026, Month: 9, Day: 22},
		Currency: "TWD", TotalDue: &decimal.Decimal{Value: totalDue}, Transactions: lines,
		Reconciliation: &parserv1.Reconciliation{Balanced: true, Computed: &decimal.Decimal{Value: "0"}, Stated: &decimal.Decimal{Value: "0"}, StatedLabel: "x"},
	}}
}

func cardLine(day int32, kind parserv1.TransactionKind, amount, description, card string) *parserv1.Transaction {
	return &parserv1.Transaction{
		TransactionDate: &gdate.Date{Year: 2026, Month: 9, Day: day}, Description: description, Kind: kind,
		Amount: &decimal.Decimal{Value: amount}, CardLast4: card,
	}
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

func TestLinesShowWhereTheirMoneyWentAndBillsWhenTheyWerePaid(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	// The bill closing 9/7 is paid on 9/22 from E.SUN; 5,000 goes to Taishin, whose number the line prints.
	store(t, pool, "esun.eml", septemberDeposit("esun", savings, "5000",
		septemberLine(22, "-6350", "玉山卡款扣繳", ""),
		septemberLine(25, "-5000", "行銀跨行轉優", "812 0087654321***234"),
	))
	store(t, pool, "taishin.eml", septemberDeposit("taishin", "876543****9234", "5000", septemberLine(25, "5000", "CD轉入", "")))
	store(t, pool, "bill.eml", septemberBill("6350",
		cardLine(22, parserv1.TransactionKind_TRANSACTION_KIND_PAYMENT, "-6350", "感謝您辦理本行自動轉帳繳款", "")))

	accounts := decode[[]Account](t, get(t, e, "/api/accounts"))
	esun, taishin := accountNamed(t, accounts, savings), accountNamed(t, accounts, "876543****9234")
	lines := decode[[]AccountLine](t, get(t, e, fmt.Sprintf("/api/accounts/%d/lines", esun.Id)))
	transfer, card := lines[0], lines[1]
	if transfer.Counterpart == nil || transfer.Counterpart.AccountId == nil || *transfer.Counterpart.AccountId != taishin.Id ||
		*transfer.Counterpart.AccountNumber != "876543****9234" {
		t.Fatalf("transfer counterpart = %+v", transfer.Counterpart)
	}
	if card.Counterpart == nil || card.Counterpart.AccountKind != CreditCard || card.Counterpart.AccountId != nil || card.Counterpart.Institution != "esun" {
		t.Fatalf("card payment counterpart = %+v", card.Counterpart)
	}
	statements := decode[[]Statement](t, get(t, e, "/api/statements"))
	if len(statements) != 1 || statements[0].PaidOn == nil || statements[0].PaidOn.String() != "2026-09-22" {
		t.Fatalf("bill = %+v", statements)
	}
}

func TestASecuritiesAccountIsWorthItsHoldingsAtTheNewestCloseAndItsTradesAreNoSpending(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	dec := func(s string) *decimal.Decimal { return &decimal.Decimal{Value: s} }
	trade := func(day int32, name string, side parserv1.TradeSide, shares, amount, net string) *parserv1.SecuritiesTrade {
		return &parserv1.SecuritiesTrade{
			Date: &gdate.Date{Year: 2026, Month: 7, Day: day}, Name: name, Side: side, Shares: dec(shares), Price: dec("50"),
			Category: "集賣", Amount: dec(amount), Fee: dec("143"), Tax: dec("303"), Net: dec(net),
		}
	}
	// Synthetic (ADR 0006): 範例航運 sold out, so not held.
	store(t, pool, "skis.eml", &parserv1.ParseDocumentResponse{Parser: "skis.securities", SecuritiesStatement: &parserv1.SecuritiesStatement{
		Institution: "skis", AccountNumber: "12**345",
		PeriodStart: &gdate.Date{Year: 2026, Month: 7, Day: 1}, PeriodEnd: &gdate.Date{Year: 2026, Month: 7, Day: 31},
		HoldingsDate: &gdate.Date{Year: 2026, Month: 8, Day: 4},
		Holdings:     []*parserv1.SecuritiesHolding{{Name: "範例電子", Shares: dec("3500")}},
		Trades: []*parserv1.SecuritiesTrade{
			trade(3, "範例電子", parserv1.TradeSide_TRADE_SIDE_SELL, "2000", "101000", "100554"),
			trade(15, "範例航運", parserv1.TradeSide_TRADE_SIDE_SELL, "1000", "30000", "29554"),
		},
		Reconciliation: &parserv1.Reconciliation{Balanced: true, Computed: dec("0"), Stated: dec("0"), StatedLabel: "應收付金額"},
	}})
	day := func(d int) pgtype.Date {
		return pgtype.Date{Time: time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC), Valid: true}
	}
	price := func(s string) pgtype.Numeric {
		var n pgtype.Numeric
		_ = n.Scan(s)
		return n
	}
	if err := dbq.New(pool).UpsertQuotes(t.Context(), dbq.UpsertQuotesParams{
		Days: []pgtype.Date{day(5), day(6)}, Names: []string{"範例電子", "範例電子"}, Codes: []string{"1234", "1234"},
		Markets: []string{"twse", "twse"}, Closes: []pgtype.Numeric{price("49.00"), price("50.50")},
	}); err != nil {
		t.Fatal(err)
	}

	account := accountNamed(t, decode[[]Account](t, get(t, e, "/api/accounts")), "12**345")
	if account.Kind != Securities || account.Continuity != Checked || len(account.Balances) != 1 {
		t.Fatalf("account = %+v", account)
	}
	held := account.Balances[0]
	if held.Unit != "範例電子" || *held.Worth != "176750.00" || *held.Code != "1234" || *held.Price != "50.50" || held.RateDay.String() != "2026-11-06" {
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
