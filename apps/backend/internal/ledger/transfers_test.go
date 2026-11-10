package ingest

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
)

// Synthetic accounts and amounts (ADR 0006).

func day(t *testing.T, s string) pgtype.Date {
	t.Helper()
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		t.Fatal(err)
	}
	return pgtype.Date{Time: d, Valid: true}
}

func money(t *testing.T, s string) pgtype.Numeric {
	t.Helper()
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		t.Fatal(err)
	}
	return n
}

type deposit struct {
	account             int64
	institution, number string
	full                string
	statement           int64
}

var (
	esunSavings    = deposit{account: 1, institution: "esun", number: "0345678***012", statement: 10}
	taishinRichart = deposit{account: 2, institution: "taishin", number: "876543****9234", statement: 20}
	fubonSavings   = deposit{account: 3, institution: "fubon", number: "73520004****66", statement: 30}
)

func depositLine(t *testing.T, id int64, at deposit, on, amount, counterparty string) transferLine {
	t.Helper()
	l := transferLine{
		ID: id, AccountID: at.account, AccountKind: KindDeposit, Institution: at.institution, Number: at.number,
		StatementID: at.statement, TransactionDate: day(t, on), Kind: "other", Amount: money(t, amount), Unit: "TWD",
	}
	if at.full != "" {
		l.FullNumber = &at.full
	}
	if counterparty != "" {
		l.Counterparty = &counterparty
	}
	return l
}

func paymentLine(t *testing.T, id int64, institution string, statement int64, on, amount string) transferLine {
	t.Helper()
	return transferLine{
		ID: id, AccountID: 100, AccountKind: KindCreditCard, Institution: institution, StatementID: statement,
		TransactionDate: day(t, on), Kind: "payment", Amount: money(t, amount), Unit: "TWD",
	}
}

func inUnit(l transferLine, unit, memo string) transferLine {
	l.Unit = unit
	if memo != "" {
		l.Memo = &memo
	}
	return l
}

func TestACardPaymentLinksToTheDepositOutflowOfTheSameAmountWithinThreeDays(t *testing.T) {
	pairs := matchTransfers([]transferLine{
		depositLine(t, 1, esunSavings, "2026-09-22", "-6350", ""),
		paymentLine(t, 2, "esun", 50, "2026-09-22", "-6350"),
		// From another bank's account: a card is paid from whichever.
		depositLine(t, 3, esunSavings, "2026-09-23", "-58830", "081 0000007351***004"),
		paymentLine(t, 4, "hsbc", 60, "2026-09-25", "-58830"),
		// Too far apart, and a different amount.
		depositLine(t, 5, esunSavings, "2026-09-01", "-500", ""),
		paymentLine(t, 6, "taishin", 70, "2026-09-10", "-500"),
		paymentLine(t, 7, "taishin", 70, "2026-09-02", "-501"),
	})
	want := map[int64]int64{1: 2, 2: 1, 3: 4, 4: 3}
	if len(pairs) != len(want) {
		t.Fatalf("pairs = %v, want %v", pairs, want)
	}
	for a, b := range want {
		if pairs[a] != b {
			t.Fatalf("pairs = %v, want %v", pairs, want)
		}
	}
}

func TestAnOwnTransferLinksOnlyWhenACounterpartyFitsTheOtherAccount(t *testing.T) {
	cases := map[string]struct {
		lines []transferLine
		want  bool
	}{
		"the outflow names the account, masked as banks print it": {[]transferLine{
			depositLine(t, 1, esunSavings, "2026-07-21", "-30000", "812 0087654321***234"),
			depositLine(t, 2, taishinRichart, "2026-07-21", "30000", ""),
		}, true},
		"the inflow names where it came from": {[]transferLine{
			depositLine(t, 1, esunSavings, "2026-07-21", "-30000", ""),
			depositLine(t, 2, taishinRichart, "2026-07-22", "30000", "808 0000345678***012"),
		}, true},
		"neither names the other: equal amounts prove nothing": {[]transferLine{
			depositLine(t, 1, esunSavings, "2026-07-21", "-30000", ""),
			depositLine(t, 2, taishinRichart, "2026-07-21", "30000", ""),
		}, false},
		"another bank's code": {[]transferLine{
			depositLine(t, 1, esunSavings, "2026-07-21", "-30000", "807 0087654321***234"),
			depositLine(t, 2, taishinRichart, "2026-07-21", "30000", ""),
		}, false},
		"a digit both print differs": {[]transferLine{
			depositLine(t, 1, esunSavings, "2026-07-21", "-30000", "812 0087654321***235"),
			depositLine(t, 2, taishinRichart, "2026-07-21", "30000", ""),
		}, false},
		"a lone number is at the line's own bank": {[]transferLine{
			depositLine(t, 1, fubonSavings, "2026-07-21", "-30000", "73520004123466"),
			depositLine(t, 2, deposit{account: 4, institution: "fubon", number: "73520004****66", statement: 40}, "2026-07-21", "30000", ""),
		}, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pairs := matchTransfers(tc.lines)
			if linked := pairs[1] == 2 && pairs[2] == 1; linked != tc.want {
				t.Fatalf("linked = %v, want %v (pairs %v)", linked, tc.want, pairs)
			}
		})
	}
}

func TestAFullNumberLetsACounterpartyFitWithEveryDigit(t *testing.T) {
	// A bank that shows only two digits of the account: too few to fit anything by.
	owner := deposit{account: 2, institution: "taishin", number: "************44", statement: 20}
	lines := func(account deposit) []transferLine {
		return []transferLine{
			depositLine(t, 1, esunSavings, "2026-07-21", "-30000", "812 0087654321349***"),
			depositLine(t, 2, account, "2026-07-21", "30000", ""),
		}
	}
	if pairs := matchTransfers(lines(owner)); len(pairs) != 0 {
		t.Fatalf("without the full number: pairs = %v, want none", pairs)
	}
	owner.full = "87654321349234"
	if pairs := matchTransfers(lines(owner)); pairs[1] != 2 {
		t.Fatalf("with the full number: pairs = %v", pairs)
	}
}

func TestAnExchangeLinksTheTwoLegsTheBankPrintsOneReferenceOn(t *testing.T) {
	twd := deposit{account: 5, institution: "nextbank", number: "123456****7890", statement: 50}
	fx := deposit{account: 6, institution: "nextbank", number: "123456****1111", statement: 50}
	const reference = "新臺幣換美元;@31.5000;FT0000001AAA"
	out := func(memo string) transferLine {
		return inUnit(depositLine(t, 1, twd, "2026-02-23", "-300000", ""), "TWD", memo)
	}
	in := func(at deposit, on, memo string) transferLine {
		return inUnit(depositLine(t, 2, at, on, "9523.81", ""), "USD", memo)
	}
	cases := map[string]struct {
		lines []transferLine
		want  bool
	}{
		"the same reference on both, the same day":        {[]transferLine{out(reference), in(fx, "2026-02-23", reference)}, true},
		"another reference":                               {[]transferLine{out(reference), in(fx, "2026-02-23", "新臺幣換美元;@31.5000;FT0000002BBB")}, false},
		"no memo: two amounts in two units prove nothing": {[]transferLine{out(""), in(fx, "2026-02-23", "")}, false},
		"another day":                                     {[]transferLine{out(reference), in(fx, "2026-02-24", reference)}, false},
		"another bank":                                    {[]transferLine{out(reference), in(taishinRichart, "2026-02-23", reference)}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pairs := matchTransfers(tc.lines)
			if linked := pairs[1] == 2 && pairs[2] == 1; linked != tc.want {
				t.Fatalf("linked = %v, want %v (pairs %v)", linked, tc.want, pairs)
			}
		})
	}
}

func TestATransferOrPaymentIsInOneUnit(t *testing.T) {
	pairs := matchTransfers([]transferLine{
		inUnit(depositLine(t, 1, esunSavings, "2026-07-21", "-500", "812 0087654321***234"), "USD", ""),
		depositLine(t, 2, taishinRichart, "2026-07-21", "500", ""),
		inUnit(depositLine(t, 3, esunSavings, "2026-09-22", "-6350", ""), "USD", ""),
		paymentLine(t, 4, "esun", 50, "2026-09-22", "-6350"),
	})
	if len(pairs) != 0 {
		t.Fatalf("pairs = %v, want none across units", pairs)
	}
	usd := "USD"
	bills := []dbq.CardBillsRow{{ID: 50, Institution: "esun", ClosingDate: day(t, "2026-09-07"), DueDate: day(t, "2026-09-22"), Currency: &usd, TotalDue: money(t, "6350")}}
	if paid := matchPaid(bills, []transferLine{depositLine(t, 3, esunSavings, "2026-09-22", "-6350", "")}); len(paid) != 0 {
		t.Fatalf("a US$ bill paid in NT$: %v", paid)
	}
}

func TestEachLineIsInOneLinkAndNearerDaysPairFirst(t *testing.T) {
	pairs := matchTransfers([]transferLine{
		depositLine(t, 1, esunSavings, "2026-09-20", "-1000", ""),
		depositLine(t, 2, esunSavings, "2026-09-22", "-1000", ""),
		paymentLine(t, 3, "esun", 50, "2026-09-22", "-1000"),
	})
	if pairs[3] != 2 || pairs[2] != 3 || pairs[1] != 0 {
		t.Fatalf("pairs = %v, want the same-day outflow", pairs)
	}
}

func bill(t *testing.T, id int64, institution, closing, due, total string) dbq.CardBillsRow {
	t.Helper()
	return dbq.CardBillsRow{ID: id, Institution: institution, ClosingDate: day(t, closing), DueDate: day(t, due), TotalDue: money(t, total)}
}

func TestABillIsPaidByADepositOutflowElseALaterBillsPaymentLineOfItsTotalDue(t *testing.T) {
	bills := []dbq.CardBillsRow{
		bill(t, 50, "esun", "2026-09-07", "2026-09-22", "6350"),
		bill(t, 51, "esun", "2026-10-07", "2026-10-22", "780"),
		bill(t, 60, "cathay", "2026-10-01", "2026-10-17", "1046"),
		bill(t, 61, "cathay", "2026-11-01", "2026-11-17", "900"),
		bill(t, 70, "taishin", "2026-10-07", "2026-10-22", "0"),
		bill(t, 80, "hsbc", "2026-10-21", "2026-11-08", "500"),
	}
	lines := []transferLine{
		depositLine(t, 1, esunSavings, "2026-09-22", "-6350", ""),
		// Cathay's own account is not read: the next bill's payment line shows it was paid.
		paymentLine(t, 2, "cathay", 61, "2026-10-20", "-1046"),
		// Past the due date and its grace.
		depositLine(t, 3, esunSavings, "2026-11-20", "-500", ""),
	}
	paid := matchPaid(bills, lines)
	check := func(id int64, on string, by int64) {
		t.Helper()
		got, ok := paid[id]
		if on == "" {
			if ok {
				t.Fatalf("bill %d paid %+v, want unpaid", id, got)
			}
			return
		}
		if !ok || got.on.Time.Format(time.DateOnly) != on || got.by != by {
			t.Fatalf("bill %d = %+v (%v), want %s by %d", id, got, ok, on, by)
		}
	}
	check(50, "2026-09-22", 1)
	check(51, "", 0)
	check(60, "2026-10-20", 2)
	check(61, "", 0)
	check(70, "2026-10-07", 0) // owes nothing: paid on its closing date
	check(80, "", 0)
}

func TestDigitsFit(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0087654321***234", "876543****9234", true},
		{"0345678***012", "0000345678123012", true},
		{"0345678***012", "0345678***013", false},
		{"***1", "1", false}, // too few agree
		{"73520004****66", "73520004123466", true},
	}
	for _, tc := range cases {
		if got := DigitsFit(tc.a, tc.b, minAgreeingDigits); got != tc.want {
			t.Errorf("DigitsFit(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
