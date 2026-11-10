package ingest

import (
	"context"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
)

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

// unitIDs finds, or creates, the units of everything an account reports (ADR 0020).
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
	return ledger.Units(ctx, queries, a.account.Kind.UnitKind(), symbols...)
}

// unitID is the unit of a symbol the account reports, among its units.
func (a storedAccount) unitID(units map[string]int64, symbol string) int64 {
	return units[domain.Symbol(a.account.Kind.UnitKind(), symbol)]
}
