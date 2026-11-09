package ingest

import (
	"context"
	"fmt"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
)

// Kinds of unit, as stored in units.kind (ADR 0020), by the kind of account that holds them (UnitKind).
const (
	UnitMoney    = "money"
	UnitSecurity = "security"
	UnitPoints   = "points"
)

// UnitKind is the kind of the units an account of a kind holds: a securities account securities, a points
// account points, and every other money.
func UnitKind(accountKind string) string {
	switch accountKind {
	case KindSecurities:
		return UnitSecurity
	case KindPoints:
		return UnitPoints
	default:
		return UnitMoney
	}
}

// Units finds the units of a kind with these symbols, creating those no source has reported before, by
// symbol.
func Units(ctx context.Context, queries *dbq.Queries, kind string, symbols ...string) (map[string]int64, error) {
	ids := make(map[string]int64, len(symbols))
	if len(symbols) == 0 {
		return ids, nil
	}
	rows, err := queries.EnsureUnits(ctx, dbq.EnsureUnitsParams{Kind: kind, Symbols: symbols})
	if err != nil {
		return nil, fmt.Errorf("units: %w", err)
	}
	for _, r := range rows {
		ids[r.Symbol] = r.ID
	}
	return ids, nil
}

// The rows a statement reports of an account, each with its unit's symbol until the unit is found when
// stored.
type (
	storedLine struct {
		unit string
		dbq.InsertTransactionsParams
	}
	storedBalance struct {
		unit string
		dbq.InsertBalanceParams
	}
	storedExpiry struct {
		unit string
		dbq.InsertExpiryParams
	}
)

// unitIDs finds the units of everything an account reports.
func (a storedAccount) unitIDs(ctx context.Context, queries *dbq.Queries) (map[string]int64, error) {
	var symbols []string
	for _, l := range a.lines {
		symbols = append(symbols, l.unit)
	}
	for _, b := range a.balances {
		symbols = append(symbols, b.unit)
	}
	for _, e := range a.expiries {
		symbols = append(symbols, e.unit)
	}
	return Units(ctx, queries, UnitKind(a.account.Kind), symbols...)
}
