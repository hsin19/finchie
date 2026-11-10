package ledger

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hsin19/finchie/apps/backend/internal/domain"
)

const (
	kindPurchase = string(domain.Purchase)
	kindFee      = string(domain.ForeignTransactionFee)
	kindRefund   = string(domain.Refund)
)

var nextLine int64

type lineOption func(*line)

func card(last4 string) lineOption { return func(l *line) { l.CardLast4 = &last4 } }

func foreign(currency, amount string) lineOption {
	return func(l *line) {
		l.OriginalCurrency = &currency
		if err := l.OriginalAmount.Scan(amount); err != nil {
			panic(err)
		}
	}
}

func onStatement(id int64) lineOption { return func(l *line) { l.StatementID = id } }

func newLine(date, description, kind, amount string, options ...lineOption) line {
	nextLine++
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		panic(err)
	}
	l := line{ID: nextLine, StatementID: 1, Institution: "taishin", TransactionDate: pgtype.Date{Time: d, Valid: true}, Description: description, Kind: domain.LineKind(kind)}
	if err := l.Amount.Scan(amount); err != nil {
		panic(err)
	}
	for _, option := range options {
		option(&l)
	}
	return l
}

func assertParent(t *testing.T, parents map[int64]int64, child, parent line) {
	t.Helper()
	if got, ok := parents[child.ID]; !ok || got != parent.ID {
		t.Errorf("%q: parent = %d (linked %v), want %q (%d)", child.Description, got, ok, parent.Description, parent.ID)
	}
}

func assertNoParent(t *testing.T, parents map[int64]int64, child line) {
	t.Helper()
	if got, ok := parents[child.ID]; ok {
		t.Errorf("%q: parent = %d, want none", child.Description, got)
	}
}

func TestFeesGoToTheirPurchases(t *testing.T) {
	emirates := newLine("2026-10-16", "SSPEMIRATESLLCABUDH", kindPurchase, "556")
	autogrill := newLine("2026-10-16", "AutogrillMiddleEast", kindPurchase, "635")
	dutyFree := newLine("2026-10-16", "LCAUO5ADDFDUTYFREEA", kindPurchase, "918")
	emiratesFee := newLine("2026-10-16", "國外交易手續費SSPEMIRATESL", kindFee, "8")
	// OCR read "Autogrill" as "Autogril1": no name match, but 10 / 635 is 1.6%.
	autogrillFee := newLine("2026-10-16", "國外交易手續費Autogril1Mid", kindFee, "10")
	dutyFreeFee := newLine("2026-10-16", "國外交易手續費LCAUO5ADDFDU", kindFee, "14")
	otherDay := newLine("2026-10-17", "SSPEMIRATESLLCABUDH", kindPurchase, "556")

	parents := matchParents([]line{emirates, autogrill, dutyFree, emiratesFee, autogrillFee, dutyFreeFee, otherDay})
	assertParent(t, parents, emiratesFee, emirates)
	assertParent(t, parents, autogrillFee, autogrill)
	assertParent(t, parents, dutyFreeFee, dutyFree)
	if len(parents) != 3 {
		t.Errorf("parents = %v, want only the three fees", parents)
	}
}

func TestUnnamedFeesGoByRateOnePerPurchase(t *testing.T) {
	stream := newLine("2026-09-18", "STREAMING*SERVICE", kindPurchase, "479", onStatement(2))
	coffee := newLine("2026-09-18", "COFFEE", kindPurchase, "120", onStatement(2))
	game := newLine("2026-09-18", "GAME*STORE", kindPurchase, "470", onStatement(2))
	// Either fee fits either purchase (7 / 120 does not); each purchase takes one.
	first := newLine("2026-09-18", "國外交易服務費", kindFee, "7", onStatement(2))
	second := newLine("2026-09-18", "國外交易服務費", kindFee, "7", onStatement(2))
	// Same day, other statement: not a candidate.
	elsewhere := newLine("2026-09-18", "STREAMING*SERVICE", kindPurchase, "479", onStatement(3))

	parents := matchParents([]line{stream, coffee, game, first, second, elsewhere})
	got := map[int64]bool{parents[first.ID]: true, parents[second.ID]: true}
	if len(parents) != 2 || !got[stream.ID] || !got[game.ID] {
		t.Errorf("parents = %v, want the two fees on %d and %d", parents, stream.ID, game.ID)
	}
}

func TestNamedFeesGoByRateWithinAMerchant(t *testing.T) {
	// Two purchases at one merchant on one day, each with a named fee; the larger purchase and the
	// smaller fee come first, so going by line order would cross them.
	large := newLine("2026-08-03", "CITYMART DOWNTOWN", kindPurchase, "400", card("1111"))
	small := newLine("2026-08-03", "CITYMART DOWNTOWN", kindPurchase, "120", card("1111"))
	smallFee := newLine("2026-08-03", "國外交易手續費CITYMART", kindFee, "2", card("1111"))
	largeFee := newLine("2026-08-03", "國外交易手續費CITYMART", kindFee, "6", card("1111"))

	parents := matchParents([]line{large, small, smallFee, largeFee})
	assertParent(t, parents, largeFee, large)
	assertParent(t, parents, smallFee, small)
}

func TestFeesStayOnTheirCard(t *testing.T) {
	onA := newLine("2026-09-20", "UBER *TRIP", kindPurchase, "300", card("1111"))
	onB := newLine("2026-09-20", "UBER *TRIP", kindPurchase, "300", card("2222"))
	fee := newLine("2026-09-20", "國外交易手續費UBER *TRIP", kindFee, "5", card("2222"))

	assertParent(t, matchParents([]line{onA, onB, fee}), fee, onB)
}

func TestFullRefundsGoToTheLatestPurchaseBefore(t *testing.T) {
	older := newLine("2026-06-01", "HOTELBOOKING*RIVERSIDE", kindPurchase, "4200", card("1111"))
	booking := newLine("2026-07-07", "HOTELBOOKING*RIVERSIDE", kindPurchase, "4200", card("1111"))
	other := newLine("2026-07-07", "HOTELBOOKING*HARBOURVIEW", kindPurchase, "9800", card("1111"))
	fee := newLine("2026-07-07", "國外交易服務費", kindFee, "63", card("1111"))
	// On the next statement: the refund, and the fee was not refunded.
	refund := newLine("2026-07-20", "HOTELBOOKING*RIVERSIDE", kindRefund, "-4200", card("1111"), onStatement(2))
	// A second refund of the same amount takes back the older booking, not this one again.
	again := newLine("2026-07-21", "HOTELBOOKING*RIVERSIDE", kindRefund, "-4200", card("1111"), onStatement(2))
	third := newLine("2026-07-22", "HOTELBOOKING*RIVERSIDE", kindRefund, "-4200", card("1111"), onStatement(2))

	parents := matchParents([]line{older, booking, other, fee, refund, again, third})
	assertParent(t, parents, refund, booking)
	assertParent(t, parents, fee, booking)
	assertParent(t, parents, again, older)
	assertNoParent(t, parents, third)
}

func TestRefundsInPartsAddUpInTheForeignCurrency(t *testing.T) {
	order := newLine("2026-07-06", "MARKETPLACE*ORDER31", kindPurchase, "2150", card("1111"), foreign("CNY", "470.00"))
	small := newLine("2026-06-26", "MARKETPLACE*ORDER7", kindPurchase, "360", card("1111"), foreign("CNY", "78.00"))
	// NT$ 431 + 1712 is not 2150 (the rate moved); CNY 94.00 + 376.00 is 470.00.
	part1 := newLine("2026-07-06", "MARKETPLACE*ORDER31", kindRefund, "-431", card("1111"), foreign("CNY", "-94.00"))
	part2 := newLine("2026-07-06", "MARKETPLACE*ORDER31", kindRefund, "-1712", card("1111"), foreign("CNY", "-376.00"))

	parents := matchParents([]line{order, small, part1, part2})
	assertParent(t, parents, part1, order)
	assertParent(t, parents, part2, order)
}

func TestUnclearRefundsKeepNoParent(t *testing.T) {
	a := newLine("2026-05-16", "MARKETPLACE*SHOP", kindPurchase, "500", card("1111"), foreign("CNY", "110.00"))
	b := newLine("2026-05-13", "MARKETPLACE*SHOP", kindPurchase, "300", card("1111"), foreign("CNY", "65.00"))
	// Part of one of several orders.
	partial := newLine("2026-05-31", "MARKETPLACE*SHOP", kindRefund, "-120", card("1111"), foreign("CNY", "-26.00"))
	// No purchase of that merchant at all.
	taxFree := newLine("2026-10-16", "TAX FREE REFUND SE", kindRefund, "-1500")
	// The right amount, but too late, or on another card.
	late := newLine("2026-09-01", "MARKETPLACE*SHOP", kindRefund, "-500", card("1111"))
	otherCard := newLine("2026-05-20", "MARKETPLACE*SHOP", kindRefund, "-300", card("2222"))

	parents := matchParents([]line{a, b, partial, taxFree, late, otherCard})
	if len(parents) != 0 {
		t.Errorf("parents = %v, want none", parents)
	}
}

func TestPointsRedemptionsGoToThatDaysPurchase(t *testing.T) {
	tea := newLine("2026-07-05", "外送-茶飲店ABC", kindPurchase, "120", card("3333"))
	earlierTea := newLine("2026-07-01", "外送-茶飲店ABC", kindPurchase, "120", card("3333"))
	points := newLine("2026-07-05", "點數折抵_外送-茶飲", kindRefund, "-30", card("3333"))

	parents := matchParents([]line{tea, earlierTea, points})
	assertParent(t, parents, points, tea)
}

func TestARedemptionTheParserClassesAsARebateGoesToItsPurchaseButCashbackKeepsNone(t *testing.T) {
	tea := newLine("2026-07-05", "外送-茶飲店ABC", kindPurchase, "120", card("3333"))
	points := newLine("2026-07-05", "點數折抵_外送-茶飲", string(domain.Rebate), "-30", card("3333"))
	cashback := newLine("2026-07-05", "外送-茶飲店ABC 回饋", string(domain.Rebate), "-6", card("3333"))

	parents := matchParents([]line{tea, points, cashback})
	assertParent(t, parents, points, tea)
	assertNoParent(t, parents, cashback)
}

func TestARefundAddsUpExactlyWhateverTheDecimals(t *testing.T) {
	// Added up exactly, never through a float (ADR 0004).
	purchase := newLine("2026-07-01", "BOOKSHOP", kindPurchase, "0.30", foreign("USD", "0.30"))
	first := newLine("2026-07-03", "BOOKSHOP", kindRefund, "-0.10", foreign("USD", "-0.10"))
	second := newLine("2026-07-03", "BOOKSHOP", kindRefund, "-0.20", foreign("USD", "-0.20"))

	parents := matchParents([]line{purchase, first, second})
	assertParent(t, parents, first, purchase)
	assertParent(t, parents, second, purchase)
}

// Stored out of order and parsed again, a refund still finds its purchase on another statement.
