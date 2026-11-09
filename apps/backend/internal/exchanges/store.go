package exchanges

import (
	"context"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/ingest"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// Store keeps an exchange account's snapshot taken at a moment, whole or not at all (ADR 0018): it
// replaces a snapshot already taken that day in Taipei, puts every unit it no longer lists at 0, and keeps
// its valuation and its coins' prices (ADR 0020).
func Store(ctx context.Context, pool *pgxpool.Pool, institution string, at time.Time, snapshot Snapshot) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // a no-op once committed
	queries := dbq.New(tx)

	accountID, err := queries.UpsertAccount(ctx, dbq.UpsertAccountParams{Institution: institution, Kind: ingest.KindExchange})
	if err != nil {
		return fmt.Errorf("upsert account: %w", err)
	}
	// A unit at 0 is one the account no longer holds; ImplySoldOut puts it there if it held some.
	held := slices.DeleteFunc(slices.Sorted(maps.Keys(snapshot.Holdings)), func(unit string) bool {
		return snapshot.Holdings[unit].Sign() == 0
	})
	symbols := held
	if v := snapshot.Valuation; v != nil {
		symbols = append(slices.Clone(held), v.Unit)
	}
	units, err := ingest.Units(ctx, queries, ingest.UnitMoney, symbols...)
	if err != nil {
		return err
	}

	asOf := pgtype.Date{Time: taipei.Today(at), Valid: true}
	if err := queries.ClearSnapshot(ctx, dbq.ClearSnapshotParams{AccountID: accountID, AsOf: asOf}); err != nil {
		return fmt.Errorf("clear snapshot: %w", err)
	}
	holdings := dbq.InsertSnapshotParams{AccountID: accountID, AsOf: asOf}
	for _, unit := range held {
		n, err := numeric.FromRat(snapshot.Holdings[unit])
		if err != nil {
			return fmt.Errorf("%s: %w", unit, err)
		}
		holdings.UnitIds = append(holdings.UnitIds, units[unit])
		holdings.Amounts = append(holdings.Amounts, n)
	}
	if len(held) > 0 {
		if err := queries.InsertSnapshot(ctx, holdings); err != nil {
			return fmt.Errorf("insert snapshot: %w", err)
		}
	}
	if err := queries.ImplySoldOut(ctx, dbq.ImplySoldOutParams{AccountID: accountID, AsOf: asOf}); err != nil {
		return fmt.Errorf("imply sold out: %w", err)
	}
	if err := queries.ClearValuation(ctx, dbq.ClearValuationParams{AccountID: accountID, AsOf: asOf}); err != nil {
		return fmt.Errorf("clear valuation: %w", err)
	}
	if v := snapshot.Valuation; v != nil {
		n, err := numeric.FromRat(v.Amount)
		if err != nil {
			return fmt.Errorf("valuation: %w", err)
		}
		if err := queries.InsertValuation(ctx, dbq.InsertValuationParams{AccountID: accountID, AsOf: asOf, UnitID: units[v.Unit], Amount: n}); err != nil {
			return fmt.Errorf("insert valuation: %w", err)
		}
	}
	if err := storePrices(ctx, queries, institution, at, snapshot.Prices); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// storePrices keeps the exchange's prices as its own (ADR 0020): one of each coin is worth the amount.
func storePrices(ctx context.Context, queries *dbq.Queries, source string, at time.Time, prices map[string]Value) error {
	if len(prices) == 0 {
		return nil
	}
	params := dbq.UpsertPricesParams{Source: source, Kind: ingest.UnitMoney}
	one := pgtype.Numeric{Int: big.NewInt(1), Valid: true}
	for _, coin := range slices.Sorted(maps.Keys(prices)) {
		price := prices[coin]
		n, err := numeric.FromRat(price.Amount)
		if err != nil {
			return fmt.Errorf("price of %s: %w", coin, err)
		}
		params.Symbols = append(params.Symbols, coin)
		params.AsOf = append(params.AsOf, pgtype.Timestamptz{Time: at, Valid: true})
		params.Amounts = append(params.Amounts, one)
		params.Quotes = append(params.Quotes, price.Unit)
		params.QuoteAmounts = append(params.QuoteAmounts, n)
	}
	if err := queries.UpsertPrices(ctx, params); err != nil {
		return fmt.Errorf("store prices: %w", err)
	}
	return nil
}
