package api

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// Net worth on any day since Finchie started (ADR 0023): every account as the domain assesses it on the
// day, added up (domain.Net).

// maxPoints bounds a history: a few years of days, or any span of month ends.
const maxPoints = 1000

func (s *Server) GetNetWorth(ctx context.Context, request GetNetWorthRequestObject) (GetNetWorthResponseObject, error) {
	day := taipei.Today(time.Now())
	if on := request.Params.On; on != nil {
		day = on.Time
	}
	nets, err := netWorths(ctx, dbq.New(s.pool), []time.Time{day})
	if err != nil {
		return nil, err
	}
	n := nets[0]
	result := NetWorth{
		On: openapi_types.Date{Time: day}, Assets: numeric.Cents(n.Assets), Liabilities: numeric.Cents(n.Liabilities),
		Net: numeric.Cents(n.Net), Unvalued: n.Unvalued, ByKind: make([]KindWorth, 0, len(n.ByKind)), Behind: []int64{},
	}
	for _, k := range n.ByKind {
		result.ByKind = append(result.ByKind, KindWorth{Kind: AccountKind(k.Kind), Unvalued: k.Unvalued, Worth: numeric.Cents(k.Worth)})
	}
	if !n.CompleteAsOf.IsZero() {
		result.CompleteAsOf = &openapi_types.Date{Time: n.CompleteAsOf}
	}
	result.Behind = append(result.Behind, n.Behind...)
	return GetNetWorth200JSONResponse(result), nil
}

func (s *Server) GetNetWorthHistory(ctx context.Context, request GetNetWorthHistoryRequestObject) (GetNetWorthHistoryResponseObject, error) {
	params := request.Params
	to := taipei.Today(time.Now())
	if params.To != nil {
		to = params.To.Time
	}
	step := Month
	if params.Step != nil && params.Step.Valid() {
		step = *params.Step
	}
	days, problem := series(params.From.Time, to, step == Day)
	if problem != "" {
		return GetNetWorthHistory400JSONResponse{ProblemJSONResponse{Message: problem}}, nil
	}
	nets, err := netWorths(ctx, dbq.New(s.pool), days)
	if err != nil {
		return nil, err
	}
	points := make(GetNetWorthHistory200JSONResponse, 0, len(days))
	for i, day := range days {
		n := nets[i]
		points = append(points, NetWorthPoint{
			On: openapi_types.Date{Time: day}, Assets: numeric.Cents(n.Assets), Liabilities: numeric.Cents(n.Liabilities),
			Net: numeric.Cents(n.Net), Unvalued: n.Unvalued,
			Complete: !n.CompleteAsOf.IsZero() && !day.After(n.CompleteAsOf),
		})
	}
	return points, nil
}

// series is every day from `from` to `to`, or the last day of each month between and `to` itself.
func series(from, to time.Time, daily bool) ([]time.Time, string) {
	if to.Before(from) {
		return nil, "to is before from"
	}
	var days []time.Time
	for day := from; !day.After(to); {
		if len(days) == maxPoints {
			return nil, fmt.Sprintf("more than %d points: ask for a shorter span, or by month", maxPoints)
		}
		if daily {
			days = append(days, day)
			day = day.AddDate(0, 0, 1)
			continue
		}
		monthEnd := time.Date(day.Year(), day.Month()+1, 0, 0, 0, 0, 0, time.UTC)
		if monthEnd.After(to) {
			monthEnd = to
		}
		days = append(days, monthEnd)
		day = monthEnd.AddDate(0, 0, 1)
	}
	return days, ""
}

// netWorths works out net worth on each day: each account's balances that day assessed by the domain,
// and, to tell how exact each figure is, every account's newest report.
func netWorths(ctx context.Context, queries *dbq.Queries, days []time.Time) ([]domain.NetWorth, error) {
	accounts, err := queries.ListAccounts(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	latest, err := queries.LatestBalances(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("latest balances: %w", err)
	}
	dates := make([]pgtype.Date, 0, len(days))
	for _, day := range days {
		dates = append(dates, pgtype.Date{Time: day, Valid: true})
	}
	held, err := queries.BalancesOn(ctx, dbq.BalancesOnParams{Days: dates, Itemized: domain.KindsWhere(domain.AccountKind.Itemized)})
	if err != nil {
		return nil, fmt.Errorf("balances on days: %w", err)
	}
	valued, err := queries.ValuationsOn(ctx, dates)
	if err != nil {
		return nil, fmt.Errorf("valuations on days: %w", err)
	}

	// How each account stands now, for its newest report and whether it holds anything.
	latestOf := map[int64][]domain.Balance{}
	for _, b := range latest {
		latestOf[b.AccountID] = append(latestOf[b.AccountID], domain.Balance{
			Unit: b.Unit, AsOf: b.AsOf.Time, Amount: numeric.ToRat(b.Amount), FromStatement: b.StatementID != nil,
		})
	}
	type key struct {
		day     time.Time
		account int64
	}
	heldOn := map[key][]domain.Balance{}
	for _, b := range held {
		k := key{b.Day.Time, b.AccountID}
		heldOn[k] = append(heldOn[k], domain.Balance{
			Unit: b.Unit, AsOf: b.AsOf.Time, Amount: numeric.ToRat(b.Amount), Worth: numeric.ToRat(b.Worth),
			FromStatement: b.StatementID != nil,
		})
	}
	valuedOn := map[key]*domain.Valuation{}
	for _, v := range valued {
		valuedOn[key{v.Day.Time, v.AccountID}] = &domain.Valuation{AsOf: v.AsOf.Time, Worth: numeric.ToRat(v.Worth)}
	}

	nets := make([]domain.NetWorth, 0, len(days))
	for _, day := range days {
		worths := make([]domain.Worth, 0, len(accounts))
		for _, a := range accounts {
			balances, ok := heldOn[key{day, a.ID}]
			if !ok {
				// Nothing had reported it yet that day.
				continue
			}
			now := domain.Assess(a.Kind, latestOf[a.ID], domain.Gaps{}, nil)
			standing := domain.Assess(a.Kind, balances, domain.Gaps{}, valuedOn[key{day, a.ID}])
			worths = append(worths, domain.Worth{
				AccountID: a.ID, Kind: a.Kind, Worth: standing.Worth, Newest: now.Newest,
				ByHand: a.Institution == domain.OwnerInstitution, Empty: len(latestOf[a.ID]) > 0 && len(now.Held) == 0,
			})
		}
		nets = append(nets, domain.Net(day, worths))
	}
	return nets, nil
}
