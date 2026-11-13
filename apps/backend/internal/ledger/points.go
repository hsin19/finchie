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

// ProgramLots is what a points program's bills come to in one account, by its lapse rule (ADR 0019).
type ProgramLots struct {
	AccountID int64
	Unit      string
	domain.Lots
}

// PointsLots works out the lots of every points program a lapse rule is known for, of one account or, with
// none, of all.
func PointsLots(ctx context.Context, queries *dbq.Queries, accountID *int64) ([]ProgramLots, error) {
	rows, err := queries.PointsBills(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("points bills: %w", err)
	}
	var programs []ProgramLots
	for start := 0; start < len(rows); {
		end := start
		for end < len(rows) && rows[end].AccountID == rows[start].AccountID && rows[end].Unit == rows[start].Unit {
			end++
		}
		bills := make([]domain.PointsBill, 0, end-start)
		for _, r := range rows[start:end] {
			bills = append(bills, domain.PointsBill{
				Closing: r.Closing.Time, Added: numeric.ToRat(r.Added), Used: numeric.ToRat(r.Used), Balance: numeric.ToRat(r.Balance),
			})
		}
		first := rows[start]
		rule := domain.LapseRule{MonthsAfterClosing: whole(first.LapseMonths), PurgeLastMonth: whole(first.LapsePurgeMonth), PurgeYears: whole(first.LapsePurgeYears)}
		programs = append(programs, ProgramLots{AccountID: first.AccountID, Unit: first.Unit, Lots: domain.WorkOutLots(bills, rule)})
		start = end
	}
	return programs, nil
}

func whole(n *int16) int {
	if n == nil {
		return 0
	}
	return int(*n)
}

// explainLapses takes out of each points gap what lapsed by its program's rule (ADR 0019): points that
// vanish between two bills with no line are mostly what lapsed, and a gap that is all of it is no gap.
func explainLapses(ctx context.Context, queries *dbq.Queries, accountID *int64, gaps []domain.Gap) ([]domain.Gap, error) {
	if len(gaps) == 0 {
		return gaps, nil
	}
	programs, err := PointsLots(ctx, queries, accountID)
	if err != nil {
		return nil, err
	}
	type step struct {
		account  int64
		unit     string
		from, to string
	}
	lapsed := map[step]*big.Rat{}
	for _, p := range programs {
		for _, l := range p.Lapsed {
			lapsed[step{p.AccountID, p.Unit, l.From.Format(time.DateOnly), l.To.Format(time.DateOnly)}] = l.Amount
		}
	}
	kept := make([]domain.Gap, 0, len(gaps))
	for _, g := range gaps {
		if amount, ok := lapsed[step{g.AccountID, g.Unit, g.From.Format(time.DateOnly), g.To.Format(time.DateOnly)}]; ok {
			g.Unseen = new(big.Rat).Add(g.Unseen, amount)
			if g.Unseen.Sign() == 0 {
				continue
			}
		}
		kept = append(kept, g)
	}
	return kept, nil
}
