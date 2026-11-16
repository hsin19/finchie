package ledger

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
)

// AccountNow is what the ledger knows of an account now, as the API shows it and notices read it: its
// newest balance in each unit, gaps, valuation, what its points will lapse by, the card bills its autopay
// takes (domain.Debits), and its standing (domain.Assess).
type AccountNow struct {
	dbq.ListAccountsRow
	Latest    []dbq.LatestBalancesRow
	Gaps      domain.Gaps
	Valuation *dbq.LatestValuationsRow
	Expiries  []dbq.LatestExpiriesRow
	Programs  []ProgramLots
	Debits    []domain.CardBill
	Standing  domain.Standing
}

// ReadAccounts reads the owner's accounts, or the one with id, as they stand now.
func ReadAccounts(ctx context.Context, queries *dbq.Queries, id *int64) ([]AccountNow, error) {
	rows, err := queries.ListAccounts(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	latest, err := queries.LatestBalances(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("latest balances: %w", err)
	}
	gaps, err := Gaps(ctx, queries, id)
	if err != nil {
		return nil, err
	}
	valuations, err := queries.LatestValuations(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("latest valuations: %w", err)
	}
	expiries, err := queries.LatestExpiries(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("latest expiries: %w", err)
	}
	programs, err := PointsLots(ctx, queries, id)
	if err != nil {
		return nil, err
	}
	debits, err := debits(ctx, queries, latest)
	if err != nil {
		return nil, err
	}

	byID := make(map[int64]*AccountNow, len(rows))
	accounts := make([]AccountNow, len(rows))
	for i, row := range rows {
		accounts[i] = AccountNow{ListAccountsRow: row, Debits: debits[row.ID]}
		byID[row.ID] = &accounts[i]
	}
	for _, b := range latest {
		if a := byID[b.AccountID]; a != nil {
			a.Latest = append(a.Latest, b)
		}
	}
	for _, v := range valuations {
		if a := byID[v.AccountID]; a != nil {
			a.Valuation = &v
		}
	}
	for _, e := range expiries {
		if a := byID[e.AccountID]; a != nil {
			a.Expiries = append(a.Expiries, e)
		}
	}
	for _, p := range programs {
		if a := byID[p.AccountID]; a != nil {
			a.Programs = append(a.Programs, p)
		}
	}
	for accountID, read := range domain.ReadGaps(gaps) {
		if a := byID[accountID]; a != nil {
			a.Gaps = read
		}
	}
	for i := range accounts {
		accounts[i].Standing = accounts[i].assess()
	}
	return accounts, nil
}

func (a AccountNow) assess() domain.Standing {
	balances := make([]domain.Balance, 0, len(a.Latest))
	for _, b := range a.Latest {
		balances = append(balances, domain.Balance{
			Unit: b.Unit, AsOf: b.AsOf.Time, Amount: numeric.ToRat(b.Amount), Worth: numeric.ToRat(b.Worth),
			FromStatement: b.StatementID != nil,
		})
	}
	var valued *domain.Valuation
	if a.Valuation != nil {
		valued = &domain.Valuation{AsOf: a.Valuation.AsOf.Time, Worth: numeric.ToRat(a.Valuation.Worth)}
	}
	return domain.Assess(a.Kind, balances, a.Gaps, valued)
}

// Fresh is how current the account is on today.
func (a AccountNow) Fresh(today time.Time) domain.Freshness {
	var checkAgainOn time.Time
	if a.CheckAgainOn.Valid {
		checkAgainOn = a.CheckAgainOn.Time
	}
	return domain.Fresh(a.Kind, a.Institution == domain.OwnerInstitution, a.Standing.Held, checkAgainOn, today)
}

// Lots are its points programs' lots by their rules, a day's together, soonest first per program.
func (a AccountNow) Lots() []domain.Lapsing {
	var lots []domain.Lapsing
	for _, p := range a.Programs {
		start := len(lots)
		for _, l := range p.Held {
			if n := len(lots); n > start && lots[n-1].By.Equal(l.By) {
				lots[n-1].Amount = new(big.Rat).Add(lots[n-1].Amount, l.Amount)
				continue
			}
			lots = append(lots, domain.Lapsing{Unit: p.Unit, Amount: l.Amount, By: l.By, Estimated: true})
		}
	}
	return lots
}

// NextLapse is the soonest of its points to lapse from today on (domain.NextLapse).
func (a AccountNow) NextLapse(today time.Time) *domain.Lapsing {
	reported := make([]domain.Lapsing, 0, len(a.Expiries))
	for _, e := range a.Expiries {
		reported = append(reported, domain.Lapsing{Unit: e.Unit, Amount: numeric.ToRat(e.Amount), By: e.LapsesBy.Time})
	}
	return domain.NextLapse(a.Lots(), reported, today)
}

// Watched is what notices read of it on today.
func (a AccountNow) Watched(today time.Time) domain.Watched {
	fresh := a.Fresh(today)
	return domain.Watched{
		ID: a.ID, Fresh: fresh, Gaps: a.Gaps.Unseen, NextLapse: a.NextLapse(today),
		Shortfalls: domain.Shortfalls(a.Standing.Held, a.Debits, fresh, today),
	}
}

// debits works out the card bills that leave each deposit account after its newest balance, `latest`
// (domain.Debits).
func debits(ctx context.Context, queries *dbq.Queries, latest []dbq.LatestBalancesRow) (map[int64][]domain.CardBill, error) {
	rows, err := queries.CardBillPayments(ctx)
	if err != nil {
		return nil, fmt.Errorf("card bill payments: %w", err)
	}
	bills := make([]domain.CardBill, 0, len(rows))
	for _, b := range rows {
		if !b.ClosingDate.Valid || !b.DueDate.Valid || b.Currency == nil {
			continue
		}
		bill := domain.CardBill{
			ID: b.ID, Institution: b.Institution, ClosingDate: b.ClosingDate.Time, DueDate: b.DueDate.Time,
			Unit: *b.Currency, TotalDue: numeric.ToRat(b.TotalDue),
		}
		if b.PaidFrom != nil {
			bill.PaidFrom = *b.PaidFrom
		}
		if b.PaidOn.Valid {
			bill.PaidOn = b.PaidOn.Time
		}
		bills = append(bills, bill)
	}
	newest := make(map[domain.AccountUnit]time.Time, len(latest))
	for _, b := range latest {
		newest[domain.AccountUnit{AccountID: b.AccountID, Unit: b.Unit}] = b.AsOf.Time
	}
	return domain.Debits(bills, newest), nil
}
