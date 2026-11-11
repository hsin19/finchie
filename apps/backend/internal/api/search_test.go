package api

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	gdate "google.golang.org/genproto/googleapis/type/date"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
	pt "github.com/hsin19/finchie/apps/backend/internal/parsertest"
)

type line struct {
	date, description, kind, amount string
	currency, country, card         string
	// In currency, when it is set.
	foreign string
}

// seedStatement stores a card bill with its lines through ingest, as the worker would, and returns the
// statement's ID. A line's currency comes with an amount in it (ADR 0021).
func seedStatement(t *testing.T, pool *pgxpool.Pool, institution, closing string, lines []line) int64 {
	t.Helper()
	day := func(s string) *gdate.Date {
		d := pgDate(t, s).Time
		return &gdate.Date{Year: int32(d.Year()), Month: int32(d.Month()), Day: int32(d.Day())} //nolint:gosec // a date
	}
	kinds := map[string]parserv2.LineKind{
		"purchase": parserv2.LineKind_LINE_KIND_PURCHASE, "foreign_transaction_fee": parserv2.LineKind_LINE_KIND_FOREIGN_TRANSACTION_FEE,
		"refund": parserv2.LineKind_LINE_KIND_REFUND, "payment": parserv2.LineKind_LINE_KIND_PAYMENT, "rebate": parserv2.LineKind_LINE_KIND_REBATE,
	}
	printed := make([]*parserv2.Line, 0, len(lines))
	for _, l := range lines {
		options := []pt.CardOption{pt.Country(l.country), pt.OnCard(l.card)}
		if l.currency != "" {
			options = append(options, pt.Foreign(l.currency, l.foreign))
		}
		printed = append(printed, pt.CardLine(day(l.date), l.description, kinds[l.kind], l.amount, options...))
	}
	documentID := store(t, pool, institution+closing, pt.Response(institution+".credit_card", pt.Bill(institution, day(closing), day(closing), "0", printed...)))
	var id int64
	if err := pool.QueryRow(t.Context(), "SELECT id FROM statements WHERE document_id = $1", documentID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func pgDate(t *testing.T, s string) pgtype.Date {
	t.Helper()
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		t.Fatal(err)
	}
	return pgtype.Date{Time: d, Valid: true}
}

func seedLedger(t *testing.T, pool *pgxpool.Pool) (hsbc int64) {
	t.Helper()
	hsbc = seedStatement(t, pool, "hsbc", "2026-10-20", []line{
		{"2026-10-08", "Leather Atelier Firenze", "purchase", "23104", "EUR", "IT", "4417", "650.00"},
		{"2026-10-08", "Foreign transaction fee", "foreign_transaction_fee", "346.56", "", "", "4417", ""},
		{"2026-10-07", "Tax Free Refund", "refund", "-2180", "", "SE", "4417", ""},
		{"2026-09-30", "Payment, thank you", "payment", "-41280", "", "", "", ""},
	})
	seedStatement(t, pool, "esun", "2026-10-07", []line{
		{"2026-10-02", "Uber Eats", "purchase", "289", "", "TW", "3381", ""},
		{"2026-10-07", "U Bear cashback", "rebate", "-35.5", "", "", "3381", ""},
	})
	// The fee (1.5% of 23104) became the leather purchase's child as it was stored; the tax-free refund
	// fits none.
	return hsbc
}

func TestSearchTransactionsFiltersAndTotals(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	seedLedger(t, pool)

	// The fee is folded into the leather purchase: one row fewer, the same sum.
	all := decode[TransactionSearch](t, get(t, e, "/api/transactions?from=2026-10-01&to=2026-10-31"))
	if all.Total != 4 || len(all.Items) != 4 || all.Sum != "21524.06" {
		t.Fatalf("October: total=%d items=%d sum=%s", all.Total, len(all.Items), all.Sum)
	}
	leather := all.Items[0]
	if leather.Description != "Leather Atelier Firenze" || leather.Institution != "hsbc" || leather.Amount != "23104" {
		t.Fatalf("newest first, then statement line order: %+v", leather)
	}
	if leather.Children == nil || len(*leather.Children) != 1 || (*leather.Children)[0].Kind != TransactionKindForeignTransactionFee || (*leather.Children)[0].Amount != "346.56" {
		t.Fatalf("leather children = %+v", leather.Children)
	}

	// Asked for fees alone, a fee is a row again, pointing at its purchase.
	fees := decode[TransactionSearch](t, get(t, e, "/api/transactions?kind=foreign_transaction_fee"))
	if fees.Total != 1 || fees.Items[0].Parent == nil || fees.Items[0].Parent.Id != leather.Id || fees.Items[0].Parent.StatementId != leather.StatementId {
		t.Fatalf("fees = %+v", fees.Items)
	}
	fee := fees.Items[0]

	purchases := decode[TransactionSearch](t, get(t, e, "/api/transactions?kind=purchase&kind=refund&card=4417"))
	if purchases.Total != 2 || purchases.Sum != "21270.56" {
		t.Fatalf("purchase (with its fee) + refund on 4417: total=%d sum=%s", purchases.Total, purchases.Sum)
	}
	// byKind ignores the kind filter but keeps the card filter, and counts lines as printed.
	kinds := map[TransactionKind]string{}
	for _, k := range purchases.ByKind {
		kinds[k.Kind] = k.Amount
	}
	if len(kinds) != 3 || kinds[TransactionKindForeignTransactionFee] != "346.56" || kinds[TransactionKindRebate] != "" {
		t.Fatalf("byKind = %+v", purchases.ByKind)
	}
	// byCard ignores the card filter but keeps the kind filter.
	if len(purchases.ByCard) != 2 || purchases.ByCard[0].Institution != "esun" || purchases.ByCard[0].Amount != "289" {
		t.Fatalf("byCard = %+v", purchases.ByCard)
	}

	overseas := decode[TransactionSearch](t, get(t, e, "/api/transactions?overseas=true"))
	if overseas.Total != 2 {
		t.Fatalf("overseas total = %d, want the EUR purchase and the SE refund", overseas.Total)
	}

	search := decode[TransactionSearch](t, get(t, e, "/api/transactions?q=uber%20&limit=0"))
	if search.Total != 1 || len(search.Items) != 0 || search.Sum != "289" {
		t.Fatalf("q=uber limit=0: total=%d items=%d sum=%s", search.Total, len(search.Items), search.Sum)
	}

	page := decode[TransactionSearch](t, get(t, e, "/api/transactions?limit=2&offset=4"))
	if page.Total != 5 || len(page.Items) != 1 {
		t.Fatalf("last page: total=%d items=%d", page.Total, len(page.Items))
	}

	// By statement instead of by transaction date: HSBC's statement closing 10-20 also carries a
	// payment dated 09-30, which the October transaction-date filter above leaves out.
	closing := decode[TransactionSearch](t, get(t, e, "/api/transactions?closingFrom=2026-10-11&closingTo=2026-10-31&limit=0"))
	if closing.Total != 3 || closing.Sum != "-20009.44" || len(closing.ByCard) != 2 || closing.ByCard[0].Institution != "hsbc" {
		t.Fatalf("closing 10-11..10-31: total=%d sum=%s byCard=%+v", closing.Total, closing.Sum, closing.ByCard)
	}
	closingPurchases := decode[TransactionSearch](t, get(t, e, "/api/transactions?closingFrom=2026-10-01&closingTo=2026-10-10&kind=purchase"))
	if closingPurchases.Total != 1 || closingPurchases.Items[0].Institution != "esun" {
		t.Fatalf("purchases closing 10-01..10-10: %+v", closingPurchases)
	}

	// Amounts sort by size: the -41280 payment is the largest, the -35.5 rebate the smallest.
	for query, want := range map[string]string{
		"sort=largest":  "Payment, thank you",
		"sort=smallest": "U Bear cashback",
		"sort=oldest":   "Payment, thank you",
		"sort=newest":   "Leather Atelier Firenze",
		"sort=bogus":    "Leather Atelier Firenze",
	} {
		sorted := decode[TransactionSearch](t, get(t, e, "/api/transactions?limit=1&"+query))
		if len(sorted.Items) != 1 || sorted.Items[0].Description != want {
			t.Fatalf("%s: first = %+v, want %q", query, sorted.Items, want)
		}
	}
	largest := decode[TransactionSearch](t, get(t, e, "/api/transactions?sort=largest&kind=purchase"))
	if len(largest.Items) != 2 || largest.Items[0].Amount != "23104" || largest.Items[1].Amount != "289" {
		t.Fatalf("largest purchases = %+v", largest.Items)
	}

	none := decode[TransactionSearch](t, get(t, e, "/api/transactions?from=2030-01-01"))
	if none.Total != 0 || none.Sum != "0" || len(none.ByKind) != 0 {
		t.Fatalf("empty match = %+v", none)
	}

	// A line's family, from either end.
	fromFee := decode[TransactionFamily](t, get(t, e, "/api/transactions/"+itoa(fee.Id)))
	if fromFee.Transaction.Id != fee.Id || fromFee.Parent == nil || fromFee.Parent.Id != leather.Id || len(fromFee.Children) != 0 {
		t.Fatalf("family of the fee = %+v", fromFee)
	}
	fromPurchase := decode[TransactionFamily](t, get(t, e, "/api/transactions/"+itoa(leather.Id)))
	if fromPurchase.Parent != nil || len(fromPurchase.Children) != 1 || fromPurchase.Children[0].Id != fee.Id {
		t.Fatalf("family of the purchase = %+v", fromPurchase)
	}
	if got := get(t, e, "/api/transactions/999999").Code; got != http.StatusNotFound {
		t.Errorf("unknown transaction: status = %d", got)
	}
}

func TestStatementAndDocumentContent(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	hsbc := seedLedger(t, pool)

	statement := decode[Statement](t, get(t, e, "/api/statements/"+itoa(hsbc)))
	if statement.Institution != "hsbc" || statement.TransactionCount != 4 {
		t.Fatalf("statement = %+v", statement)
	}
	lines := decode[[]Transaction](t, get(t, e, "/api/statements/"+itoa(hsbc)+"/transactions"))
	if len(lines) != 4 || lines[0].StatementId != hsbc || lines[0].Institution != "hsbc" {
		t.Fatalf("lines = %+v", lines)
	}

	// A document stored as a bare PDF before ADR 0014 opens in place.
	legacy, err := dbq.New(pool).UpsertDocument(t.Context(), dbq.UpsertDocumentParams{
		Sha256: []byte("legacy"), Filename: "hsbc.pdf", ContentType: "application/pdf", Content: []byte("%PDF"),
	})
	if err != nil {
		t.Fatal(err)
	}
	content := get(t, e, "/api/documents/"+itoa(legacy.ID)+"/content")
	if content.Code != http.StatusOK || content.Body.String() != "%PDF" ||
		content.Header().Get("Content-Type") != "application/pdf" ||
		content.Header().Get("Content-Disposition") != `inline; filename=hsbc.pdf` {
		t.Fatalf("content: %d %v %q", content.Code, content.Header(), content.Body)
	}
	for _, path := range []string{"/api/statements/999", "/api/documents/999/content"} {
		if got := get(t, e, path).Code; got != http.StatusNotFound {
			t.Errorf("%s: status = %d", path, got)
		}
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func TestSearchListsEveryAccountsLinesAsTheOwnerSeesThem(t *testing.T) {
	e, pool := newAPI(t, 1<<20)
	fee := septemberLine(26, "-15", "跨行手續費", "")
	fee.Kind = parserv2.LineKind_LINE_KIND_FEE
	transfer := septemberLine(25, "-5000", "行銀跨行轉優", "812 0087654321***234")
	transfer.GetDeposit().Memo = "房租"
	deposit := store(t, pool, "esun.eml", septemberDeposit("esun", savings, "47165",
		septemberLine(5, "60000", "薪資", ""),
		septemberLine(22, "-6350", "玉山卡款扣繳", ""),
		transfer,
		fee,
	))
	store(t, pool, "taishin.eml", septemberDeposit("taishin", "876543****9234", "5000", septemberLine(25, "5000", "CD轉入", "")))
	store(t, pool, "bill.eml", septemberBill("6350",
		cardLine(1, parserv2.LineKind_LINE_KIND_PURCHASE, "6350", "SHOP", "3381"),
		cardLine(22, parserv2.LineKind_LINE_KIND_PAYMENT, "-6350", "感謝您辦理本行自動轉帳繳款", "")))
	accounts := decode[[]Account](t, get(t, e, "/api/accounts"))
	esun, taishin := accountNamed(t, accounts, savings), accountNamed(t, accounts, "876543****9234")

	// Every account's lines, each link once: Taishin's inflow and the bill's payment line are listed
	// under the E.SUN outflows they came from.
	all := decode[TransactionSearch](t, get(t, e, "/api/transactions"))
	if all.Total != 5 || len(all.Items) != 5 {
		t.Fatalf("total = %d, items = %d", all.Total, len(all.Items))
	}
	// What went out and came in: the purchase and the fee, and the salary. The card payment and the
	// transfer only moved money between the owner's own lines.
	if len(all.Flows) != 1 || all.Flows[0] != (UnitFlow{Unit: "TWD", Spent: "-6365", Received: "60000", Moved: "11350", MovedCount: 2}) {
		t.Fatalf("flows = %+v", all.Flows)
	}
	items := map[string]Transaction{}
	for _, item := range all.Items {
		items[item.Description] = item
	}
	if shop := items["SHOP"]; shop.Amount != "6350" || shop.Flow != "-6350" || shop.AccountKind != AccountKindCreditCard || shop.AccountId == nil {
		t.Fatalf("card purchase = %+v", shop)
	}
	if salary := items["薪資"]; salary.Flow != "60000" || salary.AccountKind != AccountKindDeposit || *salary.AccountId != esun.Id || *salary.AccountNumber != savings {
		t.Fatalf("salary = %+v", salary)
	}
	if out := items["行銀跨行轉優"]; out.Counterpart == nil || *out.Counterpart.AccountId != taishin.Id || *out.Memo != "房租" {
		t.Fatalf("transfer = %+v", out)
	}
	if paid := items["玉山卡款扣繳"]; paid.Counterpart == nil || paid.Counterpart.AccountKind != AccountKindCreditCard {
		t.Fatalf("card payment = %+v", paid)
	}
	// Without the outflow, the line it went to is a row of its own.
	inflow := decode[TransactionSearch](t, get(t, e, fmt.Sprintf("/api/transactions?account=%d", taishin.Id)))
	if inflow.Total != 1 || inflow.Items[0].Description != "CD轉入" || *inflow.Items[0].Counterpart.AccountId != esun.Id || inflow.Flows[0].MovedCount != 1 {
		t.Fatalf("Taishin alone = %+v", inflow)
	}
	payments := decode[TransactionSearch](t, get(t, e, "/api/transactions?kind=payment"))
	if payments.Total != 1 || *payments.Items[0].Counterpart.AccountId != esun.Id {
		t.Fatalf("payments = %+v", payments.Items)
	}
	// The kind facet counts lines as printed.
	if counts := countByKind(all.ByKind); counts[TransactionKindOther] != 4 || counts[TransactionKindPayment] != 1 {
		t.Fatalf("byKind = %+v", all.ByKind)
	}
	kinds := map[TransactionKind]string{}
	for _, k := range all.ByKind {
		kinds[k.Kind] = k.Flow
	}
	if kinds[TransactionKindFee] != "-15" || kinds[TransactionKindPurchase] != "-6350" {
		t.Fatalf("byKind = %+v", all.ByKind)
	}

	if hidden := decode[TransactionSearch](t, get(t, e, "/api/transactions?hideTransfers=true")); hidden.Total != 3 || hidden.Flows[0].Spent != "-6365" || hidden.Flows[0].MovedCount != 0 {
		t.Fatalf("without transfers: total %d, flows %+v", hidden.Total, hidden.Flows)
	}
	// The account picker's facets skip the picker itself.
	one := decode[TransactionSearch](t, get(t, e, fmt.Sprintf("/api/transactions?account=%d", esun.Id)))
	if one.Total != 4 || len(one.ByAccount) != 2 || one.ByAccount[0].AccountId != esun.Id || one.ByAccount[0].Count != 4 || one.ByAccount[1].Count != 1 || len(one.ByCard) == 0 {
		t.Fatalf("one account: total %d, byAccount %+v, byCard %+v", one.Total, one.ByAccount, one.ByCard)
	}
	if memo := decode[TransactionSearch](t, get(t, e, "/api/transactions?q=房租")); memo.Total != 1 {
		t.Fatalf("q matches the memo: total %d", memo.Total)
	}
	// The bill pages ask for card lines alone, whose amounts add up as printed.
	cards := decode[TransactionSearch](t, get(t, e, "/api/transactions?accountKind=credit_card"))
	if cards.Total != 2 || cards.Sum != "0" || len(cards.ByAccount) != 0 {
		t.Fatalf("card lines: total %d, sum %s, byAccount %+v", cards.Total, cards.Sum, cards.ByAccount)
	}

	// The statement endpoints still list card bills alone.
	if statements := decode[[]Statement](t, get(t, e, "/api/statements")); len(statements) != 1 || statements[0].Institution != "esun" {
		t.Fatalf("statements = %+v", statements)
	}
	// The document says what it produced, so the upload page needs no card bill to count its lines.
	stored := decode[Document](t, get(t, e, fmt.Sprintf("/api/documents/%d", deposit)))
	if stored.StatementKind == nil || *stored.StatementKind != AccountKindDeposit || stored.LineCount != 4 || stored.AccountCount != 1 {
		t.Fatalf("document = kind %v, %d lines, %d accounts", stored.StatementKind, stored.LineCount, stored.AccountCount)
	}
}

func countByKind(totals []KindTotal) map[TransactionKind]int {
	counts := map[TransactionKind]int{}
	for _, k := range totals {
		counts[k.Kind] = k.Count
	}
	return counts
}
