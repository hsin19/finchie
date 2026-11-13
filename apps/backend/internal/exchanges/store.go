package exchanges

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// Store keeps an exchange account's snapshot taken at a moment, whole or not at all (ADR 0018), as the
// account's snapshot of that day in Taipei (ledger.StoreSnapshot), with its valuation, its coins' prices
// (ADR 0020), and its deposits and withdrawals as lines, which link to a bank's (ADR 0016).
func Store(ctx context.Context, pool *pgxpool.Pool, institution string, at time.Time, snapshot Snapshot) error {
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		queries := dbq.New(tx)
		accountID, err := queries.UpsertAccount(ctx, dbq.UpsertAccountParams{Institution: institution, Kind: domain.Exchange})
		if err != nil {
			return fmt.Errorf("upsert account: %w", err)
		}
		day := taipei.Today(at)
		if _, err := ledger.StoreSnapshot(ctx, queries, ledger.Snapshot{
			AccountID: accountID, Kind: domain.Exchange, Day: day, Holdings: snapshot.Holdings,
		}); err != nil {
			return err
		}

		asOf := pgtype.Date{Time: day, Valid: true}
		if err := queries.ClearValuation(ctx, dbq.ClearValuationParams{AccountID: accountID, AsOf: asOf}); err != nil {
			return fmt.Errorf("clear valuation: %w", err)
		}
		if v := snapshot.Valuation; v != nil {
			units, err := ledger.Units(ctx, queries, domain.Money, v.Unit)
			if err != nil {
				return err
			}
			amount, err := numeric.FromRat(v.Amount)
			if err != nil {
				return fmt.Errorf("valuation: %w", err)
			}
			unitID := units[domain.Symbol(domain.Money, v.Unit)]
			if err := queries.InsertValuation(ctx, dbq.InsertValuationParams{AccountID: accountID, AsOf: asOf, UnitID: unitID, Amount: amount}); err != nil {
				return fmt.Errorf("insert valuation: %w", err)
			}
		}

		if err := storeMovements(ctx, queries, accountID, snapshot.Movements); err != nil {
			return err
		}

		// The exchange's own prices (ADR 0020): one of each coin is worth the amount.
		prices := make([]ledger.Price, 0, len(snapshot.Prices))
		for _, coin := range slices.Sorted(maps.Keys(snapshot.Prices)) {
			price := snapshot.Prices[coin]
			prices = append(prices, ledger.Price{Symbol: coin, AsOf: at, Amount: "1", Quote: price.Unit, QuoteAmount: numeric.Exact(price.Amount)})
		}
		return ledger.StorePrices(ctx, queries, domain.PriceSource(institution), domain.Money, prices)
	})
	if err != nil {
		return fmt.Errorf("store %s snapshot: %w", institution, err)
	}
	return nil
}

// storeMovements keeps each deposit and withdrawal as a line of the account, dated in Taipei, and links
// again once one is new or reported otherwise. One the exchange stops reporting stays.
func storeMovements(ctx context.Context, queries *dbq.Queries, accountID int64, moves []Movement) error {
	symbols := make([]string, 0, len(moves))
	for _, m := range moves {
		symbols = append(symbols, m.Unit)
	}
	units, err := ledger.Units(ctx, queries, domain.Money, symbols...)
	if err != nil {
		return err
	}
	changed := false
	for _, m := range moves {
		amount, err := numeric.FromRat(m.Amount)
		if err != nil {
			return fmt.Errorf("movement %s: %w", m.ID, err)
		}
		description := "入金"
		if m.Amount.Sign() < 0 {
			description = "出金"
		}
		var memo *string
		if m.Memo != "" {
			memo = &m.Memo
		}
		rows, err := queries.UpsertExchangeLine(ctx, dbq.UpsertExchangeLineParams{
			OnDate: pgtype.Date{Time: taipei.Today(m.At), Valid: true}, Description: description, Amount: amount,
			AccountID: accountID, UnitID: units[domain.Symbol(domain.Money, m.Unit)], Memo: memo, ExternalID: m.ID,
		})
		if err != nil {
			return fmt.Errorf("store movement %s: %w", m.ID, err)
		}
		changed = changed || rows > 0
	}
	if !changed {
		return nil
	}
	if _, err := ledger.Relink(ctx, queries); err != nil {
		return err
	}
	return nil
}
