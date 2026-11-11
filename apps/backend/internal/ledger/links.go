package ledger

import (
	"cmp"
	"context"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
)

// A line that adjusts another points at it as its parent (ADR 0009): a foreign transaction fee at the
// purchase it was charged for, a refund or a points redemption at the purchase it takes back. Banks
// print no such link, so it is worked out from dates, cards, merchant names, and amounts; a line that
// fits no purchase clearly keeps no parent.

const (
	// A refund comes back within this long of its purchase.
	refundWindow = 90 * 24 * time.Hour
	// The shorter of two merchant names compared by prefix must be at least this long.
	minKeyLength = 4
)

var (
	// Banks charge about 1.5% of a foreign purchase, as a separate line on the same day and card.
	typicalFeeRate = big.NewRat(15, 1000)
	// A fee further than this from the typical rate is not that purchase's.
	maxRateDistance = big.NewRat(1, 100)
)

type line = dbq.LinkLinesRow

// linkKinds are the lines a parent link starts or ends at. A points redemption is a rebate, as the parser
// classes it, or a refund, as banks that print it as a negative purchase have it.
var linkKinds = []string{string(domain.Purchase), string(domain.ForeignTransactionFee), string(domain.Refund), string(domain.Rebate)}

// redemption reports whether a line spends points on a purchase ("點數折抵_外送-茶飲").
func redemption(l line) bool {
	return (l.Kind == domain.Rebate || l.Kind == domain.Refund) && strings.HasPrefix(l.Description, "點數折抵")
}

// Labels banks put before the merchant on fee and points lines: "國外交易手續費SSPEMIRATESL",
// "點數折抵_外送-茶飲".
var lineLabel = regexp.MustCompile(`國外交易(手續|服務)費|點數折抵`)

// merchantKey keeps the letters and digits of the merchant part, lowercased: "A·T·B·SERVIZ" → "atbserviz".
func merchantKey(description string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, lineLabel.ReplaceAllString(description, ""))
}

// sameMerchant compares merchant names by prefix: banks cut descriptions at different lengths.
func sameMerchant(a, b string) bool {
	short, long := a, b
	if utf8.RuneCountInString(short) > utf8.RuneCountInString(long) {
		short, long = long, short
	}
	return utf8.RuneCountInString(short) >= minKeyLength && strings.HasPrefix(long, short)
}

// E.SUN and Taishin list several cards on one statement; a line belongs to a purchase on its card.
func sameCard(a, b *string) bool {
	return a == nil || b == nil || *a == *b
}

// amount reads an amount exactly (ADR 0004); a NULL one is 0.
func amount(n pgtype.Numeric) *big.Rat {
	if r := numeric.ToRat(n); r != nil {
		return r
	}
	return new(big.Rat)
}

// round rounds half away from zero, as banks round a fee.
func round(r *big.Rat) *big.Int {
	n, rem := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int))
	if new(big.Int).Mul(rem.Abs(rem), big.NewInt(2)).Cmp(r.Denom()) >= 0 {
		n.Add(n, big.NewInt(int64(r.Sign())))
	}
	return n
}

// Relink works out every link again from all the lines and stores them, in the caller's transaction:
// parents (ADR 0009), transfers, and bills' payments (ADR 0016). Whatever changes the lines, or what fits
// them, relinks in the same transaction, so that no link is ever stale. The lock keeps two such
// transactions from interleaving; taking it again in the same transaction is a no-op. It returns how many
// lines have a parent.
func Relink(ctx context.Context, queries *dbq.Queries) (int, error) {
	if err := queries.LockLinks(ctx); err != nil {
		return 0, fmt.Errorf("lock links: %w", err)
	}
	lines, err := queries.LinkLines(ctx, linkKinds)
	if err != nil {
		return 0, fmt.Errorf("list lines to link: %w", err)
	}
	if err := queries.ClearParents(ctx); err != nil {
		return 0, fmt.Errorf("clear links: %w", err)
	}
	// Transfers and bills' payments are links of the same kind, worked out under the same lock (ADR 0016).
	if err := retransfer(ctx, queries); err != nil {
		return 0, err
	}
	parents := matchParents(lines)
	if len(parents) == 0 {
		return 0, nil
	}
	params := dbq.SetParentsParams{}
	for child, parent := range parents {
		params.Ids = append(params.Ids, child)
		params.ParentIds = append(params.ParentIds, parent)
	}
	if err := queries.SetParents(ctx, params); err != nil {
		return 0, fmt.Errorf("store links: %w", err)
	}
	return len(parents), nil
}

// RelinkAll works out every link again in a transaction of its own; run at start, it links lines stored
// before links existed or under older rules. It returns how many lines have a parent.
func RelinkAll(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var linked int
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		linked, err = Relink(ctx, dbq.New(tx))
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("relink: %w", err)
	}
	return linked, nil
}

// matchParents returns child line ID → parent line ID.
func matchParents(lines []line) map[int64]int64 {
	parents := matchFees(lines)
	for child, parent := range matchRefunds(lines) {
		parents[child] = parent
	}
	return parents
}

// matchFees pairs each foreign transaction fee with the purchase it was charged for: on the same
// statement, day, and card. A fee that names its merchant goes to that merchant's purchase; otherwise,
// or when the name was misread, to the purchase whose amount it is closest to 1.5% of. Each purchase
// takes at most one fee, best pairs first.
func matchFees(lines []line) map[int64]int64 {
	type day struct {
		statement int64
		date      time.Time
	}
	purchases := map[day][]line{}
	for _, l := range lines {
		if l.Kind == domain.Purchase {
			key := day{l.StatementID, l.TransactionDate.Time}
			purchases[key] = append(purchases[key], l)
		}
	}

	type pair struct {
		fee, purchase int64
		// Lower is better: by name before by rate, then a fee that is exactly 1.5% rounded, then the
		// distance from 1.5%. Named pairs are ranked by rate too: one merchant can have several
		// purchases on a day.
		byRate, inexact int
		// Nil when there is no rate: farther than any.
		distance *big.Rat
	}
	rated := func(fee, purchase line, byRate int) pair {
		feeAmount, purchaseAmount := amount(fee.Amount), amount(purchase.Amount)
		p := pair{fee: fee.ID, purchase: purchase.ID, byRate: byRate, inexact: 1}
		if purchaseAmount.Sign() > 0 {
			rate := new(big.Rat).Quo(feeAmount, purchaseAmount)
			p.distance = rate.Abs(rate.Sub(rate, typicalFeeRate))
			if round(new(big.Rat).Mul(purchaseAmount, typicalFeeRate)).Cmp(round(feeAmount)) == 0 {
				p.inexact = 0
			}
		}
		return p
	}
	closer := func(a, b *big.Rat) int {
		switch {
		case a == nil && b == nil:
			return 0
		case a == nil:
			return 1
		case b == nil:
			return -1
		default:
			return a.Cmp(b)
		}
	}
	var pairs []pair
	for _, fee := range lines {
		if fee.Kind != domain.ForeignTransactionFee {
			continue
		}
		var candidates []line
		for _, p := range purchases[day{fee.StatementID, fee.TransactionDate.Time}] {
			if sameCard(fee.CardLast4, p.CardLast4) {
				candidates = append(candidates, p)
			}
		}

		key := merchantKey(fee.Description)
		var named []pair
		for _, p := range candidates {
			if key != "" && strings.HasPrefix(merchantKey(p.Description), key) {
				named = append(named, rated(fee, p, 0))
			}
		}
		if len(named) > 0 {
			pairs = append(pairs, named...)
			continue
		}
		for _, p := range candidates {
			if r := rated(fee, p, 1); r.distance != nil && r.distance.Cmp(maxRateDistance) <= 0 {
				pairs = append(pairs, r)
			}
		}
	}

	slices.SortFunc(pairs, func(a, b pair) int {
		return cmp.Or(
			cmp.Compare(a.byRate, b.byRate),
			cmp.Compare(a.inexact, b.inexact),
			closer(a.distance, b.distance),
			cmp.Compare(a.fee, b.fee),
			cmp.Compare(a.purchase, b.purchase),
		)
	})
	parents := map[int64]int64{}
	taken := map[int64]bool{}
	for _, p := range pairs {
		if _, done := parents[p.fee]; done || taken[p.purchase] {
			continue
		}
		parents[p.fee] = p.purchase
		taken[p.purchase] = true
	}
	return parents
}

// matchRefunds pairs refunds with the purchases they take back, which may be on an earlier statement:
// same bank and card, within 90 days before, same merchant. Only clear cases link:
//   - a points redemption ("點數折抵_…"): the one purchase of that merchant on the same day;
//   - a full refund: a purchase of the same amount, in NT$ or in the foreign currency; the latest one
//     before the refund, each purchase refunded in full once;
//   - a refund in parts: refunds of one merchant, card, and day that add up to such a purchase.
//
// Anything else, such as part of one of several purchases, keeps no parent.
func matchRefunds(lines []line) map[int64]int64 {
	purchases := map[string][]line{}
	var refunds []line
	for _, l := range lines {
		switch {
		case l.Kind == domain.Purchase:
			purchases[l.Institution] = append(purchases[l.Institution], l)
		case l.Kind == domain.Refund || redemption(l):
			refunds = append(refunds, l)
		}
	}
	slices.SortFunc(refunds, func(a, b line) int {
		return cmp.Or(a.TransactionDate.Time.Compare(b.TransactionDate.Time), cmp.Compare(a.ID, b.ID))
	})

	// The purchases a refund can take back, latest first.
	candidates := func(r line) []line {
		key := merchantKey(r.Description)
		var found []line
		for _, p := range purchases[r.Institution] {
			gap := r.TransactionDate.Time.Sub(p.TransactionDate.Time)
			if gap >= 0 && gap <= refundWindow && sameCard(r.CardLast4, p.CardLast4) && sameMerchant(key, merchantKey(p.Description)) {
				found = append(found, p)
			}
		}
		slices.SortFunc(found, func(a, b line) int {
			return cmp.Or(b.TransactionDate.Time.Compare(a.TransactionDate.Time), cmp.Compare(a.ID, b.ID))
		})
		return found
	}

	parents := map[int64]int64{}
	refunded := map[int64]bool{}
	var rest []line
	for _, r := range refunds {
		found := candidates(r)
		if redemption(r) {
			var sameDay []line
			for _, p := range found {
				if p.TransactionDate.Time.Equal(r.TransactionDate.Time) {
					sameDay = append(sameDay, p)
				}
			}
			if len(sameDay) == 1 {
				parents[r.ID] = sameDay[0].ID
			}
			continue
		}
		matched := false
		for _, p := range found {
			if !refunded[p.ID] && takesBack([]line{r}, p) {
				parents[r.ID] = p.ID
				refunded[p.ID] = true
				matched = true
				break
			}
		}
		if !matched {
			rest = append(rest, r)
		}
	}

	// Refunds in parts: the refunds left over, by merchant, card, and day.
	type batchKey struct {
		institution, card, merchant string
		date                        time.Time
	}
	batches := map[batchKey][]line{}
	var order []batchKey
	for _, r := range rest {
		key := batchKey{institution: r.Institution, merchant: merchantKey(r.Description), date: r.TransactionDate.Time}
		if r.CardLast4 != nil {
			key.card = *r.CardLast4
		}
		if batches[key] == nil {
			order = append(order, key)
		}
		batches[key] = append(batches[key], r)
	}
	for _, key := range order {
		batch := batches[key]
		if len(batch) < 2 {
			continue
		}
		for _, p := range candidates(batch[0]) {
			if !refunded[p.ID] && takesBack(batch, p) {
				for _, r := range batch {
					parents[r.ID] = p.ID
				}
				refunded[p.ID] = true
				break
			}
		}
	}
	return parents
}

// takesBack reports whether the refunds add up to the whole purchase, in NT$ or in its foreign
// currency.
func takesBack(refunds []line, purchase line) bool {
	twd, foreign := new(big.Rat), new(big.Rat)
	inCurrency := purchase.OriginalCurrency != nil && purchase.OriginalAmount.Valid
	for _, r := range refunds {
		twd.Add(twd, amount(r.Amount))
		if !inCurrency || r.OriginalCurrency == nil || !r.OriginalAmount.Valid || *r.OriginalCurrency != *purchase.OriginalCurrency {
			inCurrency = false
			continue
		}
		foreign.Add(foreign, amount(r.OriginalAmount))
	}
	if twd.Neg(twd).Cmp(amount(purchase.Amount)) == 0 {
		return true
	}
	return inCurrency && foreign.Neg(foreign).Cmp(amount(purchase.OriginalAmount)) == 0
}
