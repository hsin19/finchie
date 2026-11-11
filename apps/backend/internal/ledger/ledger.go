// Package ledger keeps the domain's invariants as rows are written: every source, whether a statement, an
// exchange, a price feed, or the owner, writes units, snapshots, and prices through it, in the caller's
// transaction, so that each rule has one implementation. It reads and writes the database; what the rules
// are lives in domain.
package ledger

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
)

// Units finds the units of a kind with these symbols, creating those no source has reported before
// (ADR 0020). Symbols are spelled as the kind spells them (domain.Symbol), and so are the map's keys: a
// caller looks a unit up by the symbol it passed, spelled likewise.
func Units(ctx context.Context, queries *dbq.Queries, kind domain.UnitKind, symbols ...string) (map[string]int64, error) {
	ids := make(map[string]int64, len(symbols))
	if len(symbols) == 0 {
		return ids, nil
	}
	spelled := make([]string, 0, len(symbols))
	for _, s := range symbols {
		spelled = append(spelled, domain.Symbol(kind, s))
	}
	slices.Sort(spelled)
	rows, err := queries.EnsureUnits(ctx, dbq.EnsureUnitsParams{Kind: string(kind), Symbols: slices.Compact(spelled)})
	if err != nil {
		return nil, fmt.Errorf("units: %w", err)
	}
	for _, r := range rows {
		ids[r.Symbol] = r.ID
	}
	return ids, nil
}

// Snapshot is what a source reports an account holds on a day, with no statement: an exchange's API
// (ADR 0018) or the owner's word (ADR 0019).
type Snapshot struct {
	AccountID int64
	Kind      domain.AccountKind
	// In Taipei (ADR 0004).
	Day time.Time
	// By symbol.
	Holdings map[string]*big.Rat
}

// StoreSnapshot keeps a snapshot as the whole account on its day: it replaces one taken earlier that day,
// and puts every unit the account held before and no longer lists at 0. A unit at 0 of an account that
// holds positions is one it does not hold, as if left out; a deposit account at 0 still has its currency.
// It returns the units of what the account holds, by symbol as their kind spells it.
func StoreSnapshot(ctx context.Context, queries *dbq.Queries, s Snapshot) (map[string]int64, error) {
	unitKind := s.Kind.UnitKind()
	held := map[string]*big.Rat{}
	for symbol, amount := range s.Holdings {
		if amount.Sign() != 0 || !s.Kind.HoldsPositions() {
			held[domain.Symbol(unitKind, symbol)] = amount
		}
	}
	symbols := slices.Sorted(maps.Keys(held))
	units, err := Units(ctx, queries, unitKind, symbols...)
	if err != nil {
		return nil, err
	}

	day := pgtype.Date{Time: s.Day, Valid: true}
	if err := queries.ClearSnapshot(ctx, dbq.ClearSnapshotParams{AccountID: s.AccountID, AsOf: day}); err != nil {
		return nil, fmt.Errorf("clear snapshot: %w", err)
	}
	rows := dbq.InsertSnapshotParams{AccountID: s.AccountID, AsOf: day}
	for _, symbol := range symbols {
		amount, err := numeric.FromRat(held[symbol])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", symbol, err)
		}
		rows.UnitIds = append(rows.UnitIds, units[symbol])
		rows.Amounts = append(rows.Amounts, amount)
	}
	if len(symbols) > 0 {
		if err := queries.InsertSnapshot(ctx, rows); err != nil {
			return nil, fmt.Errorf("insert snapshot: %w", err)
		}
	}
	if err := queries.ImplySoldOut(ctx, dbq.ImplySoldOutParams{AccountID: s.AccountID, AsOf: day}); err != nil {
		return nil, fmt.Errorf("imply sold out: %w", err)
	}
	return units, nil
}

// Price is what `Amount` of a unit was worth in `QuoteAmount` of a money unit, at a moment (ADR 0020). The
// amounts are decimals as the source prints them, kept so (ADR 0004).
type Price struct {
	Symbol      string
	AsOf        time.Time
	Amount      string
	Quote       string
	QuoteAmount string
}

// StorePrices keeps a source's prices of units of a kind: one per unit, source, and day in Taipei, a later
// one the same day replacing it. A price of a unit no account has held is dropped, as is one of a unit in
// itself.
func StorePrices(ctx context.Context, queries *dbq.Queries, source domain.PriceSource, kind domain.UnitKind, prices []Price) error {
	params := dbq.UpsertPricesParams{Source: string(source), Kind: string(kind)}
	for _, p := range prices {
		symbol, quote := domain.Symbol(kind, p.Symbol), domain.Symbol(domain.Money, p.Quote)
		if kind == domain.Money && symbol == quote {
			continue
		}
		amount, err := numeric.FromText(p.Amount)
		if err != nil {
			return fmt.Errorf("price of %s: %w", symbol, err)
		}
		quoteAmount, err := numeric.FromText(p.QuoteAmount)
		if err != nil {
			return fmt.Errorf("price of %s: %w", symbol, err)
		}
		params.Symbols = append(params.Symbols, symbol)
		params.AsOf = append(params.AsOf, pgtype.Timestamptz{Time: p.AsOf, Valid: true})
		params.Amounts = append(params.Amounts, amount)
		params.Quotes = append(params.Quotes, quote)
		params.QuoteAmounts = append(params.QuoteAmounts, quoteAmount)
	}
	if len(params.Symbols) == 0 {
		return nil
	}
	if err := queries.UpsertPrices(ctx, params); err != nil {
		return fmt.Errorf("store prices: %w", err)
	}
	return nil
}

// Gaps reads where accounts' balances do not join up (ADR 0013), of one account or, with none, of all.
func Gaps(ctx context.Context, queries *dbq.Queries, accountID *int64) ([]domain.Gap, error) {
	rows, err := queries.AccountGaps(ctx, dbq.AccountGapsParams{
		AccountID:       accountID,
		Itemized:        domain.KindsWhere(domain.AccountKind.Itemized),
		EveryLineListed: domain.KindsWhere(domain.AccountKind.EveryLineListed),
	})
	if err != nil {
		return nil, fmt.Errorf("account gaps: %w", err)
	}
	gaps := make([]domain.Gap, 0, len(rows))
	for _, r := range rows {
		gaps = append(gaps, domain.Gap{
			AccountID: r.AccountID, Unit: r.Unit, From: r.FromDate.Time, To: r.ToDate.Time,
			Unseen: numeric.ToRat(r.Unseen), Itemized: r.Itemized,
		})
	}
	return gaps, nil
}

var (
	// ErrNoUnit means no unit has the id.
	ErrNoUnit = errors.New("no such unit")
	// ErrNoPrice means a price is not one: it must be a number, 0 or more, in a money unit some account
	// holds, of any unit but the base currency, which is worth itself (ADR 0020).
	ErrNoPrice = errors.New("no price")
)

// SetOwnerPrice keeps the owner's price of a unit, as of now: `value` of the quote unit for one. It counts
// while it is the unit's newest, like any source's (ADR 0020).
func SetOwnerPrice(ctx context.Context, queries *dbq.Queries, unitID int64, value string, quote string, at time.Time) error {
	unit, err := queries.GetUnit(ctx, unitID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoUnit
	}
	if err != nil {
		return fmt.Errorf("get unit: %w", err)
	}
	quote = domain.Symbol(domain.Money, quote)
	amount, ok := new(big.Rat).SetString(value)
	switch {
	case !ok || amount.Sign() < 0:
		return refuse(ErrNoPrice, "%q is no price: a number, 0 or more", value)
	case unit.Kind == domain.Money && unit.Symbol == domain.BaseCurrency:
		return refuse(ErrNoPrice, "%s is worth itself and has no price", domain.BaseCurrency)
	case unit.Kind == domain.Money && unit.Symbol == quote:
		return refuse(ErrNoPrice, "%s is not priced in itself", quote)
	}
	quotes, err := queries.ListUnits(ctx, ptr(string(domain.Money)))
	if err != nil {
		return fmt.Errorf("list units: %w", err)
	}
	if !slices.ContainsFunc(quotes, func(u dbq.ListUnitsRow) bool { return u.Symbol == quote }) {
		return refuse(ErrNoPrice, "%s is no money unit any account holds", quote)
	}
	return StorePrices(ctx, queries, domain.SourceOwner, unit.Kind, []Price{
		{Symbol: unit.Symbol, AsOf: at, Amount: "1", Quote: quote, QuoteAmount: value},
	})
}

func ptr[T any](v T) *T { return &v }
