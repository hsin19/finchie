package exchanges

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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// Store keeps an exchange account's snapshot taken at a moment, whole or not at all (ADR 0018), as the
// account's snapshot of that day in Taipei (ledger.StoreSnapshot), with its valuation, its coins' prices
// (ADR 0020), and the deposits and withdrawals of a span that have settled, as lines, which link to a
// bank's or another exchange's (ADR 0016); then where the next read of them starts.
func Store(ctx context.Context, pool *pgxpool.Pool, institution string, at time.Time, snapshot Snapshot, history History) error {
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

		if err := storeMovements(ctx, queries, accountID, history.Movements); err != nil {
			return err
		}
		if !history.To.IsZero() {
			next := pgtype.Timestamptz{Time: history.next(at), Valid: true}
			if err := queries.SetExchangeCursor(ctx, dbq.SetExchangeCursorParams{Institution: institution, ReadFrom: next}); err != nil {
				return fmt.Errorf("move the cursor: %w", err)
			}
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

// readFrom is where the next read of an exchange's deposits and withdrawals starts: where the last one left
// off, or HistoryStart years before `at` the first time.
func readFrom(ctx context.Context, pool *pgxpool.Pool, institution string, at time.Time) (time.Time, error) {
	from, err := dbq.New(pool).ExchangeCursor(ctx, institution)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return at.AddDate(-HistoryStart, 0, 0), nil
	case err != nil:
		return time.Time{}, fmt.Errorf("%s cursor: %w", institution, err)
	}
	return from.Time, nil
}

// storeMovements keeps each settled deposit and withdrawal as a line of the account, dated in Taipei, and
// the fee the exchange took for it as a line of its own; and links again once one is new or reported
// otherwise. One the exchange stops reporting stays.
func storeMovements(ctx context.Context, queries *dbq.Queries, accountID int64, moves []Movement) error {
	var lines []dbq.UpsertExchangeLineParams
	var symbols []string
	for _, m := range moves {
		if m.Pending {
			continue
		}
		on := pgtype.Date{Time: taipei.Today(m.At), Valid: true}
		in := m.Amount.Sign() >= 0
		description, memo := map[bool]string{true: "入金", false: "出金"}[in], m.Memo
		if m.TxID != "" {
			// The chain's transaction is what the other end records too (ADR 0016).
			description, memo = map[bool]string{true: "充幣", false: "提幣"}[in], m.TxID
		}
		line, err := exchangeLine(m.ID, on, description, domain.Other, m.Amount, memo)
		if err != nil {
			return err
		}
		lines, symbols = append(lines, line), append(symbols, m.Unit)
		if m.Fee != nil && m.Fee.Sign() != 0 {
			fee, err := exchangeLine(m.ID+"/fee", on, description+"手續費", domain.Fee, new(big.Rat).Neg(new(big.Rat).Abs(m.Fee)), memo)
			if err != nil {
				return err
			}
			lines, symbols = append(lines, fee), append(symbols, m.FeeUnit)
		}
	}
	units, err := ledger.Units(ctx, queries, domain.Money, symbols...)
	if err != nil {
		return err
	}
	changed := false
	for i, line := range lines {
		line.AccountID, line.UnitID = accountID, units[domain.Symbol(domain.Money, symbols[i])]
		rows, err := queries.UpsertExchangeLine(ctx, line)
		if err != nil {
			return fmt.Errorf("store movement %s: %w", line.ExternalID, err)
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

// exchangeLine is one line of a movement, its account and unit found when it is stored.
func exchangeLine(id string, on pgtype.Date, description string, kind domain.LineKind, amount *big.Rat, memo string) (dbq.UpsertExchangeLineParams, error) {
	n, err := numeric.FromRat(amount)
	if err != nil {
		return dbq.UpsertExchangeLineParams{}, fmt.Errorf("movement %s: %w", id, err)
	}
	line := dbq.UpsertExchangeLineParams{OnDate: on, Description: description, Kind: kind, Amount: n, ExternalID: id}
	if memo != "" {
		line.Memo = &memo
	}
	return line, nil
}
