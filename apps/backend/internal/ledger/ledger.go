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
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

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
		gaps = append(gaps, toGap(r))
	}
	return gaps, nil
}

func toGap(r dbq.AccountGapsRow) domain.Gap {
	return domain.Gap{
		AccountID: r.AccountID, Unit: r.Unit, From: r.FromDate.Time, To: r.ToDate.Time,
		Unseen: numeric.ToRat(r.Unseen), Itemized: r.Itemized, ReportedFrom: r.ReportedFrom.Time,
	}
}

// ErrNoGap means the account has no such gap to fill: its two balances join up (a fill among their lines
// included), are no longer consecutive, or no source could show what moved between them.
var ErrNoGap = errors.New("no such gap")

// Fill is what the owner made up for a gap: a line of its own.
type Fill struct {
	ID  int64
	Gap domain.Gap
	// What the owner wrote about it; nil when nothing.
	Memo *string
}

// FillGap makes up for one of an account's gaps, named by its unit and its two balances' days, with a line
// for what it lacks, on the day domain.Gap.FilledOn gives, with the balance it leaves. A memo, when there
// is one, goes with it. A fill already dated between the two balances, one a statement found later set
// aside among them, is the one filled again, with its own memo unless another is given.
func FillGap(ctx context.Context, pool *pgxpool.Pool, accountID int64, unit string, from, to time.Time, memo *string) (Fill, error) {
	var fill Fill
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		queries := dbq.New(tx)
		rows, err := queries.AccountGaps(ctx, dbq.AccountGapsParams{
			AccountID:       &accountID,
			Itemized:        domain.KindsWhere(domain.AccountKind.Itemized),
			EveryLineListed: domain.KindsWhere(domain.AccountKind.EveryLineListed),
		})
		if err != nil {
			return fmt.Errorf("account gaps: %w", err)
		}
		i := slices.IndexFunc(rows, func(r dbq.AccountGapsRow) bool {
			return r.Unit == unit && r.FromDate.Time.Equal(from) && r.ToDate.Time.Equal(to)
		})
		if i < 0 {
			return ErrNoGap
		}
		r := rows[i]
		gap := toGap(r)
		if !gap.Itemized || !gap.From.Before(gap.To) {
			return ErrNoGap
		}
		on := pgtype.Date{Time: gap.FilledOn(), Valid: true}
		balance := numeric.Add(r.FromAmount, r.Unseen)
		id, err := queries.FillLineIn(ctx, dbq.FillLineInParams{AccountID: accountID, UnitID: r.UnitID, FromDate: r.FromDate, ToDate: r.ToDate})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			line, err := queries.InsertFillLine(ctx, dbq.InsertFillLineParams{
				OnDate: on, Amount: r.Unseen, AccountID: accountID, UnitID: r.UnitID, Memo: fillMemo(memo), Balance: balance,
			})
			if err != nil {
				return fmt.Errorf("insert fill line: %w", err)
			}
			fill = Fill{ID: line.ID, Gap: gap, Memo: line.Memo}
		case err != nil:
			return fmt.Errorf("fill of the gap: %w", err)
		default:
			line, err := queries.UpdateFillLine(ctx, dbq.UpdateFillLineParams{ID: id, OnDate: on, Amount: r.Unseen, Balance: balance, Memo: fillMemo(memo)})
			if err != nil {
				return fmt.Errorf("update fill line: %w", err)
			}
			fill = Fill{ID: line.ID, Gap: gap, Memo: line.Memo}
		}
		return nil
	})
	if err != nil {
		return Fill{}, fmt.Errorf("fill gap: %w", err)
	}
	return fill, nil
}

// ErrNoFill means the account has no fill with the id.
var ErrNoFill = errors.New("no such fill")

// SetFillMemo replaces what the owner wrote about a fill; an empty memo clears it.
func SetFillMemo(ctx context.Context, queries *dbq.Queries, accountID, fillID int64, memo string) error {
	updated, err := queries.SetFillMemo(ctx, dbq.SetFillMemoParams{ID: fillID, AccountID: accountID, Memo: fillMemo(&memo)})
	if err != nil {
		return fmt.Errorf("set memo of fill %d: %w", fillID, err)
	}
	if updated == 0 {
		return ErrNoFill
	}
	return nil
}

// fillMemo is a memo as stored: trimmed, and nil when there is nothing left.
func fillMemo(memo *string) *string {
	if memo == nil {
		return nil
	}
	if trimmed := strings.TrimSpace(*memo); trimmed != "" {
		return &trimmed
	}
	return nil
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
