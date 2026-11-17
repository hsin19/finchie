package api

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// The investments over time: what the securities and exchange accounts are worth, as net worth has them,
// against what went into them (domain.InvestedOver); or one account's alone, for its own page.

func (s *Server) GetInvestmentHistory(ctx context.Context, request GetInvestmentHistoryRequestObject) (GetInvestmentHistoryResponseObject, error) {
	params := request.Params
	to := taipei.Today(time.Now())
	if params.To != nil {
		to = params.To.Time
	}
	step := Month
	if params.Step != nil && params.Step.Valid() {
		step = *params.Step
	}
	if to.Before(params.From.Time) {
		return GetInvestmentHistory400JSONResponse{ProblemJSONResponse{Message: "to is before from"}}, nil
	}
	queries := dbq.New(s.pool)
	since, err := queries.InvestmentsSince(ctx)
	if err != nil {
		return nil, fmt.Errorf("investments since: %w", err)
	}
	from := params.From.Time
	if since.Valid && since.Time.After(from) {
		from = since.Time
	}
	if !since.Valid || from.After(to) {
		return GetInvestmentHistory200JSONResponse{}, nil
	}
	days, problem := series(from, to, step == Day)
	if problem != "" {
		return GetInvestmentHistory400JSONResponse{ProblemJSONResponse{Message: problem}}, nil
	}
	worths, err := worthsOn(ctx, queries, days, func(a dbq.ListAccountsRow) bool {
		return a.Kind.Invests() && a.Institution != domain.OwnerInstitution && (params.Account == nil || a.ID == *params.Account)
	})
	if err != nil {
		return nil, err
	}
	rows, err := queries.InvestmentFlows(ctx, pgtype.Date{Time: days[len(days)-1], Valid: true})
	if err != nil {
		return nil, fmt.Errorf("investment flows: %w", err)
	}
	flows := make([]domain.Flow, 0, len(rows))
	for _, f := range rows {
		if params.Account == nil || f.AccountID == *params.Account {
			flows = append(flows, domain.Flow{AccountID: f.AccountID, Day: f.Day.Time, Worth: numeric.ToRat(f.Worth)})
		}
	}

	series := domain.InvestedOver(days, worths, flows)
	points := make(GetInvestmentHistory200JSONResponse, 0, len(series))
	for _, p := range series {
		point := InvestmentPoint{
			On: openapi_types.Date{Time: p.Day}, Worth: numeric.Cents(p.Worth), Invested: numeric.Cents(p.Invested),
			Gain: numeric.Cents(p.Gain), Unvalued: p.Unvalued, Unpriced: p.Unpriced, Complete: p.Complete,
		}
		if p.Return != nil {
			percent := numeric.Cents(new(big.Rat).Mul(p.Return, big.NewRat(100, 1)))
			point.ReturnPercent = &percent
		}
		points = append(points, point)
	}
	return points, nil
}
