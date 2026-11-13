package ingest

import (
	"context"
	"slices"

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
	storedLapseRule struct {
		unit string
		rule domain.LapseRule
	}
)

// params are the rule as its unit keeps it.
func (r storedLapseRule) params(unitID int64) dbq.SetLapseRuleParams {
	short := func(n int) *int16 {
		if n == 0 {
			return nil
		}
		v := int16(n) //nolint:gosec // within range: lapseRule checks it
		return &v
	}
	return dbq.SetLapseRuleParams{
		ID: unitID, Months: short(r.rule.MonthsAfterClosing), PurgeMonth: short(r.rule.PurgeLastMonth), PurgeYears: short(r.rule.PurgeYears),
	}
}

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
	for _, r := range a.lapseRules {
		symbols = append(symbols, r.unit)
	}
	return ledger.Units(ctx, queries, a.account.Kind.UnitKind(), symbols...)
}

// unitID is the unit of a symbol the account reports, among its units.
func (a storedAccount) unitID(units map[string]int64, symbol string) int64 {
	return units[domain.Symbol(a.account.Kind.UnitKind(), symbol)]
}

// balanceUnits are the units the account reports a balance in, each once, in order.
func (a storedAccount) balanceUnits() []string {
	var units []string
	for _, b := range a.balances {
		if !slices.Contains(units, b.unit) {
			units = append(units, b.unit)
		}
	}
	return units
}
