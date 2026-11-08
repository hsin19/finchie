package api

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	parserv1 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v1"
	"github.com/hsin19/finchie/apps/backend/internal/ingest"
)

type line struct {
	date, description, kind, amount string
	currency, country, card         string
}

// seedStatement stores a statement with its lines the way the ingest worker would.
func seedStatement(t *testing.T, pool *pgxpool.Pool, institution, closing string, lines []line) int64 {
	t.Helper()
	ctx := t.Context()
	queries := dbq.New(pool)
	doc, err := queries.UpsertDocument(ctx, dbq.UpsertDocumentParams{
		Sha256: []byte(institution + closing), Filename: institution + ".pdf", ContentType: "application/pdf", Content: []byte("%PDF"),
	})
	if err != nil {
		t.Fatal(err)
	}
	id, err := queries.InsertStatement(ctx, dbq.InsertStatementParams{
		DocumentID: doc.ID, Institution: institution, Kind: ingest.KindCreditCard, ClosingDate: pgDate(t, closing), DueDate: pgDate(t, closing),
		Currency: optional("TWD"), TotalDue: numeric(t, "0"), ComputedNewCharges: numeric(t, "0"), StatedNewCharges: numeric(t, "0"),
		StatedLabel: optional("total"), Balanced: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := queries.UpsertAccount(ctx, dbq.UpsertAccountParams{Institution: institution, Kind: ingest.KindCreditCard})
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]dbq.InsertTransactionsParams, 0, len(lines))
	for i, l := range lines {
		rows = append(rows, dbq.InsertTransactionsParams{
			StatementID: id, AccountID: account, Unit: "TWD", LineNo: int32(i + 1), TransactionDate: pgDate(t, l.date), Description: l.description,
			Kind: l.kind, Amount: numeric(t, l.amount),
			OriginalCurrency: optional(l.currency), Country: optional(l.country), CardLast4: optional(l.card),
		})
	}
	if _, err := queries.InsertTransactions(ctx, rows); err != nil {
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

func numeric(t *testing.T, s string) pgtype.Numeric {
	t.Helper()
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		t.Fatal(err)
	}
	return n
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func seedLedger(t *testing.T, pool *pgxpool.Pool) (hsbc int64) {
	t.Helper()
	hsbc = seedStatement(t, pool, "hsbc", "2026-10-20", []line{
		{"2026-10-08", "Leather Atelier Firenze", "purchase", "23104", "EUR", "IT", "4417"},
		{"2026-10-08", "Foreign transaction fee", "foreign_transaction_fee", "346.56", "", "", "4417"},
		{"2026-10-07", "Tax Free Refund", "refund", "-2180", "", "SE", "4417"},
		{"2026-09-30", "Payment, thank you", "payment", "-41280", "", "", ""},
	})
	seedStatement(t, pool, "esun", "2026-10-07", []line{
		{"2026-10-02", "Uber Eats", "purchase", "289", "", "TW", "3381"},
		{"2026-10-07", "U Bear cashback", "rebate", "-35.5", "", "", "3381"},
	})
	// The fee (1.5% of 23104) becomes the leather purchase's child; the tax-free refund fits none.
	if _, err := ingest.Relink(t.Context(), pool); err != nil {
		t.Fatal(err)
	}
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
	if leather.Children == nil || len(*leather.Children) != 1 || (*leather.Children)[0].Kind != ForeignTransactionFee || (*leather.Children)[0].Amount != "346.56" {
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
	if len(kinds) != 3 || kinds[ForeignTransactionFee] != "346.56" || kinds[Rebate] != "" {
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

	content := get(t, e, "/api/documents/"+itoa(statement.DocumentId)+"/content")
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
	fee.Kind = parserv1.DepositLineKind_DEPOSIT_LINE_KIND_FEE
	transfer := septemberLine(25, "-5000", "行銀跨行轉優", "812 0087654321***234")
	transfer.Memo = "房租"
	deposit := store(t, pool, "esun.eml", septemberDeposit("esun", savings, "47165",
		septemberLine(5, "60000", "薪資", ""),
		septemberLine(22, "-6350", "玉山卡款扣繳", ""),
		transfer,
		fee,
	))
	store(t, pool, "taishin.eml", septemberDeposit("taishin", "876543****9234", "5000", septemberLine(25, "5000", "CD轉入", "")))
	store(t, pool, "bill.eml", septemberBill("6350",
		cardLine(1, parserv1.TransactionKind_TRANSACTION_KIND_PURCHASE, "6350", "SHOP", "3381"),
		cardLine(22, parserv1.TransactionKind_TRANSACTION_KIND_PAYMENT, "-6350", "感謝您辦理本行自動轉帳繳款", "")))
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
	if shop := items["SHOP"]; shop.Amount != "6350" || *shop.Flow != "-6350" || *shop.AccountKind != CreditCard || shop.AccountId != nil {
		t.Fatalf("card purchase = %+v", shop)
	}
	if salary := items["薪資"]; *salary.Flow != "60000" || *salary.AccountKind != Deposit || *salary.AccountId != esun.Id || *salary.AccountNumber != savings {
		t.Fatalf("salary = %+v", salary)
	}
	if out := items["行銀跨行轉優"]; out.Counterpart == nil || *out.Counterpart.AccountId != taishin.Id || *out.Memo != "房租" {
		t.Fatalf("transfer = %+v", out)
	}
	if paid := items["玉山卡款扣繳"]; paid.Counterpart == nil || paid.Counterpart.AccountKind != CreditCard {
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
	if counts := countByKind(all.ByKind); counts[Other] != 4 || counts[Payment] != 1 {
		t.Fatalf("byKind = %+v", all.ByKind)
	}
	kinds := map[TransactionKind]string{}
	for _, k := range all.ByKind {
		kinds[k.Kind] = k.Flow
	}
	if kinds[Fee] != "-15" || kinds[Purchase] != "-6350" {
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
	if stored.StatementKind == nil || *stored.StatementKind != Deposit || stored.LineCount != 4 || stored.AccountCount != 1 {
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
