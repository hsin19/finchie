package exchanges

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/ingest"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
)

// Store keeps an exchange account's snapshot of the day, whole or not at all (ADR 0018): it replaces a
// snapshot already taken that day, puts every unit it no longer lists at 0, and keeps its valuation.
func Store(ctx context.Context, pool *pgxpool.Pool, institution string, day time.Time, snapshot Snapshot) error {
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
	asOf := pgtype.Date{Time: day, Valid: true}
	if err := queries.ClearSnapshot(ctx, dbq.ClearSnapshotParams{AccountID: accountID, AsOf: asOf}); err != nil {
		return fmt.Errorf("clear snapshot: %w", err)
	}
	held := dbq.InsertSnapshotParams{AccountID: accountID, AsOf: asOf}
	for _, unit := range slices.Sorted(maps.Keys(snapshot.Holdings)) {
		amount := snapshot.Holdings[unit]
		// A unit at 0 is one the account no longer holds; ImplySoldOut puts it there if it held some.
		if amount.Sign() == 0 {
			continue
		}
		n, err := numeric.FromRat(amount)
		if err != nil {
			return fmt.Errorf("%s: %w", unit, err)
		}
		held.Units = append(held.Units, unit)
		held.Amounts = append(held.Amounts, n)
	}
	if len(held.Units) > 0 {
		if err := queries.InsertSnapshot(ctx, held); err != nil {
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
		if err := queries.InsertValuation(ctx, dbq.InsertValuationParams{AccountID: accountID, AsOf: asOf, Unit: v.Unit, Amount: n}); err != nil {
			return fmt.Errorf("insert valuation: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
